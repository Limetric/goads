# Remaining work — goads

> **Agent handoff.** This is the plan for finishing the Go port of
> [`FGRibreau/mcp-google-ads`](https://github.com/FGRibreau/mcp-google-ads)
> (Rust, commit `084f2e4`, v0.4.1). Read this top to bottom before starting.
> The companion file [`PORTING.md`](PORTING.md) is the **status table** — update
> it as you go. This file is the **how**.
>
> **Status: complete.** All client endpoints, the mutate allow-list, guards,
> models, and every read/write tool in [`PORTING.md`](PORTING.md) are ported with
> offline tests (43 MCP tools / CLI commands). The milestones below are kept as a
> record of the approach. Live `//go:build integration` smoke tests live in
> `integration_test.go`. `go build`, `go vet`, `gofmt -l .`, `staticcheck`, and
> `go test ./...` are all green.

---

## 1. Read these first

1. [`AGENTS.md`](../AGENTS.md) — conventions and commands (authoritative).
2. [`PORTING.md`](PORTING.md) — what's done / what's left, with Rust→Go file mapping.
3. The three reference tools already wired end-to-end — **copy these patterns**:
   - `tool_search.go` — a read tool.
   - `tool_accounts.go` — a read tool that builds a GAQL query.
   - `tool_budget_write.go` — a **write** tool using the confirm-token flow.

Do **not** invent new structure. The project is a flat `package main` at the repo
root (no `cmd/`, no `internal/`), Cobra CLI + MCP server sharing one handler per tool.

## 2. Current state (verified)

Builds, vets clean, tests pass; CLI and MCP server both run. Baseline to preserve:

```bash
go build -o build/goads .     # must stay green
go vet ./...                  # must stay clean
go test ./... -count=1        # must stay green
gofmt -l .                    # must print nothing
```

Done: core plumbing (`config`, `auth`, `client` with `Search`+`Mutate`, `gaql`,
`safety`) and three tools (`search`, `list_accounts`, `set_campaign_budget`).
Everything else below is TODO.

## 3. Architecture you must follow

- **One tool = one `tool_<name>.go`.** It contains: an `Args` struct (with `json`
  and `jsonschema` tags), a `Result` struct, a pure handler
  `run<Name>(ctx, *Client, Args) (Result, error)`, a Cobra subcommand, and an
  `init()` that registers flags.
- **The `Args` struct is the single source of truth.** The CLI builds flags from
  it; the MCP SDK derives the JSON input schema from its `jsonschema` tags by
  reflection. Never hand-write a schema.
- **Reads** call `client.Search(ctx, customerID, gaql)`.
- **Writes never mutate on the first call.** They build mutate operations, stage
  them with `stageMutation(...)`, and return a preview + confirm token. The
  caller re-invokes with `Confirm: <token>`, which `consumeMutation(token)`
  validates (single-use, TTL) before `client.Mutate(...)` runs. Call
  `auditLog(p, applied)` on both success and failure. See `tool_budget_write.go`.
- **REST, not gRPC.** All API calls are JSON over HTTP via `client.post(...)`.
  There is no protobuf. Match the request/response JSON shapes in the
  [Google Ads REST v23 reference](https://developers.google.com/google-ads/api/rest/overview).
- **Customer IDs**: always pass through `normalizeCustomerID` (strips dashes).
- **Money is in micros** (1 unit = 1,000,000 micros).

## 4. How to port one tool (the recipe)

For each tool in the table in §6:

1. **Find the upstream handler.** Open the Rust file in
   `src/tools/<name>.rs` (raw URL pattern:
   `https://raw.githubusercontent.com/FGRibreau/mcp-google-ads/084f2e4aac98bfce453e14710288418429bdf30d/src/tools/<name>.rs`).
   Note its inputs, the GAQL it builds (reads) or the mutate operations it
   constructs (writes), and the output shape.
2. **Create `tool_<name>.go`** following the skeleton below.
3. **Register in three places:**
   - `init()` in the tool file → `searchCmd.Flags()...` style flag setup.
   - `main.go` `init()` → `rootCmd.AddCommand(<name>Cmd)`.
   - `mcp.go` `registerTools` → `addTool(server, client, "<mcp_name>", "<desc>", run<Name>)`.
   Keep the CLI command and the MCP tool list in sync — that's the one manual
   coupling in the codebase.
4. **Add `tool_<name>_test.go`** — table-driven, offline, using `httptest`
   (set the server URL as `Config.BaseURL`; see `tool_search_test.go` and
   `newTestClient`). Writes: test that the preview path stages a token and does
   **not** hit the mutate endpoint, and that the confirm path does.
5. **Run the baseline commands in §2.** All must stay green.
6. **Update [`PORTING.md`](PORTING.md)** status, and the skill
   (`.claude/skills/goads/SKILL.md`) if the tool is user-facing.

### Read-tool skeleton

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

type CampaignsArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max rows to return (0 = no limit)"`
}

type CampaignsResult struct {
	CustomerID string            `json:"customer_id"`
	RowCount   int               `json:"row_count"`
	Rows       []json.RawMessage `json:"rows"`
}

func runCampaigns(ctx context.Context, c *Client, args CampaignsArgs) (CampaignsResult, error) {
	if args.CustomerID == "" {
		return CampaignsResult{}, fmt.Errorf("customer_id is required")
	}
	q, err := buildSelect(
		[]string{"campaign.id", "campaign.name", "campaign.status"},
		"campaign", "", args.Limit,
	)
	if err != nil {
		return CampaignsResult{}, err
	}
	rows, err := c.Search(ctx, args.CustomerID, q)
	if err != nil {
		return CampaignsResult{}, toolError("campaigns", err)
	}
	return CampaignsResult{normalizeCustomerID(args.CustomerID), len(rows), rows}, nil
}

var campaignsArgs CampaignsArgs

var campaignsCmd = &cobra.Command{
	Use:   "campaigns",
	Short: "List campaigns in an account",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runCampaigns(cmd.Context(), client, campaignsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	campaignsCmd.Flags().StringVar(&campaignsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	campaignsCmd.Flags().IntVar(&campaignsArgs.Limit, "limit", 0, "max rows (0 = all)")
	_ = campaignsCmd.MarkFlagRequired("customer-id")
}
```

Then: `rootCmd.AddCommand(campaignsCmd)` in `main.go`, and
`addTool(server, client, "campaigns", "List campaigns in an account.", runCampaigns)`
in `mcp.go`.

### Write tools

Copy `tool_budget_write.go` verbatim and change three things: the `Args` fields,
the `operations()` method (build the correct mutate op JSON — verify the
`updateMask` field names against the REST reference), and the `summary` string.
Do not change the stage → preview → consume → mutate → audit flow.

## 5. Client endpoints still to implement (`client.go`)

The upstream server uses exactly five REST endpoints; two are done. Add these
following the `Search`/`Mutate` pattern (build path, `c.post(ctx, path, body, &out)`):

| Method to add               | Endpoint                                   | Used by |
|-----------------------------|--------------------------------------------|---------|
| `GenerateKeywordIdeas`      | `customers/{id}:generateKeywordIdeas`      | `keyword_planner` |
| `ApplyRecommendations`      | `customers/{id}/recommendations:apply`     | `recommendations` |
| `DismissRecommendations`    | `customers/{id}/recommendations:dismiss`   | `recommendations` |

## 6. Tools to port

Priority is a suggestion: get the common read tools working first (they unblock
useful queries), then the write tools (which all share the budget pattern).
Upstream source is `src/tools/<file>` at commit `084f2e4`. Sizes hint at effort.

### Reads (do first)

| Upstream file              | Size  | New Go file               | MCP/CLI name        |
|----------------------------|-------|---------------------------|---------------------|
| `campaigns.rs`             | 2.9K  | `tool_campaigns.go`       | `campaigns`         |
| `ads.rs`                   | 1.5K  | `tool_ads.go`             | `ads`               |
| `keywords.rs`              | 3.3K  | `tool_keywords.go`        | `keywords`          |
| `reporting.rs`             | 0.9K  | `tool_reporting.go`       | `report`            |
| `geo.rs`                   | 2.1K  | `tool_geo.go`             | `geo`               |
| `conversions.rs`           | 0.9K  | `tool_conversions.go`     | `conversions`       |
| `policy.rs`                | 0.9K  | `tool_policy.go`          | `policy`            |
| `keyword_planner.rs`       | 2.2K  | `tool_keyword_planner.go` | `keyword_ideas`     |
| `recommendations.rs`       | 7.4K  | `tool_recommendations.go` | `recommendations`   |
| `extensions.rs`            | 1.0K  | `tool_extensions.go`      | `extensions`        |

### Writes (share the confirm flow from `tool_budget_write.go`)

| Upstream file              | Size  | New Go file                | MCP/CLI name           |
|----------------------------|-------|----------------------------|------------------------|
| `campaigns_write.rs`       | 28K   | `tool_campaigns_write.go`  | `campaign create/update` |
| `extensions_write.rs`      | 18K   | `tool_extensions_write.go` | `extension *`          |
| `pmax.rs`                  | 16K   | `tool_pmax.go`             | `pmax *`               |
| `keywords_write.rs`        | 14K   | `tool_keywords_write.go`   | `keyword add/remove`   |
| `ads_write.rs`             | 13K   | `tool_ads_write.go`        | `ad *`                 |
| `ad_groups_write.rs`       | 11K   | `tool_ad_groups_write.go`  | `adgroup *`            |
| `bidding.rs`               | 9.8K  | `tool_bidding.go`          | `bidding *`            |
| `entity_lifecycle.rs`      | 9.1K  | `tool_entity_lifecycle.go` | `pause/enable/remove`  |
| `audiences.rs`             | 8.0K  | `tool_audiences.go`        | `audience *`           |
| `scheduling.rs`            | 7.7K  | `tool_scheduling.go`       | `schedule *`           |
| `assets.rs`                | 5.6K  | `tool_assets.go`           | `asset *`              |

> `campaigns_write.rs` is the largest single file and the highest-risk write —
> consider splitting it into create vs. update Go files. Port it after at least
> one other write tool so the pattern is muscle memory.

### Shared write-safety work (do before/with the first write tools)

- **Mutate operation allow-list.** Upstream `client.rs` validates mutate
  operations against ~88 allowed operation types before sending. Port that
  allow-list into `safety.go` (a `var allowedMutateOps = map[string]bool{...}`)
  and have `Mutate` (or a guard called before it) reject unknown operation
  keys. This is a real safety feature — don't skip it.
- **Guards/preview parity.** Re-read upstream `src/safety/guards.rs` and
  `src/safety/preview.rs` and confirm the Go `safety.go` matches their
  semantics (spend caps, destructive-op warnings, richer preview text). The Go
  version is currently minimal.

### Models / enums

Port `src/models/*` (`ad_status`, `ad_rotation_mode`, `next_action_hint`) as Go
string constants (e.g. `type AdStatus string` + consts) in a `models.go`, used by
the relevant tools. Don't over-engineer — strings with validation are fine.

## 7. Gotchas (read before touching the SDK or writes)

- **MCP SDK is pinned at `v0.2.0`.** Its API differs from the SDK's `main`-branch
  README. Handlers are `func(ctx, *mcp.ServerSession, *mcp.CallToolParamsFor[A]) (*mcp.CallToolResultFor[R], error)`,
  and stdio uses `mcp.NewStdioTransport()` (the zero-value `&mcp.StdioTransport{}`
  segfaults — nil `rwc`). All of this is already handled in `mcp.go`'s `addTool`;
  if you bump the SDK version, that function is the only place to adjust.
- **Writes must preview first.** If you ever call `client.Mutate` directly from a
  tool without going through `stageMutation`/`consumeMutation`, that's a bug.
- **Keep CLI and MCP in sync.** A new tool is not done until it's registered in
  both `main.go` `init()` and `mcp.go` `registerTools`.
- **Tests stay offline.** No live network in `go test`. Use `httptest`; gate any
  live test behind `//go:build integration`.
- **Verify mutate JSON against the REST reference**, not by guessing field names —
  `updateMask` paths and nested resource shapes must match exactly or the API
  rejects the call.

## 8. Definition of done (per tool)

- [ ] `tool_<name>.go` with `Args` (+`jsonschema` tags), `Result`, handler, CLI cmd.
- [ ] Registered in `main.go` `init()` **and** `mcp.go` `registerTools`.
- [ ] Writes go through the confirm-token flow + `auditLog`.
- [ ] `tool_<name>_test.go` covers happy path, an API error, and (writes) the
      preview-vs-apply split — all offline.
- [ ] `go build`, `go vet`, `go test`, `gofmt -l .` all clean.
- [ ] `PORTING.md` row updated; skill updated if user-facing.

## 9. Suggested milestones

1. **M1 — Reads:** port all §6 read tools + `GenerateKeywordIdeas`/recommendations
   endpoints. Now goads can answer most reporting questions.
2. **M2 — Write safety:** port the mutate allow-list and guard/preview parity.
3. **M3 — Writes:** port write tools, smallest first; `campaigns_write` last.
4. **M4 — Models + polish:** enums, richer previews, integration tests, README.
