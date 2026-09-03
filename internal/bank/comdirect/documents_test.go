package comdirect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testClient(baseURL string) *Client {
	return &Client{
		http:    http.DefaultClient,
		baseURL: baseURL,
		token:   &tokenCache{AccessToken: "test", ExpiresAt: time.Now().Add(time.Hour)},
	}
}

func TestListDocuments_FollowsPagination(t *testing.T) {
	var gotFirst []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := r.URL.Query().Get("paging-first")
		gotFirst = append(gotFirst, first)
		w.Header().Set("Content-Type", "application/json")
		switch first {
		case "0":
			fmt.Fprint(w, `{"paging":{"index":0,"matches":3},"values":[
				{"documentId":"1","name":"doc1","dateCreation":"2024-01-01","mimeType":"application/pdf","deletable":true},
				{"documentId":"2","name":"doc2","dateCreation":"2024-01-02","mimeType":"application/pdf","deletable":true}
			]}`)
		case "2":
			fmt.Fprint(w, `{"paging":{"index":2,"matches":3},"values":[
				{"documentId":"3","name":"doc3","dateCreation":"2024-01-03","mimeType":"application/pdf","deletable":true}
			]}`)
		default:
			t.Errorf("unexpected paging-first=%q", first)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	docs, err := testClient(srv.URL).ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(gotFirst) != 2 {
		t.Fatalf("requests = %v, want 2 (one per page)", gotFirst)
	}
	if len(docs) != 3 {
		t.Fatalf("got %d documents, want 3", len(docs))
	}
	for i, id := range []string{"1", "2", "3"} {
		if docs[i].ID != id {
			t.Errorf("docs[%d].ID = %q, want %q", i, docs[i].ID, id)
		}
	}
}

func TestListDocuments_StopsOnEmptyPage(t *testing.T) {
	// Defensive: if a page ever comes back empty despite paging.matches
	// claiming more, stop instead of looping forever on the same offset.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"paging":{"index":0,"matches":100},"values":[]}`)
	}))
	defer srv.Close()

	docs, err := testClient(srv.URL).ListDocuments(context.Background())
	if err != nil {
		t.Fatalf("ListDocuments: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("got %d documents, want 0", len(docs))
	}
}
