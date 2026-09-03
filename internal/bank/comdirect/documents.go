package comdirect

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

// documentsPageSize is the paging-count used when listing documents, capped
// at Comdirect's documented maximum of 1000 to minimize round-trips.
const documentsPageSize = 1000

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

// ListDocuments returns all documents in the Comdirect inbox, following
// pagination (paging-first/paging-count) until every matched document has
// been fetched.
// GET /api/messages/clients/user/v2/documents ("user" is a literal path
// segment, not a placeholder for the actual username).
func (c *Client) ListDocuments(ctx context.Context) ([]bank.Document, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	var docs []bank.Document
	first := 0
	for {
		params := url.Values{
			"paging-first": {fmt.Sprintf("%d", first)},
			"paging-count": {fmt.Sprintf("%d", documentsPageSize)},
		}
		path := "/api/messages/clients/user/v2/documents?" + params.Encode()
		resp, err := c.do(ctx, "GET", path, "", nil)
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
		if len(result.Values) == 0 {
			break
		}

		for _, item := range result.Values {
			date, _ := time.Parse("2006-01-02", item.DateCreation)
			docs = append(docs, bank.Document{
				ID:       item.DocumentID,
				Filename: item.Name,
				MIMEType: item.MimeType,
				Date:     date,
			})
		}

		first += len(result.Values)
		if first >= result.Paging.Matches {
			break
		}
	}
	return docs, nil
}

// DownloadDocument fetches the raw bytes of doc. Comdirect requires the
// Accept header to name exactly the document's expected MIME type (a
// wildcard or multi-value Accept is rejected with 406), so doc.MIMEType
// (populated by ListDocuments) must be passed through rather than just the
// ID.
// GET /api/messages/v2/documents/{documentId}
func (c *Client) DownloadDocument(ctx context.Context, doc bank.Document) ([]byte, error) {
	if err := c.EnsureAuthenticated(ctx); err != nil {
		return nil, err
	}

	accept := doc.MIMEType
	if accept == "" {
		accept = "application/pdf"
	}
	resp, err := c.do(ctx, "GET", "/api/messages/v2/documents/"+doc.ID, accept, nil)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", doc.ID, err)
	}
	if err := checkStatus(resp, 200); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
