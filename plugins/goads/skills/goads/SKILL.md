---
name: goads
description: Use when working with Google Ads — querying campaigns/keywords/metrics with GAQL, listing accounts, or changing budgets and other settings. Drives the `goads` CLI. Triggers include "google ads", "GAQL", "campaign budget", "ad spend", "what's my CPC/CTR/impressions", or any read/write against a Google Ads account.
---

# goads — Google Ads via the `goads` CLI

`goads` is a single binary that talks to the Google Ads REST API. Prefer it over
ad-hoc HTTP. Drive it from the shell; pipe large results through `jq` so they
never have to land in full in context.

## Setup check

Run this first if anything errors with missing credentials:

```bash
goads doctor
```

If it reports `NOT READY`, the required `GOOGLE_ADS_*` environment variables are
not set — ask the user to provide them; do not invent values.

## Reading data (GAQL)

`search` runs a [GAQL](https://developers.google.com/google-ads/api/docs/query/overview)
query and prints result rows as JSON. Always pass `--customer-id`.

```bash
goads search --customer-id 123-456-7890 \
  --query 'SELECT campaign.id, campaign.name, metrics.impressions, metrics.cost_micros
           FROM campaign WHERE segments.date DURING LAST_7_DAYS ORDER BY metrics.cost_micros DESC' \
  | jq '.rows[].campaign'
```

Tips:
- Filter and aggregate with `jq` before summarizing — don't dump every row.
- Costs are in **micros** (1,000,000 micros = 1 unit of currency).
- `goads accounts` lists the customer IDs you can reach.

## Making changes (always previews first)

Write commands are **two-step**: the first call returns a preview and a
`confirm_token`; nothing changes until you re-run with `--confirm <token>`.
Show the preview to the user and get their go-ahead before confirming.

```bash
# 1. Preview — read confirm_token from the output
goads budget set --customer-id 123-456-7890 --budget-id 555 --amount-micros 5000000

# 2. Apply only after the user agrees
goads budget set --customer-id 123-456-7890 --budget-id 555 --amount-micros 5000000 \
  --confirm <token-from-step-1>
```

Never skip the preview, never guess a token, and never confirm a write the user
hasn't approved.

## Command reference

**Reads** (print JSON; pipe through `jq`):

| Command | What it shows |
|---|---|
| `search` / `report` | run a GAQL query (`report` adds `--format table\|csv`) |
| `accounts` | accessible customer IDs |
| `campaigns` / `ads` | campaign- / ad-level performance |
| `keywords performance` / `keywords search-terms` / `keywords negative` | keyword metrics, search terms, negatives |
| `geo search` / `geo performance` | find location IDs / geo performance |
| `conversions` / `policy` / `extensions` | conversion actions / policy issues / extensions |
| `keyword-ideas` / `keyword-forecasts` | Keyword Planner ideas / recent metrics |
| `recommendations list` | active recommendations |

**Writes** (two-step preview → `--confirm <token>`):

| Command | Action |
|---|---|
| `budget set` | set a campaign budget |
| `campaign create` / `campaign update` | draft / update a campaign |
| `adgroup create` / `adgroup update` | create / update an ad group |
| `ad draft-rsa` | draft a Responsive Search Ad |
| `keywords add` / `add-negative` / `remove` / `remove-negative` | manage keywords |
| `bidding create-strategy` / `bidding set-keyword-bid` | portfolio strategy / keyword bid |
| `extension sitelinks\|callouts\|snippets\|remove` | manage extensions |
| `audience create` / `audience target` | custom audiences / targeting |
| `asset image` / `asset text` | upload assets |
| `schedule` | set ad schedules |
| `pmax create` | create a Performance Max campaign |
| `pause` / `enable` / `remove` | change entity status (`remove` is destructive) |
| `recommendations apply` / `recommendations dismiss` | act on recommendations |

New entities (campaigns, ad groups, ads, PMax) are created **PAUSED** by default;
the preview's `next_action_hint` shows how to `enable` them afterward.

## Discovering commands

```bash
goads --help            # all commands
goads <command> --help  # flags for one command
```

Use `--help` to learn a command's flags rather than guessing.
