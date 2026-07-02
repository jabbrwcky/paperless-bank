package comdirect

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
)

const baseURL = "https://api.comdirect.de"

func init() {
	bank.Register("comdirect", func(cfg bank.Config) (bank.DocumentSource, error) {
		return New(cfg)
	})
}

// Client is an authenticated Comdirect API client.
type Client struct {
	cfg       bank.Config
	http      *http.Client
	token     *tokenCache
	sessionID string
}

// New creates a Client and loads any cached token from disk.
// If no valid token exists the caller must run Authenticate before
// calling ListDocuments or DownloadDocument.
func New(cfg bank.Config) (*Client, error) {
	c := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
	_ = c.loadTokenCache() // absence of a cache file is not an error here
	return c, nil
}

// do executes an authenticated HTTP request against the Comdirect API.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != nil {
		req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	}
	req.Header.Set("x-http-request-info", c.requestInfo())
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

// requestInfo returns the JSON value required by Comdirect for x-http-request-info.
func (c *Client) requestInfo() string {
	return fmt.Sprintf(
		`{"clientRequestId":{"sessionId":%q,"requestId":%q}}`,
		c.sessionID, newRequestID(),
	)
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b)
}

func checkStatus(resp *http.Response, wantStatus int) error {
	if resp.StatusCode != wantStatus {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}

func decodeJSON(resp *http.Response, dst any) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
