package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/httpx"
)

// Client uploads documents to a paperless-ngx instance.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// DocumentExists returns true if a document with the given original filename
// already exists in paperless-ngx, to avoid duplicate uploads.
func (c *Client) DocumentExists(ctx context.Context, filename string) (bool, error) {
	params := url.Values{}
	params.Set("original_filename__iexact", filename)
	u := c.baseURL + "/api/documents/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return false, err
	}
	c.setHeaders(req)

	resp, err := httpx.Do(c.http, req)
	if err != nil {
		return false, fmt.Errorf("check duplicate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, fmt.Errorf("paperless HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return result.Count > 0, nil
}

// Upload sends a document to paperless-ngx via POST /api/documents/.
func (c *Client) Upload(ctx context.Context, doc bank.Document, content []byte) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	part, err := w.CreateFormFile("document", filepath.Base(doc.Filename))
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := w.WriteField("title", doc.Filename); err != nil {
		return err
	}
	if !doc.Date.IsZero() {
		if err := w.WriteField("created", doc.Date.Format("2006-01-02")); err != nil {
			return err
		}
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/documents/", &body)
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := httpx.Do(c.http, req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	// paperless-ngx returns 200 or 202 on success
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		rb, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("upload HTTP %d: %s", resp.StatusCode, rb)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json")
}
