package paperless

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

func TestDocumentExists_True(t *testing.T) {
	var gotMethod, gotPath, gotFilename, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotFilename = r.URL.Query().Get("original_filename__iexact")
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"count":1}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	exists, err := c.DocumentExists(context.Background(), "Kosteninformation zum Wertpapiergeschäft.pdf")
	if err != nil {
		t.Fatalf("DocumentExists: %v", err)
	}
	if !exists {
		t.Error("exists = false, want true")
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/documents/" {
		t.Errorf("path = %q, want /api/documents/", gotPath)
	}
	if gotFilename != "Kosteninformation zum Wertpapiergeschäft.pdf" {
		t.Errorf("original_filename__iexact = %q, want the original filename round-tripped", gotFilename)
	}
	if gotAuth != "Token mytoken" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Token mytoken")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
}

func TestDocumentExists_False(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"count":0}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	exists, err := c.DocumentExists(context.Background(), "new.pdf")
	if err != nil {
		t.Fatalf("DocumentExists: %v", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
}

func TestDocumentExists_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"detail":"invalid token"}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "badtoken")
	_, err := c.DocumentExists(context.Background(), "a.pdf")
	if err == nil {
		t.Fatal("expected an error for a 403 response")
	}
}

func TestDocumentExists_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	_, err := c.DocumentExists(context.Background(), "a.pdf")
	if err == nil {
		t.Fatal("expected an error for a malformed response body")
	}
}

func TestUpload_Success(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotAccept string
	var gotFilename, gotContent, gotTitle, gotCreated string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("document")
		if err != nil {
			t.Errorf("FormFile: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		gotFilename = header.Filename
		content, _ := io.ReadAll(file)
		gotContent = string(content)
		gotTitle = r.FormValue("title")
		gotCreated = r.FormValue("created")

		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	doc := bank.Document{
		Filename: "Finanzreport Nr. 08 per 01.09.2026.pdf",
		Date:     time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := c.Upload(context.Background(), doc, []byte("pdf-bytes")); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/documents/post_document/" {
		t.Errorf("path = %q, want /api/documents/post_document/", gotPath)
	}
	if gotAuth != "Token mytoken" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Token mytoken")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotFilename != doc.Filename {
		t.Errorf("uploaded filename = %q, want %q", gotFilename, doc.Filename)
	}
	if gotContent != "pdf-bytes" {
		t.Errorf("uploaded content = %q, want %q", gotContent, "pdf-bytes")
	}
	if gotTitle != doc.Filename {
		t.Errorf("title = %q, want %q", gotTitle, doc.Filename)
	}
	if gotCreated != "2026-09-01" {
		t.Errorf("created = %q, want 2026-09-01", gotCreated)
	}
}

func TestUpload_AcceptsAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	if err := c.Upload(context.Background(), bank.Document{Filename: "a.pdf"}, []byte("x")); err != nil {
		t.Fatalf("Upload: %v", err)
	}
}

func TestUpload_OmitsCreatedFieldForZeroDate(t *testing.T) {
	var sawCreated bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_, sawCreated = r.MultipartForm.Value["created"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	doc := bank.Document{Filename: "a.pdf"} // Date is the zero value
	if err := c.Upload(context.Background(), doc, []byte("x")); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if sawCreated {
		t.Error("expected no \"created\" field when doc.Date is zero")
	}
}

func TestUpload_UsesBaseNameForFileField(t *testing.T) {
	var gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_, header, err := r.FormFile("document")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		gotFilename = header.Filename
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	doc := bank.Document{Filename: "some/nested/path/a.pdf"}
	if err := c.Upload(context.Background(), doc, []byte("x")); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if gotFilename != "a.pdf" {
		t.Errorf("uploaded filename = %q, want base name %q", gotFilename, "a.pdf")
	}
}

func TestUpload_SetsMultipartContentType(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	if err := c.Upload(context.Background(), bank.Document{Filename: "a.pdf"}, []byte("x")); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	mediaType, _, err := mime.ParseMediaType(gotContentType)
	if err != nil {
		t.Fatalf("ParseMediaType(%q): %v", gotContentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Errorf("Content-Type = %q, want multipart/form-data", mediaType)
	}
}

func TestUpload_ErrorOnFailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"document":["File type text/html not supported"]}`)
	}))
	defer srv.Close()

	c := New(srv.URL, "mytoken")
	err := c.Upload(context.Background(), bank.Document{Filename: "a.html"}, []byte("x"))
	if err == nil {
		t.Fatal("expected an error for a 400 response")
	}
}
