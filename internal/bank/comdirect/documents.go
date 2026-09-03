package comdirect

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// documentListResponse mirrors the Comdirect
// GET /api/messages/clients/user/v2/documents response (ListResourceDocument).
type documentListResponse struct {
	Paging struct {
		Index   int `json:"index"`
		Matches int `json:"matches"`
	} `json:"paging"`
	Values []documentItem `json:"values"`
}

type documentItem struct {
	DocumentID   string `json:"documentId"`
	Name         string `json:"name"`
	DateCreation string `json:"dateCreation"` // "YYYY-MM-DD"
	MimeType     string `json:"mimeType"`
	Deletable    bool   `json:"deletable"`
}

// ListDocuments returns all documents in the Comdirect inbox.
// GET /api/messages/clients/user/v2/documents ("user" is a literal path
// segment, not a placeholder for the actual username).
func (c *Client) ListDocuments(ctx context.Context) ([]bank.Document, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, "GET", "/api/messages/clients/user/v2/documents", "", nil)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	if err := checkStatus(resp, 200); err != nil {
		return nil, err
	}

	var result documentListResponse
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}

	// TODO: handle pagination when result.Paging.Matches > len(result.Values)

	docs := make([]bank.Document, 0, len(result.Values))
	for _, item := range result.Values {
		date, _ := time.Parse("2006-01-02", item.DateCreation)
		docs = append(docs, bank.Document{
			ID:       item.DocumentID,
			Filename: item.Name,
			MIMEType: item.MimeType,
			Date:     date,
		})
	}
	return docs, nil
}

// DownloadDocument fetches the raw bytes of a single document. The endpoint
// returns the document in its native format (application/pdf or text/html),
// never JSON, so it must not send the default Accept: application/json.
// GET /api/messages/v2/documents/{documentId}
func (c *Client) DownloadDocument(ctx context.Context, id string) ([]byte, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, "GET", "/api/messages/v2/documents/"+id, "application/pdf, text/html, */*", nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", id, err)
	}
	if err := checkStatus(resp, 200); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
