package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
)

// fakeAuthBank implements bank.DocumentSource, bank.TokenChecker and
// bank.Authenticator for testing Server.syncOne's auth-gating logic.
type fakeAuthBank struct {
	ensureErr  error
	authCalled bool
}

func (f *fakeAuthBank) ListDocuments(context.Context) ([]bank.Document, error) { return nil, nil }
func (f *fakeAuthBank) DownloadDocument(context.Context, bank.Document) ([]byte, error) {
	return nil, nil
}
func (f *fakeAuthBank) EnsureAuthenticated(context.Context) error { return f.ensureErr }
func (f *fakeAuthBank) Authenticate(ctx context.Context, handler bank.ChallengeHandler) error {
	f.authCalled = true
	_, err := handler.Handle(ctx, bank.Challenge{Description: "test challenge", NeedsInput: true})
	return err
}

func TestSyncOne_AutoReauthDisabled_DoesNotAuthenticate(t *testing.T) {
	fb := &fakeAuthBank{ensureErr: errors.New("token expired")}
	bank.Register("test-noauth", func(bank.Config) (bank.DocumentSource, error) { return fb, nil })

	s := &Server{
		Paperless:  paperless.New("http://example.invalid", "token"),
		challenges: newChallengeRegistry(),
		states:     map[string]*bankState{"test-noauth": {}},
		AutoReauth: false,
	}

	if err := s.syncOne(context.Background(), "test-noauth", bank.Config{}); err == nil {
		t.Fatal("expected an error when auth is required and AutoReauth is disabled")
	}
	if fb.authCalled {
		t.Error("Authenticate should not be called with AutoReauth disabled")
	}
}

func TestSyncOne_AutoReauthEnabled_AuthenticatesThenSyncs(t *testing.T) {
	fb := &fakeAuthBank{ensureErr: errors.New("token expired")}
	bank.Register("test-autoreauth", func(bank.Config) (bank.DocumentSource, error) { return fb, nil })

	s := &Server{
		Paperless:  paperless.New("http://example.invalid", "token"),
		challenges: newChallengeRegistry(),
		states:     map[string]*bankState{"test-autoreauth": {}},
		AutoReauth: true,
	}

	// Answer the challenge as soon as syncOne (via Authenticate -> Handle)
	// publishes it.
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, ok := s.challenges.pendingFor("test-autoreauth"); ok {
				s.challenges.submit("test-autoreauth", "123456")
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	if err := s.syncOne(context.Background(), "test-autoreauth", bank.Config{}); err != nil {
		t.Fatalf("syncOne: %v", err)
	}
	if !fb.authCalled {
		t.Error("expected Authenticate to be called")
	}
	if _, ok := s.challenges.pendingFor("test-autoreauth"); ok {
		t.Error("expected the pending challenge to be cleared after Authenticate returns")
	}
}

func TestSyncOne_NoAuthNeeded_SyncsDirectly(t *testing.T) {
	fb := &fakeAuthBank{ensureErr: nil}
	bank.Register("test-noop", func(bank.Config) (bank.DocumentSource, error) { return fb, nil })

	s := &Server{
		Paperless:  paperless.New("http://example.invalid", "token"),
		challenges: newChallengeRegistry(),
		states:     map[string]*bankState{"test-noop": {}},
		AutoReauth: false,
	}

	if err := s.syncOne(context.Background(), "test-noop", bank.Config{}); err != nil {
		t.Fatalf("syncOne: %v", err)
	}
	if fb.authCalled {
		t.Error("Authenticate should not be called when EnsureAuthenticated succeeds")
	}
}

func TestRun_ServesStatusAndShutsDownCleanly(t *testing.T) {
	fb := &fakeAuthBank{}
	bank.Register("test-run", func(bank.Config) (bank.DocumentSource, error) { return fb, nil })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	s := &Server{
		Paperless:    paperless.New("http://example.invalid", "token"),
		Banks:        map[string]bank.Config{"test-run": {}},
		SyncInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, addr) }()

	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never became reachable: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned an error on shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not shut down after ctx was canceled")
	}
}
