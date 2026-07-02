package comdirect

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// documentListResponse mirrors the Comdirect GET /api/banking/v3/documents response.
type documentListResponse struct {
	Paging struct {
		Index   int `json:"index"`
		Matches int `json:"matches"`
	} `json:"paging"`
	Values []documentItem `json:"values"`
}

type documentItem struct {
	DocumentID      string `json:"documentId"`
	Name            string `json:"name"`
	DateCreation    string `json:"dateCreation"` // "YYYY-MM-DD"
	MimeType        string `json:"mimeType"`
	ReadStatus      string `json:"readStatus"` // "READ" | "UNREAD"
	DeletionAllowed bool   `json:"deletionAllowed"`
}

// ListDocuments returns all documents in the Comdirect inbox.
// GET /api/banking/v3/documents
func (c *Client) ListDocuments(ctx context.Context) ([]bank.Document, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, "GET", "/api/banking/v3/documents", nil)
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

// DownloadDocument fetches the raw bytes of a single document.
// GET /api/banking/v3/documents/{id}/content
func (c *Client) DownloadDocument(ctx context.Context, id string) ([]byte, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	resp, err := c.do(ctx, "GET", "/api/banking/v3/documents/"+id+"/content", nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", id, err)
	}
	if err := checkStatus(resp, 200); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
