package comdirect

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type tokenCache struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Authenticate runs the full Comdirect 6-step OAuth2+TAN flow and persists
// the resulting tokens to the configured cache file.
//
// Step 1  POST /oauth/token  (password grant)
// Step 2  GET  /api/session/clients/user_id/v1/sessions
// Step 3  POST /api/session/clients/user_id/v1/sessions/{id}/validate  → TAN challenge
// Step 4  User enters TAN (interactive prompt)
// Step 5  PATCH /api/session/clients/user_id/v1/sessions/{id}  (activate with TAN)
// Step 6  POST /oauth/token  (cd_secondary grant → fully authenticated token)
func (c *Client) Authenticate(ctx context.Context) error {
	// Step 1 — password grant
	// TODO: implement

	// Step 2 — retrieve session list, extract sessionId
	// TODO: implement

	// Step 3 — trigger TAN challenge, parse x-once-authentication-info header
	// TODO: implement

	// Step 4 — prompt user for TAN
	tan, err := promptTAN()
	if err != nil {
		return fmt.Errorf("read TAN: %w", err)
	}
	_ = tan

	// Step 5 — PATCH session with TAN value
	// TODO: implement

	// Step 6 — exchange for fully authenticated access token
	// TODO: implement

	return fmt.Errorf("not yet implemented — see Architecture.md §Comdirect for the full flow")
}

// EnsureAuthenticated checks that a valid token exists, refreshing it if close
// to expiry. Returns an error directing the user to run 'auth' if no token is cached.
func (c *Client) EnsureAuthenticated(ctx context.Context) error {
	if c.token == nil {
		return fmt.Errorf("not authenticated: run 'paperless-bank auth comdirect' first")
	}
	if time.Until(c.token.ExpiresAt) < 5*time.Minute {
		return c.refreshToken(ctx)
	}
	return nil
}

func (c *Client) refreshToken(ctx context.Context) error {
	// TODO: POST /oauth/token with grant_type=refresh_token
	return fmt.Errorf("token refresh not yet implemented")
}

func (c *Client) loadTokenCache() error {
	path := expandPath(c.cfg.TokenCache)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tc tokenCache
	if err := json.Unmarshal(data, &tc); err != nil {
		return fmt.Errorf("parse token cache %s: %w", path, err)
	}
	c.token = &tc
	return nil
}

func (c *Client) saveTokenCache() error {
	path := expandPath(c.cfg.TokenCache)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func promptTAN() (string, error) {
	fmt.Print("Enter TAN: ")
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}

// expandPath converts a leading ~/ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
