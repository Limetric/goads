# goads

**Google Ads campaign management — a Go CLI and MCP server.**

A Go port of the Rust [`FGRibreau/mcp-google-ads`](https://github.com/FGRibreau/mcp-google-ads).
It talks to the Google Ads **REST API** (no gRPC, no protobuf codegen), and ships
as a single binary with two front-ends over one shared set of tools:

- **CLI** — `goads search …`, `goads accounts`, `goads budget set …`. Scriptable,
  pipeable into `jq`, usable in CI. This is what the bundled agent **skill** drives.
- **MCP server** — `goads mcp` serves the same tools over stdio to MCP hosts
  (Claude Desktop, Cursor, …).

## Quick start

```bash
go mod tidy
go build -o build/goads .

export GOOGLE_ADS_DEVELOPER_TOKEN=...
export GOOGLE_ADS_CLIENT_ID=...
export GOOGLE_ADS_CLIENT_SECRET=...
export GOOGLE_ADS_REFRESH_TOKEN=...
export GOOGLE_ADS_LOGIN_CUSTOMER_ID=123-456-7890   # optional manager account

build/goads doctor            # verify credentials resolve
build/goads accounts          # list accessible accounts
build/goads search --customer-id 123-456-7890 \
  --query 'SELECT campaign.id, campaign.name FROM campaign LIMIT 10' | jq
```

Writes preview first, then apply with the returned token:

```bash
build/goads budget set --customer-id 123-456-7890 --budget-id 555 --amount-micros 5000000
# → prints a preview and a confirm token, e.g. a1b2c3d4e5f6a7b8
build/goads budget set --customer-id 123-456-7890 --budget-id 555 --amount-micros 5000000 \
  --confirm a1b2c3d4e5f6a7b8
```

## As an MCP server

Point your MCP host at the binary:

```json
{
  "mcpServers": {
    "goads": {
      "command": "/path/to/build/goads",
      "args": ["mcp"],
      "env": {
        "GOOGLE_ADS_DEVELOPER_TOKEN": "...",
        "GOOGLE_ADS_CLIENT_ID": "...",
        "GOOGLE_ADS_CLIENT_SECRET": "...",
        "GOOGLE_ADS_REFRESH_TOKEN": "...",
        "GOOGLE_ADS_LOGIN_CUSTOMER_ID": "..."
      }
    }
  }
}
```

## As a Claude Code skill

The bundled skill (`.claude/skills/goads/SKILL.md`) teaches an agent when and how
to drive the CLI — token-efficient because nothing loads until it's relevant, and
big result sets stay in the shell (`| jq`) instead of the context window.

## Status

Early scaffold. Three tools are wired end-to-end (`search`, `list_accounts`,
`set_campaign_budget`) to establish the patterns; the rest of the upstream tools
are tracked in [`docs/PORTING.md`](docs/PORTING.md). See [`AGENTS.md`](AGENTS.md)
for the contributor workflow and conventions.

## License

MIT
