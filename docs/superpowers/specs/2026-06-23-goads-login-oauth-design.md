# Design: `goads login` — native OAuth2 sign-in

Date: 2026-06-23
Status: Approved (ready for implementation plan)

## Goal

Make Google Ads sign-in seamless. Add a native Go subcommand, `goads login`,
that runs the loopback OAuth2 flow without external tooling and writes the
result straight into the goads config so subsequent commands "just work".

This replaces the multi-tool bash script with one self-contained command using
the `golang.org/x/oauth2` dependency goads already has.

## Non-goals

- Collecting the **developer token** or **login-customer-id**. Those are not part
  of OAuth. They remain env/config values; `goads doctor` already reports when
  they are missing. (Decided during brainstorming: OAuth-only scope.)
- A device-code or service-account flow. Loopback redirect only (matches the
  script; OOB was removed by Google in 2022).
- Headless/remote sign-in. The loopback flow is inherently same-machine.
  `--no-browser` prints the URL but the callback still binds to localhost on the
  machine running the command.

## Command & UX

New CLI-only subcommand `goads login`. It is **not** registered as an MCP tool —
interactive browser auth over stdio makes no sense.

Happy path:

```
$ goads login --credentials ~/Downloads/client_secret_xxx.json
=== Google Ads OAuth2 sign-in ===
Opening browser for Google sign-in…
Waiting for callback on http://localhost:8085 …
✓ Authorized. Exchanged code for refresh token.
✓ Wrote credentials to ~/Library/Application Support/goads/config.toml

For CI / MCP host config, set:
  export GOOGLE_ADS_CLIENT_ID="…"
  export GOOGLE_ADS_CLIENT_SECRET="…"
  export GOOGLE_ADS_REFRESH_TOKEN="…"

Run `goads doctor` to verify. (developer token still required.)
```

Flags:

- `--credentials <path>` — path to the Desktop-app OAuth client JSON downloaded
  from Google Cloud Console.
- `--port <int>` — loopback port (default `8085`, matching the script).
- `--no-browser` — print the auth URL instead of auto-opening a browser.

## Credential input (priority order)

The flow needs a Desktop-app `client_id` + `client_secret`:

1. `--credentials file.json` if given → parse and use.
2. Else existing `GOOGLE_ADS_CLIENT_ID` / `GOOGLE_ADS_CLIENT_SECRET` from the
   already-loaded `Config` (env or TOML). Lets `goads login` run with zero args
   once client creds are configured.
3. Else a clear, actionable error pointing to Cloud Console → Create Credentials
   → OAuth 2.0 Client ID → **Desktop app**.

### credentials.json shapes (mirror the script)

- `installed` block present → Desktop app. Use `client_id` / `client_secret`. ✅
- `web` block present → Web-application client. Warn that loopback wants a
  Desktop app, but proceed using its `client_id` / `client_secret`.
- `type == "authorized_user"` → already contains a refresh token; treat its
  `client_id` / `client_secret` / `refresh_token` as the result and skip the
  browser flow entirely (write/print directly).
- none of the above → error "unrecognized credentials format".

## Output (write AND print)

On success:

1. **Write** `client_id`, `client_secret`, `refresh_token` into the TOML config at
   the resolved default path (`userConfigDir()/config.toml`), preserving any
   existing fields (`developer_token`, `login_customer_id`, `base_url`). File mode
   `0600`, directory `0700`.
2. **Print** `export GOOGLE_ADS_*` lines for CI / MCP host config, plus the path
   written and a nudge to run `goads doctor`.

If `--config <path>` was passed globally, write to that path instead of the
default (respect the existing flag).

## Architecture — new file `login.go` (+ `login_test.go`)

No new dependencies. Units, each independently testable:

- `parseCredentialsJSON(data []byte) (creds clientCreds, err error)`
  Pure parser. Returns client_id, client_secret, a `kind`
  (`installed`/`web`/`authorized_user`), and for `authorized_user` also the
  embedded refresh_token. Errors on unknown shape.

- `resolveLoginCreds(cfg *Config, credentialsPath string) (clientCreds, error)`
  Applies the priority order above.

- `runLoopbackOAuth(ctx, conf *oauth2.Config, openFn func(string) error, port int) (code string, err error)`
  - Generates a random `state` with `crypto/rand`.
  - Builds the auth URL via `conf.AuthCodeURL(state, AccessTypeOffline,
    ApprovalForce)` (`access_type=offline` + `prompt=consent` to guarantee a
    refresh token).
  - Starts an `http.Server` bound to `127.0.0.1:<port>` with a single handler.
  - Calls the injected `openFn(authURL)` (real impl = `openBrowser`; tests inject
    a function that fires the callback request).
  - Handler validates `state`, captures `?code=`, surfaces `?error=`, responds
    with a success/failure HTML page, and signals the result over a channel.
  - Honors `ctx` and a ~2-minute timeout; shuts the server down cleanly.

- `conf.Exchange(ctx, code)` → read `.RefreshToken` off the token. (For
  `authorized_user` creds this step is skipped.)

- `writeOAuthToConfig(path string, c clientCreds, refreshToken string) error`
  Decodes the existing file (if any) into a `Config`, sets the three OAuth fields,
  re-encodes only non-empty fields to TOML, writes atomically at `0600` (dir
  `0700` via `MkdirAll`).

- `openBrowser(url string) error`
  `runtime.GOOS` switch: darwin `open`, linux `xdg-open`, windows
  `rundll32 url.dll,FileProtocolHandler`.

- `loginCmd` (cobra) wires the flags → units, prints the summary. Registered in
  `init()` in `main.go` only.

### Redirect URI consistency

`conf.RedirectURL` and the value used at exchange must be byte-identical to what
the handler listens on: `http://localhost:<port>` (no trailing slash, matching
the script). The handler serves the root path.

## Error handling

All wrapped with `%w`, messages tell the user the fix:

- credentials file missing / unreadable → path + how to download a Desktop-app
  client.
- `web` client → warn (proceed); unknown shape → hard error.
- no `refresh_token` in the exchange response → reproduce the script's hint list:
  wrong client type, redirect URI not allowed, Google Ads API not enabled.
- callback timeout → "no authorization received within 2m — did you approve in
  the browser?"
- port in use → "localhost:<port> is busy; pass --port".

## Testing (offline, table-driven)

- `parseCredentialsJSON` — installed / web / authorized_user / unknown fixtures.
- `resolveLoginCreds` — file-wins, env-fallback, none-found error.
- `runLoopbackOAuth` — `openFn` fires a GET at the callback with a valid
  code+state; assert the captured code and success page. Separate case: wrong
  `state` → rejected. Separate case: `?error=access_denied` → surfaced.
- token exchange — override `conf.Endpoint.TokenURL` to an `httptest` server
  returning `{"refresh_token":"…","access_token":"…"}`; assert the refresh token
  is read.
- `writeOAuthToConfig` — write into a temp dir, decode back, assert OAuth fields
  set, pre-existing `developer_token` preserved, file mode `0600`.

No network in any test. `openBrowser` itself is not unit-tested (OS shell-out);
it is only reached through the injected `openFn` seam.

## Wiring checklist

- `rootCmd.AddCommand(loginCmd)` in `main.go` `init()`.
- Do **not** add to `registerTools` in `mcp.go`.
- `go fmt`, `go vet`, `staticcheck`, `go test ./...` all green before commit.
