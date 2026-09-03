package comdirect

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jabbrwcky/paperless-bank/internal/bank"
	"github.com/jabbrwcky/paperless-bank/internal/httpx"
)

// defaultBaseURL is overridden by tests via Client.baseURL.
const defaultBaseURL = "https://api.comdirect.de"

func init() {
	bank.Register("comdirect", func(cfg bank.Config) (bank.DocumentSource, error) {
		return New(cfg)
	})
}

// Client is an authenticated Comdirect API client.
type Client struct {
	cfg  bank.Config
	http *http.Client
	// baseURL is defaultBaseURL in production; tests override it to point
	// at an httptest.Server.
	baseURL string
	// token holds the cached OAuth tokens once authenticated.
	token *tokenCache
	// clientSessionID is a client-generated UUID sent in every
	// x-http-request-info header. It must stay constant for the whole
	// login flow and is distinct from the server session identifier.
	clientSessionID string
	// sessionID is the server-side session identifier returned by the
	// session-status endpoint, used in the validate/activate URLs.
	sessionID string
}

// New creates a Client and loads any cached token from disk.
// If no valid token exists the caller must run Authenticate before
// calling ListDocuments or DownloadDocument.
func New(cfg bank.Config) (*Client, error) {
	c := &Client{
		cfg:             cfg,
		http:            &http.Client{Timeout: 30 * time.Second},
		baseURL:         defaultBaseURL,
		clientSessionID: newUUID(),
	}
	_ = c.loadTokenCache() // absence of a cache file is not an error here
	return c, nil
}

// do executes an authenticated HTTP request against the Comdirect API. An
// empty accept defaults to "application/json"; pass an explicit value for
// endpoints that don't return JSON (e.g. document downloads).
func (c *Client) do(ctx context.Context, method, path, accept string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != nil {
		req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	}
	req.Header.Set("x-http-request-info", c.requestInfo())
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	return httpx.Do(c.http, req)
}

// requestInfo returns the JSON value required by Comdirect for x-http-request-info.
// The sessionId is the stable client-generated UUID; requestId is a fresh
// numeric value per request (Comdirect requires 1–9 characters).
func (c *Client) requestInfo() string {
	return fmt.Sprintf(
		`{"clientRequestId":{"sessionId":%q,"requestId":%q}}`,
		c.clientSessionID, newRequestID(),
	)
}

// newRequestID returns a random 9-digit numeric string.
func newRequestID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	n := binary.BigEndian.Uint32(b[:]) % 1_000_000_000
	return fmt.Sprintf("%09d", n)
}

// newUUID returns a random RFC 4122 version 4 UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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
