# Design: fool-proof interactive `goads login`

Date: 2026-06-23
Status: Approved (ready for implementation plan)
Builds on: 2026-06-23-goads-login-oauth-design.md (the existing `goads login`)

## Goal

Make first-time Google Ads setup fool-proof. Today `goads login` only runs the
OAuth flow and, when it can't find credentials, prints an error telling the user
to go create a Desktop-app client. A newcomer still has to discover, in the right
order: create a Google Cloud project, enable the Google Ads API, create a
Desktop-app OAuth client, run OAuth, obtain a developer token, and set a customer
/ MCC ID — and only then does `goads doctor` say "ready".

This turns `goads login` into a guided, interactive wizard that walks the user
through every prerequisite from scratch, collects everything needed to make real
API calls, and finishes with a live verification so the command ends in a known-
good state — not just "fields filled in".

## Non-goals

- Replacing the non-interactive path. CI, the `goads` agent skill (which drives
  the CLI over a pipe), and power users keep today's exact behavior.
- Automating the Google-side steps. The CLI cannot create a Cloud project or mint
  a developer token; it guides and collects, opening the right pages on request.
- A TUI / full-screen interface. This is a line-oriented prompt sequence.
- Changing `goads doctor`. It remains the quick health check; `login` becomes the
  full guided setup.

## Trigger & flags

`goads login` chooses a path at startup:

- **Interactive wizard** when ALL of: stdin is a TTY, `--no-input` is not set, and
  `--credentials` was not passed.
- **Non-interactive** (today's exact behavior) otherwise: non-TTY (pipe/CI/the
  agent skill), `--no-input`, or `--credentials <file>` (the explicit power path).

New flag:

- `--no-input` (bool) — force the non-interactive path even on a TTY.

Existing flags unchanged: `--credentials`, `--port`, `--no-browser`. `--port` and
`--no-browser` also apply inside the wizard's OAuth step.

TTY detection uses `term.IsTerminal(int(os.Stdin.Fd()))`.

## Wizard flow

A numbered sequence written to stdout. Each step that happens in a browser prints
instructions + the exact URL and asks `Open this in your browser now? [Y/n]`
(opening via the existing `openBrowser`), then waits on `Press Enter when done.`

```
Welcome to goads. Let's get you connected to Google Ads.
You'll need: a Google Cloud project, a Desktop-app OAuth client, and a
Google Ads developer token. I'll walk you through each — about 5 minutes.

Step 1/5 · Google Cloud project + Google Ads API
   → https://console.cloud.google.com/apis/library/googleads.googleapis.com
   Sign in, pick or create a project, and click Enable.
   Open this now? [Y/n]   … Press Enter when the API shows "Enabled".

Step 2/5 · Desktop-app OAuth client
   → https://console.cloud.google.com/apis/credentials
   Create Credentials → OAuth client ID → Application type "Desktop app" →
   Download JSON. (If asked, configure the OAuth consent screen first:
   https://console.cloud.google.com/apis/credentials/consent)
   Open this now? [Y/n]
   Path to the downloaded JSON: ~/Downloads/client_secret_….json
   ✓ Read client "…abc.apps.googleusercontent.com"

Step 3/5 · Sign in (browser)
   Opening Google sign-in… Waiting for callback on http://localhost:8085 …
   ✓ Signed in. Got your refresh token.

Step 4/5 · Developer token
   In Google Ads: Tools & Settings → API Center.
   → https://ads.google.com/aw/apicenter
   Open this now? [Y/n]
   Paste your developer token: ••••••••••••

Step 5/5 · Manager (MCC) account ID — optional
   If you manage accounts under an MCC, enter its ID; otherwise leave blank.
   Login customer ID [skip]:

Saving to ~/Library/Application Support/goads/config.toml …
Verifying… ✓ Connected — 3 accessible account(s): 123-456-7890, …

You're ready. Try:  goads accounts
```

The prerequisite URLs (canonical):

- Enable API: `https://console.cloud.google.com/apis/library/googleads.googleapis.com`
- Credentials: `https://console.cloud.google.com/apis/credentials`
- OAuth consent screen: `https://console.cloud.google.com/apis/credentials/consent`
- Developer token (API Center): `https://ads.google.com/aw/apicenter`

## Reuse / idempotency

The wizard loads the existing config first (via `loadLoginConfig`). For each value
already present it offers to keep it instead of re-prompting:

- client id/secret present → `Found an OAuth client (…abc). Keep it? [Y/n]`. If
  kept, Step 2 is skipped.
- refresh token present AND the OAuth client is being kept →
  `Reuse your existing sign-in, or sign in again? [reuse/new]`. `reuse` skips
  Step 3.
- developer token present → `Found a developer token (…xZ). Keep it? [Y/n]`.
- login customer id present → shown as the default in Step 5.

Re-running the wizard to fix one field is therefore fast and safe. Displayed
secrets are always truncated to a short suffix hint, never shown in full.

## Live verify

After writing the config, the wizard builds a `*Client` from the in-memory config
and calls a new method:

    func (c *Client) ListAccessibleCustomers(ctx context.Context) ([]string, error)

which does `GET {base}/v23/customers:listAccessibleCustomers` with the standard
auth + developer-token headers and parses `{"resourceNames":["customers/123",…]}`
into bare customer IDs. This endpoint requires only a valid OAuth token and a
developer token — no customer or MCC id — so it is the correct from-scratch
verification.

- Success → print the accessible account IDs (dash-formatted), then the
  "You're ready" hint.
- Failure → the config is already saved (nothing lost); print the error and the
  likely causes (developer token not yet approved / mistyped, or an OAuth
  problem), and tell the user to fix it and re-run `goads login`, or run
  `goads doctor`. The command exits non-zero so scripts notice, but the saved
  config remains.

## Architecture

### New file `login_wizard.go` (+ `login_wizard_test.go`)

Keeps the interactive orchestration out of `login.go` (already ~360 lines).

- `type prompter interface { line(prompt string) (string, error); secret(prompt string) (string, error); confirm(prompt string, def bool) (bool, error) }`
- `ttyPrompter` — real implementation. `line`/`confirm` read from stdin with
  `bufio`. `secret` uses `term.ReadPassword(int(os.Stdin.Fd()))` when stdin is a
  terminal, falling back to a plain line read otherwise. Writes prompts to an
  `io.Writer` (the command's stdout).
- `runLoginWizard(ctx context.Context, out io.Writer, p prompter, cfg *Config, openFn func(string) error, port int) error` —
  the orchestration. Takes the already-loaded `cfg` (for reuse), the prompter, the
  RAW browser opener `openFn` (production = `openBrowser`, or a print-the-URL
  closure under `--no-browser`; tests = a fake that fires the loopback callback),
  and the loopback port. Returns after writing config and verifying.
- The OAuth sign-in step (Step 3) calls `openFn` directly (auto-opens, the natural
  flow). The *prerequisite* pages (Steps 1, 2, 4) go through `offerToOpen`, which
  is the only place that asks `Open now? [Y/n]` before invoking `openFn`. Under
  `--no-browser`, `openFn` prints the URL instead of opening, and `offerToOpen`
  skips its prompt and just prints.
- Small step helpers, each independently testable:
  - `wizardGatherClient(p, out, cfg, openFn) (clientCreds, error)` — reuse-or-prompt
    for the JSON path, parse via `parseCredentialsJSON`, re-prompt on a bad path.
  - `wizardGatherRefreshToken(ctx, p, out, creds, cfg, openFn, port) (string, error)`
    — reuse-or-run the OAuth loopback (`runLoopbackOAuth` + `exchangeRefreshToken`).
  - `wizardGatherDeveloperToken(p, out, cfg) (string, error)` — masked secret,
    reuse-or-prompt, non-empty.
  - `wizardGatherLoginCustomerID(p, out, cfg) (string, error)` — optional,
    normalized via `normalizeCustomerID`.
  - `offerToOpen(p, out, label, url, openFn)` — print + `Open now? [Y/n]` + open +
    `Press Enter when done.`

### `login.go`

- `loginCmd.RunE` gains the branch: compute `interactive := isInteractiveLogin()`
  (TTY && !`loginNoInput` && `loginCredentialsPath == ""`); if interactive, load
  config via `loadLoginConfig`, pick the raw `openFn` (`openBrowser`, or a
  print-URL closure when `--no-browser`), and call `runLoginWizard`; else run the
  existing non-interactive flow unchanged.
- Add the `--no-input` flag (`loginNoInput`).
- `var googleOAuthEndpoint = google.Endpoint` replaces the inline `google.Endpoint`
  use, so tests can point the OAuth token exchange at an `httptest` server and
  drive the whole wizard offline. Production value is unchanged.

### `client.go`

- Add `ListAccessibleCustomers(ctx)`. It reuses `buildHeaders` (auth +
  developer-token + optional login-customer-id) and the base URL, issuing a GET
  and decoding `resourceNames`. A small `get` helper mirrors the existing `post`.

### Dependency

- New: `golang.org/x/term` (masked input). Small, official `golang.org/x` module;
  the only added dependency. `go mod tidy` after.

## Error handling

- Bad JSON path (not found / unparseable / wrong shape) → print the reason and
  re-prompt; does not abort the wizard.
- Empty required value (developer token) → re-prompt.
- `Ctrl-C` / EOF at any prompt → return an error that aborts cleanly; no partial
  config is written (config is written once, after all collection succeeds).
- A `web`-type client → warn (as today) but proceed.
- Verify failure → config already saved; print cause + next steps; exit non-zero.
- All errors wrap with `%w` and stay actionable.

## Testing (offline, table-driven)

- `fakePrompter` returns scripted answers and records prompts; drives every step.
- `ListAccessibleCustomers` — `httptest` server returns a `resourceNames` body;
  assert parsed/dash-stripped IDs; assert headers (developer-token) are set.
- `wizardGatherClient` — fake prompter returns a path to a temp JSON fixture
  (installed/web/bad); assert parse, the web-warning, and re-prompt on a bad path.
- `wizardGatherDeveloperToken` / `wizardGatherLoginCustomerID` — reuse path,
  fresh path, normalization, empty re-prompt.
- Reuse logic — existing config values are offered and kept on `y`.
- One full happy-path `runLoginWizard` run, fully offline: fake prompter +
  fake `openFn` that fires the loopback callback + `httptest` for the OAuth token
  endpoint (via `googleOAuthEndpoint`) and for `listAccessibleCustomers` (via
  `GOOGLE_ADS_API_BASE_URL`); assert config written with all fields and the
  "Connected" line printed.
- The non-interactive path keeps its existing tests; add one asserting `--no-input`
  on a (simulated) non-interactive invocation still takes the non-interactive
  branch.

## Wiring checklist

- `--no-input` flag registered in `login.go`'s `init()`.
- `runLoginWizard` is reached only from `loginCmd`; not an MCP tool.
- `go fmt`, `go vet`, `staticcheck`, `go test ./...`, `go mod tidy` all clean
  before commit.
