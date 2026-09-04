package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

func newTestServer() *Server {
	return &Server{
		states:     map[string]*bankState{"fakebank": {}},
		challenges: newChallengeRegistry(),
	}
}

func TestHandleStatus_NoPendingChallenge(t *testing.T) {
	s := newTestServer()

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OK") {
		t.Errorf("body = %q, want it to mention OK", rec.Body.String())
	}
}

func TestHandleStatus_LinksToPendingChallenge(t *testing.T) {
	s := newTestServer()
	s.challenges.set("fakebank", &pendingChallenge{
		challenge: bank.Challenge{Description: "enter TAN", NeedsInput: true},
		replyCh:   make(chan string, 1),
	})

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(rec.Body.String(), "/auth/fakebank") {
		t.Errorf("body = %q, want a link to /auth/fakebank", rec.Body.String())
	}
}

func TestHandleAuthGet_NotFoundWithoutPendingChallenge(t *testing.T) {
	s := newTestServer()

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/fakebank", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleAuthGet_RendersPendingChallenge(t *testing.T) {
	s := newTestServer()
	s.challenges.set("fakebank", &pendingChallenge{
		challenge: bank.Challenge{Description: "enter TAN", Hint: "some hint", NeedsInput: true},
		replyCh:   make(chan string, 1),
	})

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/fakebank", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "enter TAN") || !strings.Contains(body, "some hint") {
		t.Errorf("body = %q, want it to contain the description and hint", body)
	}
	if !strings.Contains(body, "<form") {
		t.Error("expected a form since NeedsInput is true")
	}
}

func TestHandleAuthPost_DeliversValueAndClearsChallenge(t *testing.T) {
	s := newTestServer()
	p := &pendingChallenge{
		challenge: bank.Challenge{Description: "enter TAN", NeedsInput: true},
		replyCh:   make(chan string, 1),
	}
	s.challenges.set("fakebank", p)

	form := url.Values{"value": {"123456"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/fakebank", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	select {
	case v := <-p.replyCh:
		if v != "123456" {
			t.Errorf("delivered value = %q, want 123456", v)
		}
	case <-time.After(time.Second):
		t.Fatal("value was never delivered to replyCh")
	}
}

func TestHandleAuthPost_ConflictWithoutPendingChallenge(t *testing.T) {
	s := newTestServer()

	form := url.Values{"value": {"123456"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/fakebank", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestHandleAuthImage_ServesPendingImage(t *testing.T) {
	s := newTestServer()
	image := []byte{0x89, 'P', 'N', 'G'}
	s.challenges.set("fakebank", &pendingChallenge{
		challenge: bank.Challenge{Image: image, NeedsInput: true},
		replyCh:   make(chan string, 1),
	})

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/fakebank/image.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Body.String() != string(image) {
		t.Errorf("body = %v, want %v", rec.Body.Bytes(), image)
	}
}

func TestHandleAuthImage_NotFoundWithoutImage(t *testing.T) {
	s := newTestServer()
	s.challenges.set("fakebank", &pendingChallenge{
		challenge: bank.Challenge{NeedsInput: true},
		replyCh:   make(chan string, 1),
	})

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/fakebank/image.png", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
