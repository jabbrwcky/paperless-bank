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
    // DownloadDocument returns the raw document bytes.
    DownloadDocument(ctx context.Context, id string) ([]byte, error)
}
```

## CLI commands

| Command | Description |
|---------|-------------|
| `paperless-bank auth <bank>` | Interactively authenticate and persist token/session |
| `paperless-bank sync` | Fetch new documents from all configured banks and upload to paperless-ngx |
| `paperless-bank list` | List available documents without uploading |

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

4. User completes TAN challenge (interactive prompt or push notification)

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
GET  /api/banking/v3/documents          → paginated list of inbox documents
GET  /api/banking/v3/documents/{id}/content  → raw document bytes
```

Response documents include `mimeType`, `name`, `dateCreation`.

**Base URL:** `https://api.comdirect.de`

## Sync flow

```
for each configured bank:
  1. Load/refresh access token from cache
  2. Call ListDocuments()
  3. For each document not yet in paperless-ngx:
     a. DownloadDocument()
     b. POST /api/documents/ to paperless-ngx (multipart, filename preserved)
  4. Log outcome (uploaded / skipped / error) per document
```

Duplicate detection: query paperless-ngx for existing documents by original filename before uploading. If a document with the same filename already exists, skip it.

## Error handling

- Auth errors (expired token, TAN required) exit with a clear message directing the user to run `paperless-bank auth <bank>`.
- Network errors are retried up to 3 times with exponential backoff (stdlib only — no retry library).
- A failed upload for one document does not abort the sync; errors are collected and reported at the end.
