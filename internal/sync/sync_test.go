package sync

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

func TestRun_SkipsDocumentsOlderThanWatermark(t *testing.T) {
	var existsCalls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			existsCalls = append(existsCalls, r.URL.Query().Get("original_filename__iexact"))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":0}`)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	watermarkPath := filepath.Join(t.TempDir(), "watermark.json")
	if err := saveWatermark(watermarkPath, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	src := &fakeSource{docs: []bank.Document{
		{ID: "1", Filename: "old.pdf", MIMEType: "application/pdf", Date: time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "2", Filename: "new.pdf", MIMEType: "application/pdf", Date: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}
	orch := &Orchestrator{Source: src, Paperless: paperless.New(srv.URL, "token"), WatermarkPath: watermarkPath}

	if err := orch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(existsCalls) != 1 || existsCalls[0] != "new.pdf" {
		t.Errorf("existsCalls = %v, want just [new.pdf]", existsCalls)
	}
}

func TestRun_AdvancesWatermarkToNewestDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":0}`)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	watermarkPath := filepath.Join(t.TempDir(), "watermark.json")
	newest := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	src := &fakeSource{docs: []bank.Document{
		{ID: "1", Filename: "a.pdf", MIMEType: "application/pdf", Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "2", Filename: "b.pdf", MIMEType: "application/pdf", Date: newest},
	}}
	orch := &Orchestrator{Source: src, Paperless: paperless.New(srv.URL, "token"), WatermarkPath: watermarkPath}

	if err := orch.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, err := loadWatermark(watermarkPath)
	if err != nil {
		t.Fatalf("loadWatermark: %v", err)
	}
	if !got.Equal(newest) {
		t.Errorf("watermark = %v, want %v", got, newest)
	}
}

func TestRun_DoesNotAdvanceWatermarkPastAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("original_filename__iexact") == "fails.pdf" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"count":0}`)
		case http.MethodPost:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	watermarkPath := filepath.Join(t.TempDir(), "watermark.json")
	earlyDate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	failDate := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	lateDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	src := &fakeSource{docs: []bank.Document{
		{ID: "1", Filename: "ok.pdf", MIMEType: "application/pdf", Date: earlyDate},
		{ID: "2", Filename: "fails.pdf", MIMEType: "application/pdf", Date: failDate},
		{ID: "3", Filename: "late.pdf", MIMEType: "application/pdf", Date: lateDate},
	}}
	orch := &Orchestrator{Source: src, Paperless: paperless.New(srv.URL, "token"), WatermarkPath: watermarkPath}

	if err := orch.Run(context.Background()); err == nil {
		t.Fatal("expected Run to report the failed document")
	}
	got, err := loadWatermark(watermarkPath)
	if err != nil {
		t.Fatalf("loadWatermark: %v", err)
	}
	if !got.Equal(earlyDate) {
		t.Errorf("watermark = %v, want %v (the last success before the failure)", got, earlyDate)
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
