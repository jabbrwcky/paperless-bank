package sync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/paperless"
)

type fakeSource struct {
	docs       []bank.Document
	downloaded map[string]bool
}

func (f *fakeSource) ListDocuments(context.Context) ([]bank.Document, error) {
	return f.docs, nil
}

func (f *fakeSource) DownloadDocument(_ context.Context, doc bank.Document) ([]byte, error) {
	if f.downloaded == nil {
		f.downloaded = map[string]bool{}
	}
	f.downloaded[doc.ID] = true
	return []byte("content"), nil
}

func TestRun_SkipsDisallowedMIMETypes(t *testing.T) {
	var existsCalls, uploadCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			existsCalls++
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":0}`)
		case http.MethodPost:
			uploadCalls++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	src := &fakeSource{docs: []bank.Document{
		{ID: "1", Filename: "a.pdf", MIMEType: "application/pdf"},
		{ID: "2", Filename: "b.html", MIMEType: "text/html"},
	}}
	orch := &Orchestrator{
		Source:    src,
		Paperless: paperless.New(srv.URL, "token"),
	}

	if err := orch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if existsCalls != 1 {
		t.Errorf("existsCalls = %d, want 1 (only for the pdf)", existsCalls)
	}
	if uploadCalls != 1 {
		t.Errorf("uploadCalls = %d, want 1", uploadCalls)
	}
	if !src.downloaded["1"] {
		t.Error("expected the pdf document to be downloaded")
	}
	if src.downloaded["2"] {
		t.Error("the html document should not have been downloaded")
	}
}

func TestRun_HonorsCustomAllowedMIMETypes(t *testing.T) {
	var uploadCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":0}`)
		case http.MethodPost:
			uploadCalls++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	src := &fakeSource{docs: []bank.Document{
		{ID: "1", Filename: "a.html", MIMEType: "text/html"},
	}}
	orch := &Orchestrator{
		Source:           src,
		Paperless:        paperless.New(srv.URL, "token"),
		AllowedMIMETypes: []string{"text/html"},
	}

	if err := orch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if uploadCalls != 1 {
		t.Errorf("uploadCalls = %d, want 1", uploadCalls)
	}
}

func TestMimeTypeAllowed(t *testing.T) {
	cases := []struct {
		mimeType string
		allowed  []string
		want     bool
	}{
		{"application/pdf", []string{"application/pdf"}, true},
		{"APPLICATION/PDF", []string{"application/pdf"}, true},
		{"text/html", []string{"application/pdf"}, false},
		{"application/pdf", nil, false},
	}
	for _, c := range cases {
		if got := mimeTypeAllowed(c.mimeType, c.allowed); got != c.want {
			t.Errorf("mimeTypeAllowed(%q, %v) = %v, want %v", c.mimeType, c.allowed, got, c.want)
		}
	}
}
