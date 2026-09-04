package server

import (
	"context"
	"sync"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// pendingChallenge is a challenge currently awaiting a response from the web
// UI for one bank.
type pendingChallenge struct {
	challenge bank.Challenge
	replyCh   chan string
}

// challengeRegistry holds at most one pending challenge per bank name and
// hands out a bank.ChallengeHandler scoped to a single bank.
type challengeRegistry struct {
	mu      sync.Mutex
	pending map[string]*pendingChallenge
}

func newChallengeRegistry() *challengeRegistry {
	return &challengeRegistry{pending: make(map[string]*pendingChallenge)}
}

// handlerFor returns a bank.ChallengeHandler that records challenges under
// name so the HTTP handlers can find and answer them.
func (r *challengeRegistry) handlerFor(name string) bank.ChallengeHandler {
	return &webChallengeHandler{registry: r, bank: name}
}

// pendingFor returns the challenge currently awaiting a response for name,
// if any.
func (r *challengeRegistry) pendingFor(name string) (bank.Challenge, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pending[name]
	if !ok {
		return bank.Challenge{}, false
	}
	return p.challenge, true
}

// submit delivers value to the goroutine blocked in Handle for name, if one
// is waiting. Returns false if there was nothing to deliver to (e.g. it
// already timed out, was already answered, or didn't need input).
func (r *challengeRegistry) submit(name, value string) bool {
	r.mu.Lock()
	p, ok := r.pending[name]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case p.replyCh <- value:
		return true
	default:
		return false
	}
}

func (r *challengeRegistry) set(name string, p *pendingChallenge) {
	r.mu.Lock()
	r.pending[name] = p
	r.mu.Unlock()
}

func (r *challengeRegistry) clear(name string) {
	r.mu.Lock()
	delete(r.pending, name)
	r.mu.Unlock()
}

// webChallengeHandler implements bank.ChallengeHandler by publishing the
// challenge for display at /auth/{bank} and, when input is needed, blocking
// until a POST to that path supplies a value.
type webChallengeHandler struct {
	registry *challengeRegistry
	bank     string
}

func (h *webChallengeHandler) Handle(ctx context.Context, challenge bank.Challenge) (string, error) {
	p := &pendingChallenge{challenge: challenge, replyCh: make(chan string, 1)}
	h.registry.set(h.bank, p)

	if !challenge.NeedsInput {
		// Pure notification (e.g. "waiting for push approval"): leave it
		// visible on the status page but don't block here - the bank's own
		// polling loop (if any) drives completion from this point.
		return "", nil
	}

	select {
	case v := <-p.replyCh:
		h.registry.clear(h.bank)
		return v, nil
	case <-ctx.Done():
		h.registry.clear(h.bank)
		return "", ctx.Err()
	}
}
