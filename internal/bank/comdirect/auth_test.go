package comdirect

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

type fakeHandler struct {
	challenge bank.Challenge
	value     string
	err       error
}

func (f *fakeHandler) Handle(_ context.Context, challenge bank.Challenge) (string, error) {
	f.challenge = challenge
	return f.value, f.err
}

func TestPromptTAN_PTAN_DecodesImage(t *testing.T) {
	image := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	challenge := &onceAuthInfo{
		Typ:            "P_TAN",
		Challenge:      base64.StdEncoding.EncodeToString(image),
		AvailableTypes: []string{"P_TAN", "M_TAN"},
	}
	h := &fakeHandler{value: "654321"}

	tan, err := (&Client{}).promptTAN(context.Background(), h, challenge)
	if err != nil {
		t.Fatalf("promptTAN: %v", err)
	}
	if tan != "654321" {
		t.Errorf("tan = %q, want 654321", tan)
	}
	if !h.challenge.NeedsInput {
		t.Error("expected NeedsInput = true for P_TAN")
	}
	if string(h.challenge.Image) != string(image) {
		t.Errorf("Image = %v, want decoded bytes %v", h.challenge.Image, image)
	}
	if h.challenge.Hint != "" {
		t.Errorf("Hint = %q, want empty when the image decoded", h.challenge.Hint)
	}
}

func TestPromptTAN_PTAN_UndecodableFallsBackToHint(t *testing.T) {
	challenge := &onceAuthInfo{Typ: "P_TAN", Challenge: "not-valid-base64!!"}
	h := &fakeHandler{value: "111111"}

	tan, err := (&Client{}).promptTAN(context.Background(), h, challenge)
	if err != nil {
		t.Fatalf("promptTAN: %v", err)
	}
	if tan != "111111" {
		t.Errorf("tan = %q, want 111111", tan)
	}
	if len(h.challenge.Image) != 0 {
		t.Error("expected no Image when the challenge failed to decode")
	}
	if h.challenge.Hint != challenge.Challenge {
		t.Errorf("Hint = %q, want the raw challenge text as a fallback", h.challenge.Hint)
	}
}

func TestPromptTAN_MTAN_SetsHint(t *testing.T) {
	challenge := &onceAuthInfo{Typ: "M_TAN", Challenge: "+49-160-XXXXXXX"}
	h := &fakeHandler{value: "222333"}

	tan, err := (&Client{}).promptTAN(context.Background(), h, challenge)
	if err != nil {
		t.Fatalf("promptTAN: %v", err)
	}
	if tan != "222333" {
		t.Errorf("tan = %q, want 222333", tan)
	}
	if h.challenge.Hint != "+49-160-XXXXXXX" {
		t.Errorf("Hint = %q, want the phone hint", h.challenge.Hint)
	}
	if len(h.challenge.Image) != 0 {
		t.Error("M_TAN should not set an Image")
	}
}

func TestPollPushTAN_NoLinkFallsBackToHandler(t *testing.T) {
	challenge := &onceAuthInfo{Typ: "P_TAN_PUSH"} // Link intentionally zero
	h := &fakeHandler{}

	if err := (&Client{}).pollPushTAN(context.Background(), h, challenge); err != nil {
		t.Fatalf("pollPushTAN: %v", err)
	}
	if !h.challenge.NeedsInput {
		t.Error("expected the no-link fallback to require input")
	}
}

func withFastPushPoll(t *testing.T, interval, timeout time.Duration) {
	t.Helper()
	origInterval, origTimeout := pushTANPollInterval, pushTANPollTimeout
	pushTANPollInterval, pushTANPollTimeout = interval, timeout
	t.Cleanup(func() { pushTANPollInterval, pushTANPollTimeout = origInterval, origTimeout })
}

func TestPollPushTAN_PollsUntilAuthenticated(t *testing.T) {
	withFastPushPoll(t, time.Millisecond, time.Second)

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls < 3 {
			fmt.Fprint(w, `{"authenticationId":"1","status":"PENDING"}`)
			return
		}
		fmt.Fprint(w, `{"authenticationId":"1","status":"AUTHENTICATED"}`)
	}))
	defer srv.Close()

	challenge := &onceAuthInfo{Typ: "P_TAN_PUSH", Link: authLink{Href: "/status"}}
	h := &fakeHandler{}

	if err := testClient(srv.URL).pollPushTAN(context.Background(), h, challenge); err != nil {
		t.Fatalf("pollPushTAN: %v", err)
	}
	if h.challenge.NeedsInput {
		t.Error("expected the initial notify to have NeedsInput = false")
	}
	if calls < 3 {
		t.Errorf("calls = %d, want at least 3 polls", calls)
	}
}

func TestPollPushTAN_TimesOut(t *testing.T) {
	withFastPushPoll(t, time.Millisecond, 20*time.Millisecond)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"authenticationId":"1","status":"PENDING"}`)
	}))
	defer srv.Close()

	challenge := &onceAuthInfo{Typ: "P_TAN_PUSH", Link: authLink{Href: "/status"}}
	err := testClient(srv.URL).pollPushTAN(context.Background(), &fakeHandler{}, challenge)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
}
