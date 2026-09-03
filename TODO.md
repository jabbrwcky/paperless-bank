# TODO

## v1 — Comdirect → paperless-ngx

### Project scaffold

- [ ] `go mod init github.com/jabbrwcky/paperless-bank`
- [ ] Add `github.com/alecthomas/kong` dependency
- [ ] Basic `cmd/paperless-bank/main.go` with kong wiring and `--config` flag

### Configuration

- [ ] Define config structs in `internal/config/config.go` (PaperlessConfig, ComdirectConfig)
- [ ] Wire config file loading via kong's `kong.Configuration(kong.JSON, ...)` or YAML provider
- [ ] Token cache file path (per-bank, configurable)

### Bank interface

- [ ] Define `Document` struct and `DocumentSource` interface in `internal/bank/interface.go`
- [ ] Bank registry in `internal/bank/registry.go` (maps name → factory)

### Comdirect integration (`internal/bank/comdirect/`)

- [x] `auth.go` — full 6-step OAuth2 + TAN flow (see Architecture.md)
- [x] `auth.go` — token cache read/write (JSON file)
- [x] `auth.go` — automatic token refresh before expiry
- [x] `client.go` — authenticated HTTP client with `x-http-request-info` header injection
- [x] `documents.go` — `ListDocuments` with pagination
- [ ] `documents.go` — `DownloadDocument`
- [ ] Unit tests with `httptest` for auth and document endpoints

### paperless-ngx client (`internal/paperless/`)

- [ ] `client.go` — token auth, `POST /api/documents/` multipart upload
- [ ] `client.go` — query existing documents by original filename (duplicate check)
- [ ] Unit tests with `httptest`

### Sync orchestrator (`internal/sync/`)

- [ ] `sync.go` — fetch → deduplicate → upload loop
- [ ] Per-document error collection; summary report at end
- [ ] 3-attempt retry with exponential backoff for transient network errors

### CLI commands

- [ ] `auth` command — interactive TAN prompt, writes token cache
- [ ] `sync` command — runs orchestrator for all configured banks
- [ ] `list` command — prints document list to stdout (no upload)

### Packaging

- [ ] `Makefile` with `build`, `test`, `vet` targets
- [ ] `.gitignore` (binaries, token cache files, `*.env`)

---

## Future (out of scope for v1)

- Document classification / auto-tagging based on content or sender
- Additional bank integrations (DKB, ING, Sparkasse, …)
- Scheduled / daemon mode (run on interval without external cron)
- paperless-ngx correspondent and document-type mapping from bank metadata
