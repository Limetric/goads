# AGENTS.md

Google Ads MCP server + CLI, in Go. Single binary, all source in `package main`
at the repo root. The binary exposes two front-ends over one shared set of tool
handlers:

- **CLI** — `goads login`, `goads search …`, `goads accounts`, `goads budget set …` (for humans,
  scripts, CI, and the agent skill that drives it via shell).
- **MCP server** — `goads mcp` serves the same tools over stdio for MCP hosts
  (Claude Desktop, Cursor, …).

Each tool is defined once: a typed `Args` struct + a pure
`func(ctx, *Client, Args) (Result, error)` handler. The CLI wires flags → handler;
the MCP front-end registers the same handler with `mcp.AddTool`, which derives the
JSON input schema from the `Args` struct by reflection.

## Commands

**Go code must be formatted before every commit** — CI rejects unformatted code.

```bash
go fmt ./...                  # Format — run before commit
go mod tidy                   # Resolve/lock deps (run once after clone)
go build -o build/goads .     # Build binary
go vet ./...                  # Lint
go run honnef.co/go/tools/cmd/staticcheck ./...   # Static analysis
go test ./... -count=1        # Unit tests (no network; uses httptest)

# Live smoke test against the real API (requires real credentials)
GOOGLE_ADS_DEVELOPER_TOKEN=… GOOGLE_ADS_CLIENT_ID=… GOOGLE_ADS_CLIENT_SECRET=… \
GOOGLE_ADS_REFRESH_TOKEN=… GOOGLE_ADS_LOGIN_CUSTOMER_ID=… \
go test -tags integration -count=1 -v ./...
```

## Layout

- `main.go` — Cobra root command, subcommand wiring, `main()`.
- `config.go` / `config_paths.go` — credentials & settings (env first, optional TOML overlay).
- `auth.go` — OAuth2 token source (refresh-token flow via `golang.org/x/oauth2`).
- `client.go` — Google Ads **REST** client (`googleads.googleapis.com/v23`): `Search`, `Mutate`, etc. No gRPC, no protobuf.
- `gaql.go` — GAQL query building + validation.
- `safety.go` — write guards, mutation preview, and the confirm-token flow (`audit.go` equivalent).
- `mcp.go` — `goads mcp` subcommand; registers every tool with the MCP SDK.
- `tool_*.go` — one file per tool (`Args` + handler + CLI subcommand). Test lives next to it.

## Conventions (match these)

- All code is `package main` at the repo root. No `cmd/` or `internal/`.
- New tool = new `tool_<name>.go` + `tool_<name>_test.go`. Register it in **two** places:
  `init()` (CLI subcommand) and `registerTools` in `mcp.go` (MCP).
- Write/mutating tools MUST go through `safety.go`: return a preview + confirm token first,
  execute only on `--confirm <token>`. Never mutate on the first call.
- Errors: wrap with `%w`, and make messages actionable (tell the user the fix).
- Tests are table-driven and offline — use `net/http/httptest` to fake the Ads API
  (set `GOOGLE_ADS_API_BASE_URL` to the test server). `//go:build integration` for live tests.

## Key references

- Google Ads REST API (v23): <https://developers.google.com/google-ads/api/rest/overview>
- MCP Go SDK: <https://github.com/modelcontextprotocol/go-sdk>
