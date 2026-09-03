# paperless-bank

A single-binary CLI that fetches documents from bank inbox/postbox APIs (Comdirect today, more
banks can be added) and uploads them to a [paperless-ngx](https://docs.paperless-ngx.com/)
instance. State lives entirely in paperless-ngx — documents already present (matched by original
filename) are skipped, so the tool can be re-run safely on a schedule.

See [Architecture.md](Architecture.md) for the design (component diagram, bank auth flow
details, sync flow, error handling) and [AGENTS.md](AGENTS.md) for coding conventions.

## Install

Requires Go 1.26+.

```sh
git clone https://github.com/jabbrwcky/paperless-bank.git
cd paperless-bank
make build      # builds ./paperless-bank
```

or install straight from the module:

```sh
go install github.com/jabbrwcky/paperless-bank/cmd/paperless-bank@latest
```

## Configure

Settings can come from a YAML config file, environment variables, or CLI flags (flags win over
env vars, which win over the config file).

By default the config file is read from `~/.config/paperless-bank/config.yaml`; override the
path with `PAPERLESS_BANK_CONFIG`.

```yaml
paperless:
  url: https://paperless.example.com
  token: YOUR_API_TOKEN

comdirect:
  client-id: YOUR_CLIENT_ID
  client-secret: YOUR_CLIENT_SECRET
  username: YOUR_USERNAME     # 8-digit Zugangsnummer
  password: YOUR_PASSWORD     # 6-digit PIN
  token-cache: ~/.cache/paperless-bank/comdirect-token.json
  tan-type: ""                # optional: P_TAN, P_TAN_PUSH, or M_TAN
```

The same settings are available as flags (`--paperless-url`, `--comdirect-client-id`, ...) or
environment variables (`PAPERLESS_URL`, `PAPERLESS_TOKEN`, `COMDIRECT_CLIENT_ID`,
`COMDIRECT_CLIENT_SECRET`, `COMDIRECT_USERNAME`, `COMDIRECT_PASSWORD`, `COMDIRECT_TAN_TYPE`).
Run `paperless-bank --help` for the full, current list.

Comdirect API credentials (`client-id`/`client-secret`) are issued under *persönlicher Bereich →
Verwaltung → API-Zugang* in your comdirect online banking.

## Use

```sh
# One-time interactive login; caches the session token to disk.
paperless-bank auth comdirect

# Preview what would be synced, without uploading anything.
paperless-bank list

# Fetch new documents from every configured bank and upload them to paperless-ngx.
paperless-bank sync
```

`auth` walks you through comdirect's TAN challenge (photoTAN scan, photoTAN push, or mobile/SMS
TAN — whichever your account returns) and stores the resulting token so `sync`/`list` can run
unattended afterwards; tokens are refreshed automatically and re-running `auth` is only needed
once the refresh token itself expires.

## Extend

### Add a bank

1. Create `internal/bank/<bankname>/` implementing `bank.DocumentSource`
   (`internal/bank/interface.go`) with, at minimum:
   - `auth.go` — OAuth2/session acquisition (implement `bank.Authenticator` too if login is
     interactive)
   - `client.go` — authenticated HTTP client
   - `documents.go` — `ListDocuments`/`DownloadDocument`
2. Register it in `internal/bank/registry.go` via `bank.Register("<bankname>", ...)` in an
   `init()`.
3. Add a `<Bankname>Flags` struct and wire it into `CLI`, `bankConfig`, and `configuredBanks` in
   `cmd/paperless-bank/main.go`, following the existing `ComdirectFlags` pattern.
4. Document the bank's auth flow quirks in `Architecture.md` under **Bank integrations**.

If the bank publishes an OpenAPI/Swagger spec, generate its low-level client instead of
hand-writing it — see below.

### Regenerate the Comdirect API client

`internal/bank/comdirect/client/` and `internal/bank/comdirect/models/` are generated from
[`comdirect_rest_api_swagger.json`](comdirect_rest_api_swagger.json) via
[go-swagger](https://github.com/go-swagger/go-swagger) (declared as a Go tool dependency in
`go.mod`; see `internal/bank/comdirect/generate.go` for the exact invocation). After updating the
spec file, regenerate with:

```sh
make generate
```

Note: today's hand-written `auth.go`/`documents.go` talk to the Comdirect REST API directly with
`net/http` rather than through this generated client — regenerate it to pick up spec changes
before extending or wiring more of the API through it.

### Paperless-ngx upload

The upload client (`internal/paperless/client.go`) speaks the paperless-ngx REST API (token auth,
`POST /api/documents/post_document/` multipart). Keep bank logic and paperless logic decoupled — `internal/sync`
is the only package that touches both.

## Develop

```sh
make build      # go build -o paperless-bank ./cmd/paperless-bank
make test       # go test ./...
make vet        # go vet ./...
make generate   # regenerate the Comdirect client from the OpenAPI spec
make clean      # remove the built binary
```

See [AGENTS.md](AGENTS.md) for code conventions (stdlib-first, error wrapping, test placement)
and the project's definition of done.

## Out of scope (v1)

- Document classification or tagging beyond filename/metadata from the bank
- Multi-user or server mode
- A persistent database (state is tracked via paperless-ngx itself)

See [TODO.md](TODO.md) for planned work.
