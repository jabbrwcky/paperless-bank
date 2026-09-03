package comdirect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// oauthTokenResponse is the JSON body returned by POST /oauth/token.
type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// sessionInfo mirrors an entry from the session-status endpoint and is
// echoed back in the validate/activate request bodies.
type sessionInfo struct {
	Identifier       string `json:"identifier"`
	SessionTanActive bool   `json:"sessionTanActive"`
	Activated2FA     bool   `json:"activated2FA"`
}

// onceAuthInfo is the parsed x-once-authentication-info header describing the
// TAN challenge the user must complete.
type onceAuthInfo struct {
	ID             string   `json:"id"`
	Typ            string   `json:"typ"`
	AvailableTypes []string `json:"availableTypes"`
	Challenge      string   `json:"challenge"`
	// Link, when present, points to an endpoint that reports whether a
	// P_TAN_PUSH challenge has been approved yet, so it can be polled
	// instead of asking the user to press Enter after approving.
	Link authLink `json:"link"`
}

type authLink struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

// authStatusResponse is returned by GET onceAuthInfo.Link.Href while polling
// for photoTAN Push approval.
type authStatusResponse struct {
	AuthenticationID string `json:"authenticationId"`
	Status           string `json:"status"`
}

// Authenticate runs the full Comdirect OAuth2 + TAN flow and persists the
// resulting tokens to the configured cache file. See Architecture.md §Comdirect.
func (c *Client) Authenticate(ctx context.Context) error {
	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		return fmt.Errorf("comdirect: client ID and secret are required (set COMDIRECT_CLIENT_ID / COMDIRECT_CLIENT_SECRET)")
	}
	if c.cfg.Username == "" || c.cfg.Password == "" {
		return fmt.Errorf("comdirect: username and password are required (set COMDIRECT_USERNAME / COMDIRECT_PASSWORD)")
	}

	// Step 1 — resource-owner password grant.
	tok, err := c.requestToken(ctx, url.Values{
		"grant_type":    {"password"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"username":      {c.cfg.Username},
		"password":      {c.cfg.Password},
	})
	if err != nil {
		return fmt.Errorf("password grant: %w", err)
	}
	// Persist the interim token so session calls carry Authorization.
	c.setToken(tok)

	// Step 2 — retrieve the session identifier.
	if err := c.fetchSessionID(ctx); err != nil {
		return fmt.Errorf("fetch session: %w", err)
	}

	// Step 3 — trigger the TAN challenge.
	challenge, err := c.validateSession(ctx)
	if err != nil {
		return fmt.Errorf("validate session: %w", err)
	}

	// Step 4 — user completes the TAN challenge.
	tan, err := c.promptTAN(ctx, challenge)
	if err != nil {
		return fmt.Errorf("read TAN: %w", err)
	}

	// Step 5 — activate the session with the TAN.
	if err := c.activateSession(ctx, challenge, tan); err != nil {
		return fmt.Errorf("activate session: %w", err)
	}

	// Step 6 — exchange for a fully authenticated token (cd_secondary grant).
	secondary, err := c.requestToken(ctx, url.Values{
		"grant_type":    {"cd_secondary"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"token":         {tok.AccessToken},
	})
	if err != nil {
		return fmt.Errorf("cd_secondary grant: %w", err)
	}
	c.setToken(secondary)

	if err := c.saveTokenCache(); err != nil {
		return fmt.Errorf("save token cache: %w", err)
	}
	fmt.Printf("Authenticated. Token cached at %s (expires %s).\n",
		expandPath(c.cfg.TokenCache), c.token.ExpiresAt.Format(time.RFC3339))
	return nil
}

// requestToken performs a form-encoded POST to /oauth/token.
func (c *Client) requestToken(ctx context.Context, form url.Values) (*oauthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, tokenError(resp)
	}
	var tok oauthTokenResponse
	if err := decodeJSON(resp, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// tokenError turns a non-200 /oauth/token response into an actionable error,
// recognising Comdirect's OAuth error codes.
func tokenError(resp *http.Response) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var oe struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &oe)

	switch oe.Error {
	case "invalid_client":
		return fmt.Errorf("comdirect rejected the client credentials (%s).\n"+
			"This is about COMDIRECT_CLIENT_ID / COMDIRECT_CLIENT_SECRET, not your username/PIN.\n"+
			"Activate and verify API access in comdirect: persönlicher Bereich → Verwaltung → API-Zugang,\n"+
			"then confirm the client_id/client_secret match exactly (regenerate them if unsure)",
			strings.TrimSpace(oe.Description))
	case "invalid_grant":
		return fmt.Errorf("comdirect rejected the login (%s).\n"+
			"Check COMDIRECT_USERNAME (8-digit Zugangsnummer) and COMDIRECT_PASSWORD (6-digit PIN)",
			strings.TrimSpace(oe.Description))
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
}

// setToken stores an OAuth response as the active cached token.
func (c *Client) setToken(tok *oauthTokenResponse) {
	c.token = &tokenCache{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}
}

// fetchSessionID retrieves the current session identifier.
// GET /api/session/clients/user/v1/sessions
func (c *Client) fetchSessionID(ctx context.Context) error {
	resp, err := c.sessionRequest(ctx, "GET", "/api/session/clients/user/v1/sessions", nil, nil)
	if err != nil {
		return err
	}
	if err := checkStatus(resp, 200); err != nil {
		return err
	}
	var sessions []sessionInfo
	if err := decodeJSON(resp, &sessions); err != nil {
		return err
	}
	if len(sessions) == 0 {
		return fmt.Errorf("no session returned")
	}
	c.sessionID = sessions[0].Identifier
	return nil
}

// validateSession triggers the TAN challenge and returns its details. If
// cfg.TanType is set, it's sent as the requested challenge type; otherwise
// Comdirect uses the account's default TAN method.
// POST /api/session/clients/user/v1/sessions/{id}/validate
func (c *Client) validateSession(ctx context.Context) (*onceAuthInfo, error) {
	body, _ := json.Marshal(sessionInfo{
		Identifier:       c.sessionID,
		SessionTanActive: true,
		Activated2FA:     true,
	})
	var headers map[string]string
	if c.cfg.TanType != "" {
		headers = map[string]string{
			"x-once-authentication-info": fmt.Sprintf(`{"typ":%q}`, c.cfg.TanType),
		}
	}
	path := "/api/session/clients/user/v1/sessions/" + c.sessionID + "/validate"
	resp, err := c.sessionRequest(ctx, "POST", path, body, headers)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp, 201); err != nil {
		return nil, err
	}
	resp.Body.Close()

	raw := resp.Header.Get("x-once-authentication-info")
	if raw == "" {
		return nil, fmt.Errorf("missing x-once-authentication-info header")
	}
	var info onceAuthInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return nil, fmt.Errorf("parse challenge info: %w", err)
	}
	return &info, nil
}

// activateSession completes the TAN challenge, activating the session.
// PATCH /api/session/clients/user/v1/sessions/{id}
func (c *Client) activateSession(ctx context.Context, challenge *onceAuthInfo, tan string) error {
	body, _ := json.Marshal(sessionInfo{
		Identifier:       c.sessionID,
		SessionTanActive: true,
		Activated2FA:     true,
	})
	headers := map[string]string{
		"x-once-authentication-info": fmt.Sprintf(`{"id":%q}`, challenge.ID),
	}
	if tan != "" {
		headers["x-once-authentication"] = tan
	}
	path := "/api/session/clients/user/v1/sessions/" + c.sessionID
	resp, err := c.sessionRequest(ctx, "PATCH", path, body, headers)
	if err != nil {
		return err
	}
	if err := checkStatus(resp, 200); err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// sessionRequest issues an authenticated JSON request carrying the standard
// Comdirect headers. extraHeaders may add or override headers (e.g. the TAN).
func (c *Client) sessionRequest(ctx context.Context, method, path string, body []byte, extraHeaders map[string]string) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, r)
	if err != nil {
		return nil, err
	}
	if c.token != nil {
		req.Header.Set("Authorization", "Bearer "+c.token.AccessToken)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-http-request-info", c.requestInfo())
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return c.http.Do(req)
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

// refreshToken exchanges the refresh token for a fresh access token.
func (c *Client) refreshToken(ctx context.Context) error {
	if c.token == nil || c.token.RefreshToken == "" {
		return fmt.Errorf("no refresh token: run 'paperless-bank auth comdirect' first")
	}
	tok, err := c.requestToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
		"refresh_token": {c.token.RefreshToken},
	})
	if err != nil {
		return fmt.Errorf("refresh token (re-run 'paperless-bank auth comdirect' if expired): %w", err)
	}
	c.setToken(tok)
	return c.saveTokenCache()
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

// tanTypeLabel describes a Comdirect TAN challenge type in plain language so
// the user knows what kind of token/action is expected before being prompted.
func tanTypeLabel(typ string) string {
	switch typ {
	case "P_TAN":
		return "photoTAN — scan the graphic with your photoTAN app/reader"
	case "P_TAN_PUSH":
		return "photoTAN Push — approve the login request in your photoTAN app"
	case "M_TAN":
		return "mobile TAN — SMS sent to your registered phone number"
	default:
		return typ + " (unrecognized type — follow the instructions in your comdirect security app)"
	}
}

// readLine prompts on stdout and reads a single trimmed line from stdin.
func readLine(prompt string) (string, error) {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	return strings.TrimSpace(line), err
}

// promptTAN guides the user through the TAN challenge, describing what kind
// of token is expected and adapting to the challenge type Comdirect returned:
// P_TAN_PUSH is approved in the photoTAN app and polled for automatically
// (no typed value); P_TAN presents a photoTAN graphic that must be decoded
// before it can be scanned; M_TAN and any unrecognized type fall back to a
// typed TAN.
func (c *Client) promptTAN(ctx context.Context, challenge *onceAuthInfo) (string, error) {
	fmt.Printf("TAN required: %s\n", tanTypeLabel(challenge.Typ))
	if len(challenge.AvailableTypes) > 0 {
		fmt.Printf("Other TAN methods available on this account: %s\n", strings.Join(challenge.AvailableTypes, ", "))
	}

	switch challenge.Typ {
	case "P_TAN_PUSH":
		return "", c.pollPushTAN(ctx, challenge)
	case "P_TAN":
		png, err := base64.StdEncoding.DecodeString(challenge.Challenge)
		if err != nil {
			// Fall back to the generic flow if the graphic can't be decoded.
			if challenge.Challenge != "" {
				fmt.Printf("Challenge: %s\n", challenge.Challenge)
			}
			return readLine("Enter TAN: ")
		}
		f, err := os.CreateTemp("", "comdirect-phototan-*.png")
		if err != nil {
			return "", fmt.Errorf("write photoTAN graphic: %w", err)
		}
		defer os.Remove(f.Name())
		if _, err := f.Write(png); err != nil {
			f.Close()
			return "", fmt.Errorf("write photoTAN graphic: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("write photoTAN graphic: %w", err)
		}
		fmt.Printf("photoTAN graphic saved to %s — open it and scan with your photoTAN app/reader.\n", f.Name())
		return readLine("Enter TAN: ")
	case "M_TAN":
		if challenge.Challenge != "" {
			fmt.Printf("Challenge: %s\n", challenge.Challenge)
		}
		return readLine("Enter TAN: ")
	default:
		if challenge.Challenge != "" {
			fmt.Printf("Challenge: %s\n", challenge.Challenge)
		}
		return readLine("Enter TAN: ")
	}
}

// pushTANPollInterval and pushTANPollTimeout match the values used by other
// Comdirect API clients polling the same status endpoint.
const (
	pushTANPollInterval = 3 * time.Second
	pushTANPollTimeout  = 5 * time.Minute
)

// pollPushTAN waits for a P_TAN_PUSH challenge to be approved in the user's
// photoTAN app by polling the status link Comdirect returns alongside the
// challenge (x-once-authentication-info.link), rather than asking the user
// to press Enter after approving. If no link is present — some accounts or
// API versions may not return one — it falls back to that manual prompt.
func (c *Client) pollPushTAN(ctx context.Context, challenge *onceAuthInfo) error {
	if challenge.Link.Href == "" {
		fmt.Println("Approve the login in your photoTAN app, then press Enter.")
		_, err := readLine("")
		return err
	}

	fmt.Println("Waiting for you to approve the login in your photoTAN app...")
	ctx, cancel := context.WithTimeout(ctx, pushTANPollTimeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for photoTAN Push approval: %w", ctx.Err())
		case <-time.After(pushTANPollInterval):
		}

		resp, err := c.sessionRequest(ctx, "GET", challenge.Link.Href, nil, nil)
		if err != nil {
			return fmt.Errorf("poll photoTAN status: %w", err)
		}
		if err := checkStatus(resp, 200); err != nil {
			resp.Body.Close()
			return fmt.Errorf("poll photoTAN status: %w", err)
		}
		var status authStatusResponse
		if err := decodeJSON(resp, &status); err != nil {
			return fmt.Errorf("poll photoTAN status: %w", err)
		}
		if status.Status == "AUTHENTICATED" {
			fmt.Println("Approved.")
			return nil
		}
	}
}

// expandPath converts a leading ~/ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
