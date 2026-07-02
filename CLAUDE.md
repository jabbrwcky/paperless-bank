# Claude Code — paperless-bank

See [AGENTS.md](AGENTS.md) for agent and coding instructions.

## Quick reference

```
go build ./...          # build
go test ./...           # test
go vet ./...            # lint
```

## Project layout

```
cmd/paperless-bank/     # main package, kong CLI wiring
internal/
  bank/                 # Bank interface + per-bank implementations
    comdirect/          # Comdirect bank integration
  paperless/            # paperless-ngx upload client
  sync/                 # Orchestrates bank → paperless pipeline
  config/               # Shared config structs (kong)
```

See [Architecture.md](Architecture.md) for design decisions and flow diagrams.
See [TODO.md](TODO.md) for planned work.
