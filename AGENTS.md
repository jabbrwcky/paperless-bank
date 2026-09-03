# Agent Instructions

## Project

`paperless-bank` is a Go CLI tool that fetches documents from bank inbox/postbox APIs and uploads them to a [paperless-ngx](https://docs.paperless-ngx.com/) instance.

For commit messages use conventional commit format.

Changes should be added via brnahce sand PRs to simplify review and merge.ß

## Code conventions

- **Go standard library first.** Reach for third-party packages only when the stdlib is genuinely insufficient. Exceptions: `github.com/alecthomas/kong` and its sub-packages for CLI and config file parsing.
- Format with `gofmt`. Run `go vet ./...` before declaring work done.
- Prefer flat packages over deep nesting. Keep `internal/` for packages not meant to be imported by external callers.
- Error handling: wrap errors with `fmt.Errorf("context: %w", err)`. No `panic` outside of `main` init.
- No global state. Pass dependencies explicitly via structs.
- Interfaces belong in the package that **uses** them, not the package that implements them.
- Tests live alongside the code they test (`foo_test.go` in the same package or `foo_test` package for black-box tests). Use `net/http/httptest` for HTTP mocking — no test-double libraries.

### Definition of Done

1. For new code there is sufficient test coverage >75%
2. Code is formatted with `gofmt` and `go vet` passes without errors
3. Code is documented with GoDoc-style comments
4. Code is linted with `golangci-lint` and passes without errors
5. Code is ready for review and merge
6. Design documentation is up-to-date
7. User documentation is up-to-date
8. Architectural documentation is up to date. Architectural decisions are captured in ADRs (Architecture decision records). Changes are captures in new ADRs, noting the superseded ADR.

## Adding a new bank

1. Create `internal/bank/<bankname>/` with at minimum:
   - `auth.go` — OAuth2 / session acquisition
   - `client.go` — authenticated HTTP client
   - `documents.go` — implements `bank.DocumentSource`
2. Register the bank in `internal/bank/registry.go`.
3. Add a configuration sub-struct in `internal/config/config.go` mirroring the existing `ComdirectConfig` pattern.
4. Document the bank's auth flow quirks in `Architecture.md` under **Bank integrations**.

## Paperless-ngx upload

The upload client lives in `internal/paperless/`. It speaks the paperless-ngx REST API (token auth, `POST /api/documents/post_document/` multipart — `/api/documents/` itself is read-only). Do not couple bank logic to paperless logic — the sync orchestrator in `internal/sync/` is the only place that touches both.

## CLI shape

Commands are defined via kong struct tags in `cmd/paperless-bank/main.go`. New top-level commands get their own file in `cmd/paperless-bank/` (e.g. `cmd_auth.go`, `cmd_sync.go`). Keep `main.go` to wiring only.

## What is out of scope (v1)

- Document classification or tagging beyond filename/metadata from the bank
- Multi-user or server mode
- Anything requiring a persistent database (state is tracked via paperless-ngx itself)
