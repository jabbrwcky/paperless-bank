# Architecture

## Overview

`paperless-bank` is a single-binary CLI tool. It authenticates with one or more bank APIs, retrieves documents from the customer inbox/postbox, and uploads them to a paperless-ngx instance. State lives entirely in paperless-ngx (duplicate detection via existing document check or filename).

```
┌─────────────────────────────────────────────────────────┐
│  paperless-bank (CLI — kong)                            │
│                                                         │
│  ┌──────────┐    ┌───────────────┐    ┌─────────────┐  │
│  │  auth    │    │     sync      │    │    list     │  │
│  │ command  │    │   command     │    │   command   │  │
│  └──────────┘    └──────┬────────┘    └─────────────┘  │
│                         │                               │
│              ┌──────────▼──────────┐                   │
│              │   sync.Orchestrator │                   │
│              └──────┬──────────────┘                   │
│                     │                                   │
│          ┌──────────┴──────────┐                        │
│          ▼                     ▼                        │
│  ┌──────────────┐    ┌──────────────────┐               │
│  │ bank.Source  │    │ paperless.Client │               │
│  │ (interface)  │    │                  │               │
│  └──────┬───────┘    └──────────────────┘               │
│         │                                               │
│  ┌──────▼───────┐                                       │
│  │  comdirect   │  (more banks added here)              │
│  │  Client      │                                       │
│  └──────────────┘                                       │
└─────────────────────────────────────────────────────────┘
```

## Key interfaces

### `bank.DocumentSource`

```go
// internal/bank/interface.go
type Document struct {
    ID          string
    Filename    string
    MIMEType    string
    Date        time.Time
    Description string
}

type DocumentSource interface {
    // ListDocuments returns all documents in the inbox/postbox.
    ListDocuments(ctx context.Context) ([]Document, error)
    // DownloadDocument returns the raw bytes of doc.
    DownloadDocument(ctx context.Context, doc Document) ([]byte, error)
}
```

### `bank.Authenticator` / `bank.ChallengeHandler` / `bank.TokenChecker`

```go
// internal/bank/interface.go
type Challenge struct {
    Description string // human label, e.g. "photoTAN — scan the graphic..."
    Image       []byte // optional challenge graphic (e.g. decoded photoTAN PNG)
    Hint        string // optional hint text (e.g. a masked phone number)
    NeedsInput  bool   // false for pure notifications (e.g. "approve the push")
}

// ChallengeHandler lets a bank's Authenticate delegate the interactive part
// of a challenge to whatever is driving it (CLI prompt, web UI, ...), so
// bank packages have no direct I/O dependency of their own.
type ChallengeHandler interface {
    Handle(ctx context.Context, challenge Challenge) (string, error)
}

type Authenticator interface {
    Authenticate(ctx context.Context, handler ChallengeHandler) error
}

// TokenChecker lets a caller verify/refresh a cached token without user
// interaction, falling back to Authenticator when that fails.
type TokenChecker interface {
    EnsureAuthenticated(ctx context.Context) error
}
```

`cmd/paperless-bank/challenge_cli.go` implements `ChallengeHandler` for the terminal (writes a
challenge graphic to a temp file, prompts on stdin); `internal/server`'s `webChallengeHandler`
implements it for the browser (serves the graphic over HTTP, blocks on a channel until a form is
submitted). Neither `comdirect` nor any future bank package touches a terminal or an HTTP response
directly.

## CLI commands

| Command | Description |
|---------|-------------|
| `paperless-bank auth <bank>` | Interactively authenticate and persist token/session |
| `paperless-bank sync` | Fetch new documents from all configured banks and upload to paperless-ngx |
| `paperless-bank list` | List available documents without uploading |
| `paperless-bank serve` | Run `sync` on an interval with a web UI for auth challenges (see **Server mode**) |
| `paperless-bank completion <shell>` | Print a bash/zsh/fish completion script, generated from the kong CLI definition via [`miekg/king`](https://github.com/miekg/king) |

## Configuration

Kong reads a config file (default `~/.config/paperless-bank/config.yaml`). Override with `--config`.

```yaml
paperless:
  url: https://paperless.example.com
  token: YOUR_API_TOKEN

comdirect:
  client_id: YOUR_CLIENT_ID
  client_secret: YOUR_CLIENT_SECRET
  username: YOUR_USERNAME
  password: YOUR_PASSWORD
  # token cache written here after `auth` command
  token_cache: ~/.config/paperless-bank/comdirect-token.json
```

## Bank integrations

### Comdirect

Comdirect uses a non-standard OAuth2 flow on top of standard token endpoints. The full sequence is required before document APIs are accessible.

**Authentication flow:**

```
1. POST /oauth/token
   grant_type=password, client_id, client_secret, username, password
   → access_token, refresh_token, kdnr (customer number), session hint

2. GET /api/session/clients/user_id/v1/sessions
   Authorization: Bearer <access_token>
   x-http-request-info: {clientRequestId: {sessionId, requestId}}
   → [{identifier: <sessionId>, ...}]

3. POST /api/session/clients/user_id/v1/sessions/{sessionId}/validate
   → 201 with x-once-authentication-info header containing TAN challenge info
      (challenge type: P_TAN, photoTAN, etc.)

4. User completes TAN challenge:
   - P_TAN/M_TAN: typed interactively (photoTAN graphic decoded to a temp file for scanning).
   - P_TAN_PUSH: no typed value — polled automatically via the status link Comdirect returns
     in x-once-authentication-info.link (GET every 3s, up to 5 minutes) until "AUTHENTICATED".

5. PATCH /api/session/clients/user_id/v1/sessions/{sessionId}
   x-once-authentication: {"typ":"...","value":"<TAN>"}
   → activates the session

6. POST /oauth/token
   grant_type=cd_secondary, token=<access_token>
   → new access_token valid for fully authenticated API calls

7. Persist access_token + refresh_token to token_cache file.
```

Tokens are refreshed automatically using the refresh_token before expiry. If the refresh token is expired, `auth` must be re-run.

**Document API:**

```
GET  /api/messages/clients/user/v2/documents   → paginated list of inbox documents ("user" is literal, not a placeholder)
GET  /api/messages/v2/documents/{documentId}   → raw document bytes (native format: application/pdf or text/html, never JSON)
```

Response documents include `mimeType`, `name`, `dateCreation`.

**Base URL:** `https://api.comdirect.de`

## Sync flow

```
for each configured bank:
  1. Load/refresh access token from cache
  2. Call ListDocuments() (Comdirect: follows paging-first/paging-count until all matches fetched)
  3. Sort documents by date; skip any strictly older than the saved sync watermark (if any)
  4. For each remaining document:
     a. Skip if its MIME type is not in --mime-types (default: application/pdf only)
     b. Skip if it already exists in paperless-ngx
     c. DownloadDocument()
     d. POST /api/documents/post_document/ to paperless-ngx (multipart, filename preserved)
  5. Log outcome (uploaded / skipped / error) per document
  6. Advance the sync watermark to the newest date processed before the first failure (if any)
```

Duplicate detection: query paperless-ngx for existing documents by original filename before uploading. If a document with the same filename already exists, skip it.

MIME type filtering exists because paperless-ngx rejects file types it doesn't support (e.g. a
bank's `text/html` marketing notices mixed into the same inbox as PDF statements) — uploading them
unfiltered fails the whole document rather than just that one item.

**Sync watermark** (`internal/sync/watermark.go`, `Orchestrator.WatermarkPath`, default
`~/.cache/paperless-bank/<bank>-sync-state.json`): bank document-list APIs generally have no
server-side date filter, so without this every run re-lists and re-checks a bank's *entire*
history against paperless-ngx forever, growing more wasteful every year. After a run, the
orchestrator persists the newest document date it fully accounted for; the next run skips
documents strictly older than that watermark before even calling `DocumentExists`. The watermark
only advances up to the last document processed before the first failure in date order, so a
transient failure (and everything from that point on) is still reconsidered next run — it's purely
a performance optimization, not a correctness mechanism: `DocumentExists`'s filename check remains
the actual duplicate-prevention safety net, since bank document dates carry no time-of-day and
same-day documents are never skipped this way.

## Error handling

- Auth errors (expired token, TAN required) exit with a clear message directing the user to run `paperless-bank auth <bank>`.
- Network errors and 5xx responses are retried up to 3 times with exponential backoff
  (`internal/httpx.Do`, stdlib only — no retry library). This wraps the Comdirect document API and
  the paperless-ngx client; the OAuth2/TAN session flow is deliberately excluded since replaying a
  TAN validate/activate call isn't safe to retry blindly.
- HTTP 429 (rate limited) is retried on a separate, longer backoff schedule, honoring a
  `Retry-After` header when the server sends one.
- A failed upload for one document does not abort the sync; errors are collected and reported at the end.

## Server mode

`internal/server.Server` (`paperless-bank serve`) reuses `sync.Orchestrator`, running it
immediately on startup and then on every `--sync-interval` tick, alongside a minimal `net/http`
web UI (no router/templating dependency — just `http.ServeMux`'s Go 1.22+ method/wildcard patterns
and inline `html/template` strings):

```
GET  /                        status page: per bank, last/next run and outcome, or a link to
                               /auth/{bank} if a challenge is pending
GET  /auth/{bank}             renders the pending challenge (graphic/hint/form as needed)
POST /auth/{bank}             submits a typed value, delivered to the blocked ChallengeHandler
GET  /auth/{bank}/image.png   serves the pending challenge's graphic, if any
```

All state (per-bank last-run outcome, pending challenges) is in-memory only and does not survive a
restart — no persistent database is introduced. Sync watermarks (see **Sync flow**) are the one
piece of state that does persist to disk, exactly like the token cache.

**Auth flow:** `Server.ensureBankAuthenticated` calls the bank's `TokenChecker.EnsureAuthenticated`.
If that fails:
- By default, it just returns an error ("action required") without attempting a login — the
  operator must run `paperless-bank auth <bank>` themselves.
- With `--auto-reauth`, it instead calls `Authenticator.Authenticate` with a `webChallengeHandler`
  bound to that bank, so the challenge (and, for a typed TAN, the submitted value) flows through
  `/auth/{bank}` instead of a terminal.

Two independent loops call `ensureBankAuthenticated` per bank, serialized by a per-bank mutex
(`bankState.authMu`) so they can never race to refresh the same token concurrently (Comdirect
rotates the refresh token on each use, so a concurrent double-refresh would fail one of the two
callers):
- The sync loop, right before each `--sync-interval` tick's sync.
- A separate **token keep-alive loop** (`tokenRefreshInterval`, currently 5 minutes, independent of
  `--sync-interval`) that exists specifically so a long sync interval — or a sync tick that keeps
  failing for unrelated reasons — doesn't leave a refreshable token to go stale between syncs.

`--auto-reauth` defaults to **off**. A live `Authenticate` call is a real side effect — it can send
the account holder an actual SMS/push TAN — and it's easy to trigger unintentionally if real
credentials happen to be available in the process environment (e.g. via `.envrc`/direnv). This
happened once during this feature's own development: an unrelated test invocation of `serve`
inherited real Comdirect credentials from the shell environment and triggered a live SMS TAN.
`--auto-reauth` exists specifically so that a live login is always an explicit choice.
