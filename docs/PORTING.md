# Porting status — Rust → Go

Tracks the port of [`FGRibreau/mcp-google-ads`](https://github.com/FGRibreau/mcp-google-ads)
(commit `084f2e4`, v0.4.1) to this Go implementation.

## Core (done)

| Rust source            | Go file(s)                     | Notes |
|------------------------|--------------------------------|-------|
| `src/main.rs`          | `main.go`, `mcp.go`            | stdio MCP is now the `goads mcp` subcommand; CLI added alongside. |
| `src/config.rs`        | `config.go`, `config_paths.go` | env-first + optional TOML; `GOOGLE_ADS_*`. |
| `src/auth.rs`          | `auth.go`                      | `yup-oauth2` → `golang.org/x/oauth2` refresh-token flow. |
| `src/client.rs`        | `client.go`                    | REST client; all 5 endpoints done + mutate op allow-list. |
| `src/gaql.rs`          | `gaql.go`, `gaql_format.go`    | validation + escaping + builder + cost/table/csv helpers. |
| `src/error.rs`         | (inline `fmt.Errorf`/`%w`)     | no separate error type yet. |
| `src/safety/*`         | `safety.go`, `guards.go`       | guards + preview + confirm-token + audit + op allow-list. |
| `src/models/*`         | `models.go`                    | enums → string consts. |

## Client endpoints

| Endpoint                                   | Method                       | Status |
|--------------------------------------------|------------------------------|--------|
| `customers/{id}/googleAds:search`          | `Client.Search`              | done   |
| `customers/{id}/googleAds:mutate`          | `Client.Mutate`              | done   |
| `customers/{id}:generateKeywordIdeas`      | `Client.GenerateKeywordIdeas`| done   |
| `customers/{id}/recommendations:apply`     | `Client.ApplyRecommendations`| done   |
| `customers/{id}/recommendations:dismiss`   | `Client.DismissRecommendations`| done |

## Tools

Each tool = one `tool_<name>.go` (Args struct + handler + CLI subcommand) and a
registration in `registerTools` (mcp.go). Write tools must use the confirm flow.

| Rust source                      | Go file                  | Kind  | Status |
|----------------------------------|--------------------------|-------|--------|
| `tools/accounts.rs`              | `tool_accounts.go`       | read  | done   |
| (search / run_gaql)              | `tool_search.go`         | read  | done   |
| `tools/budget.rs` (+ write)      | `tool_budget_write.go`   | write | done   |
| `tools/campaigns.rs`             | `tool_campaigns.go`      | read  | done   |
| `tools/campaigns_write.rs`       | `tool_campaigns_write.go`| write | todo   |
| `tools/ad_groups_write.rs`       | `tool_ad_groups_write.go`| write | todo   |
| `tools/ads.rs` / `ads_write.rs`  | `tool_ads*.go`           | both  | todo   |
| `tools/keywords.rs` / `_write`   | `tool_keywords*.go`      | both  | todo   |
| `tools/keyword_planner.rs`       | `tool_keyword_planner.go`| read  | done   |
| `tools/bidding.rs`               | `tool_bidding.go`        | write | done   |
| `tools/assets.rs`                | `tool_assets.go`         | both  | done   |
| `tools/audiences.rs`             | `tool_audiences.go`      | both  | done   |
| `tools/extensions*.rs`           | `tool_extensions*.go`    | both  | todo   |
| `tools/pmax.rs`                  | `tool_pmax.go`           | both  | todo   |
| `tools/geo.rs`                   | `tool_geo.go`            | read  | done   |
| `tools/conversions.rs`           | `tool_conversions.go`    | both  | done   |
| `tools/recommendations.rs`       | `tool_recommendations.go`| both  | done   |
| `tools/reporting.rs`             | `tool_reporting.go`      | read  | done   |
| `tools/scheduling.rs`            | `tool_scheduling.go`     | write | done   |
| `tools/entity_lifecycle.rs`      | `tool_entity_lifecycle.go`| write| done   |
| `tools/policy.rs`                | `tool_policy.go`         | read  | done   |
| `tools/confirm.rs`               | folded into `safety.go`  | —     | done   |

## Porting a tool — checklist

1. Create `tool_<name>.go`: an `Args` struct (with `json` + `jsonschema` tags),
   a `Result` struct, and a `run<Name>(ctx, *Client, Args) (Result, error)` handler.
2. Reuse `Client.Search` for reads; for writes, build mutate ops and route them
   through `stageMutation` / `consumeMutation` (never mutate on first call).
3. Add a CLI subcommand in the same file's `init()`, and register the tool in
   `registerTools` (mcp.go). Add the subcommand to `main.go`'s `init()`.
4. Add `tool_<name>_test.go`: table-driven, offline via `httptest` (set the test
   server URL as `BaseURL`).
5. Update this file's status and the skill (`.claude/skills/goads/SKILL.md`).
