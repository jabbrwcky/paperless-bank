# TODO

## v1 — Comdirect → paperless-ngx

### Project scaffold

- [x] `go mod init github.com/jabbrwcky/paperless-bank`
- [x] Add `github.com/alecthomas/kong` dependency
- [x] Basic `cmd/paperless-bank/main.go` with kong wiring and YAML config file support

### Configuration

- [x] Config fields live as kong-tagged structs in `cmd/paperless-bank/main.go`
      (`PaperlessFlags`, `ComdirectFlags`) — not a separate `internal/config` package as
      originally sketched here
- [x] Config file loading via `kong-yaml` (default `~/.config/paperless-bank/config.yaml`,
      override with `PAPERLESS_BANK_CONFIG`)
- [x] Token cache file path (per-bank, configurable via `--comdirect-token-cache`)

### Bank interface

- [x] `Document`, `DocumentSource`, `Authenticator`, `ChallengeHandler`, `TokenChecker` in
      `internal/bank/interface.go`
- [x] Bank registry in `internal/bank/registry.go` (maps name → factory)

### Comdirect integration (`internal/bank/comdirect/`)

- [x] `auth.go` — full 6-step OAuth2 + TAN flow (see Architecture.md), all TAN types (P_TAN,
      P_TAN_PUSH with status polling, M_TAN), driven by `bank.ChallengeHandler`
- [x] `auth.go` — token cache read/write (JSON file)
- [x] `auth.go` — automatic token refresh before expiry
- [x] `client.go` — authenticated HTTP client with `x-http-request-info` header injection
- [x] `documents.go` — `ListDocuments` with pagination
- [x] `documents.go` — `DownloadDocument`
- [x] Unit tests with `httptest` for auth and document endpoints (`auth_test.go`,
      `documents_test.go`)

### paperless-ngx client (`internal/paperless/`)

- [x] `client.go` — token auth, `POST /api/documents/post_document/` multipart upload
- [x] `client.go` — query existing documents by original filename (duplicate check)
- [x] Unit tests with `httptest` (`client_test.go`)

### Sync orchestrator (`internal/sync/`)

- [x] `sync.go` — fetch → filter (MIME type, sync watermark) → deduplicate → upload loop
- [x] Per-document error collection; summary report at end
- [x] 3-attempt retry with exponential backoff for transient network errors (`internal/httpx`)

### CLI commands

- [x] `auth` command — interactive TAN prompt, writes token cache
- [x] `sync` command — runs orchestrator for all configured banks
- [x] `list` command — prints document list to stdout (no upload)
- [x] `serve` command — see **v2 — Server mode** below

### Packaging

- [x] `Makefile` with `build`, `test`, `vet`, `generate`, `clean` targets
- [x] `.gitignore` (binaries, token cache files, `*.env`)

---

## v2 — Server mode

- [x] `internal/server/` — sync-on-interval loop + minimal web UI for bank auth challenges
- [x] `serve` CLI command (`--listen`, `--sync-interval`, `--auto-reauth`)
- [x] Proactive token keep-alive loop, independent of `--sync-interval`
- [x] Sync watermark so recurring runs don't re-check years of already-synced history
- [ ] A way to be notified that a bank needs attention without polling the status page
      (e.g. a webhook/email hook when a challenge becomes pending)

## Future (out of scope for v1)

- Document classification / auto-tagging based on content or sender
- Additional bank integrations (DKB, ING, Sparkasse, …)
- paperless-ngx correspondent and document-type mapping from bank metadata
