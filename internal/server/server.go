// Package server runs paperless-bank's sync loop on an interval and exposes
// a minimal web UI for completing bank authentication challenges (e.g. a
// TAN) when no terminal is attached.
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
	syncpkg "github.com/jabbrwcky/paperless-bank/internal/sync"
)

// bankState tracks the latest sync outcome for one configured bank, shown on
// the status page.
type bankState struct {
	mu        sync.Mutex
	lastRunAt time.Time
	lastErr   error
	nextRunAt time.Time

	// authMu serializes EnsureAuthenticated/Authenticate for this bank so the
	// token keep-alive loop and the sync loop never race to refresh the same
	// token concurrently (Comdirect rotates the refresh token on each use, so
	// a concurrent double-refresh would fail one of the two callers). It's
	// deliberately separate from mu above so a slow network refresh never
	// blocks the status page from rendering the cheap display fields.
	authMu sync.Mutex
}

// Server runs the sync loop on an interval and exposes a minimal web UI for
// completing bank authentication challenges.
type Server struct {
	Paperless    *paperless.Client
	Banks        map[string]bank.Config
	SyncInterval time.Duration
	// AutoReauth controls whether a failed EnsureAuthenticated triggers a
	// live Authenticate (a real bank login and TAN challenge) automatically.
	// Off by default: this is a deliberate, real-world side effect (it can
	// send an SMS/push TAN to the account holder), not something to trigger
	// silently just because a background sync tick found an expired token.
	AutoReauth bool

	challenges *challengeRegistry
	states     map[string]*bankState
}

// Run starts the sync loop and HTTP server, blocking until ctx is canceled,
// then shuts both down.
func (s *Server) Run(ctx context.Context, listen string) error {
	if s.SyncInterval <= 0 {
		return fmt.Errorf("sync interval must be positive")
	}
	s.challenges = newChallengeRegistry()
	s.states = make(map[string]*bankState, len(s.Banks))
	for name := range s.Banks {
		s.states[name] = &bankState{}
	}

	httpSrv := &http.Server{Addr: listen, Handler: s.routes()}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("web UI listening on http://%s", listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	go s.syncLoop(ctx)
	go s.tokenRefreshLoop(ctx)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}

// syncLoop runs a sync cycle immediately, then again on every tick, until
// ctx is canceled.
func (s *Server) syncLoop(ctx context.Context) {
	s.syncAll(ctx)
	ticker := time.NewTicker(s.SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAll(ctx)
		}
	}
}

// tokenRefreshInterval is how often the keep-alive loop checks whether a
// bank's token needs refreshing. It runs independently of (and typically far
// more often than) SyncInterval, so a long sync interval - or a sync tick
// that keeps failing for unrelated reasons - doesn't leave a refreshable
// token to go stale between syncs.
const tokenRefreshInterval = 5 * time.Minute

// tokenRefreshLoop periodically calls EnsureAuthenticated for every
// configured bank, keeping the session alive independently of the sync
// schedule, until ctx is canceled.
func (s *Server) tokenRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for name, cfg := range s.Banks {
				if _, err := s.ensureBankAuthenticated(ctx, name, cfg); err != nil {
					log.Printf("keep-alive [%s]: %v", name, err)
				}
			}
		}
	}
}

func (s *Server) syncAll(ctx context.Context) {
	for name, cfg := range s.Banks {
		st := s.states[name]
		st.mu.Lock()
		st.nextRunAt = time.Now().Add(s.SyncInterval)
		st.mu.Unlock()

		err := s.syncOne(ctx, name, cfg)

		st.mu.Lock()
		st.lastRunAt = time.Now()
		st.lastErr = err
		st.mu.Unlock()

		if err != nil {
			log.Printf("sync [%s]: %v", name, err)
		}
	}
}

// syncOne ensures name's token is valid, then runs a normal sync.
func (s *Server) syncOne(ctx context.Context, name string, cfg bank.Config) error {
	src, err := s.ensureBankAuthenticated(ctx, name, cfg)
	if err != nil {
		return err
	}

	orch := &syncpkg.Orchestrator{
		Source:        src,
		Paperless:     s.Paperless,
		WatermarkPath: syncpkg.DefaultWatermarkPath(name),
	}
	return orch.Run(ctx)
}

// ensureBankAuthenticated returns a DocumentSource for name with a valid
// token, refreshing it via TokenChecker if needed. If refreshing fails and
// s.AutoReauth is set, it falls back to a full interactive Authenticate via
// the web UI; this fallback only applies here (server mode) and only when
// explicitly enabled - the plain `sync` command keeps its original fail-fast
// behavior. Serialized per bank (bankState.authMu) since this is called from
// both the sync loop and the token keep-alive loop, and Comdirect rotates
// the refresh token on each use - a concurrent double-refresh would fail one
// of the two callers.
func (s *Server) ensureBankAuthenticated(ctx context.Context, name string, cfg bank.Config) (bank.DocumentSource, error) {
	st := s.states[name]
	st.authMu.Lock()
	defer st.authMu.Unlock()

	src, err := bank.New(name, cfg)
	if err != nil {
		return nil, err
	}

	tc, ok := src.(bank.TokenChecker)
	if !ok {
		return src, nil
	}
	if err := tc.EnsureAuthenticated(ctx); err != nil {
		if !s.AutoReauth {
			return nil, fmt.Errorf("authentication required: run 'paperless-bank auth %s' (or start serve with --auto-reauth): %w", name, err)
		}
		auth, ok := src.(bank.Authenticator)
		if !ok {
			return nil, fmt.Errorf("authentication required but %s does not support interactive auth: %w", name, err)
		}
		log.Printf("[%s]: authentication required, complete it at /auth/%s", name, name)
		authErr := auth.Authenticate(ctx, s.challenges.handlerFor(name))
		s.challenges.clear(name)
		if authErr != nil {
			return nil, fmt.Errorf("authenticate: %w", authErr)
		}
	}
	return src, nil
}
