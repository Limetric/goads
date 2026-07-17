# goads

[![CI](https://github.com/Limetric/goads/actions/workflows/ci.yml/badge.svg)](https://github.com/Limetric/goads/actions/workflows/ci.yml)

**Google Ads campaign management — a Go CLI and MCP server.**

It talks to the Google Ads **REST API** (no gRPC, no protobuf codegen), and ships
as a single binary with two front-ends over one shared set of tools:

- **CLI** — `goads search …`, `goads accounts`, `goads budget set …`. Scriptable,
  pipeable into `jq`, usable in CI. This is what the bundled agent **skill** drives.
- **MCP server** — `goads mcp` serves the same tools over stdio to MCP hosts
  (Claude Desktop, Cursor, …).

## Quick start

On macOS and Linux, install `goads` from the Limetric Homebrew tap:

```bash
brew install Limetric/tap/goads
```

The fastest way to get set up is the guided sign-in — it walks you through the
Google Cloud + developer-token prerequisites, signs you in via the browser, and
verifies the connection:

```bash
goads login                  # interactive: guides you from scratch, then verifies
```

Prefer to wire it up manually (or in CI)? Set the environment directly and skip
the wizard with `--no-input`:

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

Work with one account most of the time? Set a default once and drop
`--customer-id` everywhere (any explicit flag still wins):

```bash
goads config set-customer 123-456-7890   # or: export GOOGLE_ADS_CUSTOMER_ID=123-456-7890
goads campaigns                          # uses the default account
goads config show                        # see the resolved config (secrets redacted)
```

Read commands print JSON by default; pass `--format table` or `--format csv`
for human- or spreadsheet-friendly output (`campaigns`, `ads`, `keywords …`,
`search`, `report`, and the other row-returning reads all take it):

```bash
goads campaigns --format table
```

Writes preview first, then apply with the returned token:

```bash
goads budget set --budget-id 555 --amount-micros 5000000
# → prints a preview and a confirm token, e.g. a1b2c3d4e5f6a7b8
goads confirm a1b2c3d4e5f6a7b8   # applies the staged change as previewed
goads audit                      # log of every write goads has applied
```

(Re-running the original command with `--confirm <token>` still works too.)

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

The repo bundles a skill (`plugins/goads/skills/goads/SKILL.md`, symlinked at
`.claude/skills/goads` for contributors working in a clone) that teaches an agent
when and how to drive the CLI — token-efficient because nothing loads until it's
relevant, and big result sets stay in the shell (`| jq`) instead of the context
window.

If you installed `goads` via Homebrew and don't have the repo cloned, install the
skill as a Claude Code plugin instead:

```text
/plugin marketplace add Limetric/goads
/plugin install goads@goads
```

## As a Codex plugin

Codex reads the same skill through its own plugin manifest:

```bash
codex plugin marketplace add Limetric/goads
codex plugin add goads@goads
```

## Tool coverage

Comprehensive campaign management plus first-class App campaign creation is available
(48 MCP tools / equivalent CLI commands):

- **Reads** — `search`, `report`, `accounts` (+ `accounts info` for currency/time
  zone), `campaigns`, `ads`, keyword performance / search terms / negatives,
  `geo` search + performance, `conversions`, `policy`, `extensions`, Keyword
  Planner ideas + forecasts, and recommendations listing. Row-returning reads
  render as `--format json|table|csv`.
- **Writes** (all preview-then-confirm) — Search, App, and Performance Max
  campaign create/update, ad group
  create/update, RSA drafting, keyword add/remove (+ negatives), bidding
  strategies + keyword bids, sitelink/callout/snippet extensions, audiences,
  image/text assets, ad scheduling, Performance Max campaigns, pause/enable/remove,
  and recommendation apply/dismiss.

Write safety: every mutation previews first and applies only on confirm
(`goads confirm <token>` or re-running with `--confirm`); `goads audit` shows
every applied write; a client-side allow-list rejects invalid `googleAds:mutate`
operation keys; and guard rails (spend cap, bid-increase limit, blocked-op list)
are configurable via `GOOGLE_ADS_MAX_DAILY_BUDGET`,
`GOOGLE_ADS_MAX_BID_INCREASE_PCT`, and `GOOGLE_ADS_BLOCKED_OPS`. New
campaigns/ad groups/ads ship **PAUSED** by default.

## Shell completion

Homebrew installs completions automatically. For a manual install, `goads
completion` generates the script for your shell:

```bash
# bash (requires bash-completion)
goads completion bash > /usr/local/etc/bash_completion.d/goads

# zsh
goads completion zsh > "${fpath[1]}/_goads"

# fish
goads completion fish > ~/.config/fish/completions/goads.fish
```

See [`AGENTS.md`](AGENTS.md) for the contributor workflow and conventions.

## License

Apache-2.0. See [`LICENSE`](LICENSE).
