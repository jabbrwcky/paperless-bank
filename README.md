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

# Run continuously: sync on an interval, plus a web UI at the --listen address
# for completing an auth challenge when no terminal is attached.
paperless-bank serve --sync-interval 1h

# Print a shell completion script (bash, zsh, or fish).
paperless-bank completion bash > /etc/bash_completion.d/paperless-bank
```

`auth` walks you through comdirect's TAN challenge (photoTAN scan, photoTAN push, or mobile/SMS
TAN — whichever your account returns) and stores the resulting token so `sync`/`list` can run
unattended afterwards; tokens are refreshed automatically and re-running `auth` is only needed
once the refresh token itself expires.

`sync` only uploads documents whose MIME type is in `--mime-types`/`PAPERLESS_BANK_MIME_TYPES`
(comma-separated, default `application/pdf`) — paperless-ngx rejects file types it can't consume,
and bank inboxes mix in things like `text/html` marketing notices alongside PDF statements.
Documents of any other type are skipped, not treated as errors. `sync` also skips documents that
already exist in paperless-ngx (matched by original filename), so it's safe to re-run on a
schedule without creating duplicates.

Every run also saves a per-bank sync watermark (`~/.cache/paperless-bank/<bank>-sync-state.json`,
alongside the token cache) recording the newest document date it fully accounted for. Later runs
skip documents strictly older than that watermark before even checking paperless-ngx for them —
without it, every run would re-list and re-check a bank's entire history, which only gets more
wasteful as years of statements pile up. This is purely a performance optimization; the
filename-based duplicate check above remains the actual safety net, so it's safe even if a
watermark file is stale, corrupted, or deleted.

`serve` binds to `127.0.0.1:8080` by default (`--listen` to change it) and has no built-in
login — if you expose it beyond localhost, put it behind your own authenticating reverse proxy,
the same way you'd typically deploy paperless-ngx itself. It also proactively refreshes each
bank's token every 5 minutes (independent of `--sync-interval`), so a long sync interval doesn't
leave a refreshable token to go stale between runs. By default, if a bank's cached token is
invalid or expired anyway (e.g. the refresh token itself has expired), `serve` just marks it
"action required" on the status page and waits for you to run `paperless-bank auth <bank>` — it
does **not** attempt a live login on its own. Pass `--auto-reauth` to have it instead start that
login itself and surface the resulting challenge at `/auth/<bank>` in the browser. Only enable
this if you're comfortable with `serve` being able to trigger a real bank login (and a real
SMS/push TAN to your phone) automatically — this is an explicit opt-in specifically because it's
easy to trigger unintentionally if real credentials are sitting in the environment (e.g. via
`.envrc`/direnv) when `serve` starts.

## Extend

### Add a bank

1. Create `internal/bank/<bankname>/` implementing `bank.DocumentSource`
   (`internal/bank/interface.go`) with, at minimum:
   - `auth.go` — OAuth2/session acquisition. If login is interactive, implement
     `bank.Authenticator` and drive the interactive step through the `bank.ChallengeHandler`
     passed into `Authenticate`, and implement `bank.TokenChecker` so callers (including `serve`)
     can check/refresh a cached token without user interaction. Never read/write a terminal
     directly from this package — that's what `ChallengeHandler` is for.
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
- Multi-user (single set of bank/paperless-ngx credentials only)
- A persistent database (state is tracked via paperless-ngx itself; `serve`'s own status/challenge
  state is in-memory only and does not survive a restart)

See [TODO.md](TODO.md) for planned work.
