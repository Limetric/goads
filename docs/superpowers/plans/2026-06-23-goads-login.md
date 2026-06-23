# `goads login` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a native `goads login` CLI command that runs Google's loopback OAuth2 flow to mint a refresh token and writes it into the goads config — replacing the upstream bash `generate_token.sh`.

**Architecture:** One new file `login.go` (+ `login_test.go`), all `package main`. Small, independently-testable units: a credentials.json parser, a credential resolver, a loopback OAuth server, a token-exchange wrapper, a config writer, and a cobra command that wires them together. Uses the existing `golang.org/x/oauth2`/`google` and `github.com/BurntSushi/toml` deps — no new dependencies.

**Tech Stack:** Go, cobra, golang.org/x/oauth2, BurntSushi/toml, net/http (stdlib loopback server).

## Global Constraints

- All code is `package main` at the repo root. No `cmd/` or `internal/`.
- `goads login` is CLI-only: register in `init()` in `main.go`. Do **NOT** add it to `registerTools` in `mcp.go`.
- Run `go fmt ./...` before every commit (CI rejects unformatted code).
- Tests are offline and table-driven. No network — use `net/http/httptest` and injected seams.
- Errors wrap with `%w` and the message tells the user the fix.
- File perms: secrets at `0o600`, dirs at `0o700` (match `safety.go` / `config_paths.go`).
- OAuth scope is `https://www.googleapis.com/auth/adwords`. Use `access_type=offline` + `prompt=consent` so Google always returns a refresh token.
- Branch: `feat/goads-login` (already created and checked out).

---

### Task 1: credentials.json parsing & credential resolution

Parse the Desktop-app/Web/authorized_user JSON shapes, and resolve which client credentials to use (file first, then env/config).

**Files:**
- Create: `login.go`
- Test: `login_test.go`

**Interfaces:**
- Produces:
  - `type clientCreds struct { clientID, clientSecret, refreshToken, kind string }` — `kind` is `"installed"`, `"web"`, `"authorized_user"`, or `"config"`. `refreshToken` is set only for `authorized_user`.
  - `func parseCredentialsJSON(data []byte) (clientCreds, error)`
  - `func resolveLoginCreds(cfg *Config, credentialsPath string) (clientCreds, error)`
  - `const adwordsScope = "https://www.googleapis.com/auth/adwords"`

- [ ] **Step 1: Write the failing tests**

Add to `login_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestParseCredentialsJSON(t *testing.T) {
	tests := []struct {
		name           string
		data           string
		wantKind       string
		wantID         string
		wantRefresh    string
		wantErr        bool
	}{
		{
			name:     "installed desktop app",
			data:     `{"installed":{"client_id":"cid","client_secret":"csec"}}`,
			wantKind: "installed", wantID: "cid",
		},
		{
			name:     "web application",
			data:     `{"web":{"client_id":"wid","client_secret":"wsec"}}`,
			wantKind: "web", wantID: "wid",
		},
		{
			name:        "authorized_user",
			data:        `{"type":"authorized_user","client_id":"aid","client_secret":"asec","refresh_token":"rtok"}`,
			wantKind:    "authorized_user", wantID: "aid", wantRefresh: "rtok",
		},
		{
			name:    "unknown shape",
			data:    `{"something":{}}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			data:    `not json`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCredentialsJSON([]byte(tt.data))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.kind != tt.wantKind || got.clientID != tt.wantID || got.refreshToken != tt.wantRefresh {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestResolveLoginCreds_PrefersFile(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/creds.json"
	if err := writeFileHelper(p, `{"installed":{"client_id":"fileid","client_secret":"filesec"}}`); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{ClientID: "envid", ClientSecret: "envsec"}
	got, err := resolveLoginCreds(cfg, p)
	if err != nil {
		t.Fatal(err)
	}
	if got.clientID != "fileid" {
		t.Fatalf("expected file creds to win, got %+v", got)
	}
}

func TestResolveLoginCreds_FallsBackToConfig(t *testing.T) {
	cfg := &Config{ClientID: "envid", ClientSecret: "envsec"}
	got, err := resolveLoginCreds(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.clientID != "envid" || got.kind != "config" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveLoginCreds_NoneFound(t *testing.T) {
	_, err := resolveLoginCreds(&Config{}, "")
	if err == nil || !strings.Contains(err.Error(), "--credentials") {
		t.Fatalf("expected actionable error, got %v", err)
	}
}

// writeFileHelper is a tiny test helper, defined once in login_test.go.
func writeFileHelper(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
```

Add `"os"` to the `login_test.go` imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'ParseCredentialsJSON|ResolveLoginCreds' -count=1`
Expected: FAIL — `undefined: parseCredentialsJSON`, `undefined: clientCreds`, etc.

- [ ] **Step 3: Write the minimal implementation**

Create `login.go`:

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const adwordsScope = "https://www.googleapis.com/auth/adwords"

// clientCreds is the OAuth client identity used to mint a refresh token. kind is
// "installed" (Desktop app), "web", "authorized_user" (already-tokened file), or
// "config" (taken from env/TOML). refreshToken is set only for authorized_user.
type clientCreds struct {
	clientID     string
	clientSecret string
	refreshToken string
	kind         string
}

type oauthClientBlock struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// parseCredentialsJSON reads a Google Cloud OAuth client JSON. It accepts a
// Desktop-app ("installed") or Web ("web") client, or an already-authorized
// ("authorized_user") file that carries its own refresh token.
func parseCredentialsJSON(data []byte) (clientCreds, error) {
	var doc struct {
		Installed    *oauthClientBlock `json:"installed"`
		Web          *oauthClientBlock `json:"web"`
		Type         string            `json:"type"`
		ClientID     string            `json:"client_id"`
		ClientSecret string            `json:"client_secret"`
		RefreshToken string            `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return clientCreds{}, fmt.Errorf("parse credentials JSON: %w", err)
	}
	switch {
	case doc.Installed != nil:
		return clientCreds{clientID: doc.Installed.ClientID, clientSecret: doc.Installed.ClientSecret, kind: "installed"}, nil
	case doc.Web != nil:
		return clientCreds{clientID: doc.Web.ClientID, clientSecret: doc.Web.ClientSecret, kind: "web"}, nil
	case doc.Type == "authorized_user":
		return clientCreds{clientID: doc.ClientID, clientSecret: doc.ClientSecret, refreshToken: doc.RefreshToken, kind: "authorized_user"}, nil
	default:
		return clientCreds{}, errors.New("unrecognized credentials format — expected a Desktop-app OAuth client (an \"installed\" block). Download from Google Cloud Console → APIs & Services → Credentials → OAuth 2.0 Client ID → Desktop app")
	}
}

// resolveLoginCreds picks the client credentials: an explicit --credentials file
// wins; otherwise fall back to the already-resolved env/TOML config.
func resolveLoginCreds(cfg *Config, credentialsPath string) (clientCreds, error) {
	if credentialsPath != "" {
		data, err := os.ReadFile(credentialsPath)
		if err != nil {
			return clientCreds{}, fmt.Errorf("read credentials file %q: %w", credentialsPath, err)
		}
		creds, err := parseCredentialsJSON(data)
		if err != nil {
			return clientCreds{}, err
		}
		if creds.clientID == "" || creds.clientSecret == "" {
			return clientCreds{}, fmt.Errorf("credentials file %q is missing client_id/client_secret", credentialsPath)
		}
		return creds, nil
	}
	if cfg.ClientID != "" && cfg.ClientSecret != "" {
		return clientCreds{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, kind: "config"}, nil
	}
	return clientCreds{}, errors.New("no OAuth client credentials found — pass --credentials <desktop-app.json>, or set GOOGLE_ADS_CLIENT_ID and GOOGLE_ADS_CLIENT_SECRET. Create a Desktop-app client at Google Cloud Console → APIs & Services → Credentials → OAuth 2.0 Client ID → Desktop app")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'ParseCredentialsJSON|ResolveLoginCreds' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login.go login_test.go
git commit -m "feat: credentials.json parsing and resolution for goads login"
```

---

### Task 2: write OAuth credentials into the config file

Merge `client_id` / `client_secret` / `refresh_token` into the TOML config, preserving any existing fields.

**Files:**
- Modify: `login.go`
- Test: `login_test.go`

**Interfaces:**
- Consumes: `clientCreds` (Task 1), `userConfigDir()` + `defaultConfigFile` (config_paths.go).
- Produces:
  - `func writeOAuthToConfig(path string, c clientCreds, refreshToken string) error`
  - `func configWriteTarget(explicit string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `login_test.go`:

```go
func TestWriteOAuthToConfig_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"
	if err := writeFileHelper(path, "developer_token = \"devtok\"\nlogin_customer_id = \"123\"\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeOAuthToConfig(path, clientCreds{clientID: "cid", clientSecret: "csec"}, "rtok"); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DeveloperToken != "devtok" || cfg.LoginCustomerID != "123" {
		t.Errorf("existing fields not preserved: %+v", cfg)
	}
	if cfg.ClientID != "cid" || cfg.ClientSecret != "csec" || cfg.RefreshToken != "rtok" {
		t.Errorf("oauth fields not written: %+v", cfg)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %v, want 0600", perm)
	}
}

func TestWriteOAuthToConfig_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sub/config.toml" // dir does not exist yet
	if err := writeOAuthToConfig(path, clientCreds{clientID: "cid", clientSecret: "csec"}, "rtok"); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ClientID != "cid" || cfg.RefreshToken != "rtok" {
		t.Errorf("got %+v", cfg)
	}
}
```

Add `"github.com/BurntSushi/toml"` to the `login_test.go` imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'WriteOAuthToConfig' -count=1`
Expected: FAIL — `undefined: writeOAuthToConfig`

- [ ] **Step 3: Write the minimal implementation**

Add to `login.go` (and add `"bytes"`, `"path/filepath"`, and `"github.com/BurntSushi/toml"` to its imports):

```go
// writeOAuthToConfig merges the OAuth client id/secret and refresh token into the
// TOML config at path, preserving any keys already present (developer_token,
// login_customer_id, base_url, …). The file is written 0600 under a 0700 dir.
func writeOAuthToConfig(path string, c clientCreds, refreshToken string) error {
	m := map[string]any{}
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &m); err != nil {
			return fmt.Errorf("read existing config %q: %w", path, err)
		}
	}
	m["client_id"] = c.clientID
	m["client_secret"] = c.clientSecret
	m["refresh_token"] = refreshToken

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write config %q: %w", path, err)
	}
	return nil
}

// configWriteTarget returns the file goads login should write to: the explicit
// --config path if given, otherwise the default per-user config.toml.
func configWriteTarget(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(dir, defaultConfigFile), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'WriteOAuthToConfig' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login.go login_test.go
git commit -m "feat: merge OAuth credentials into goads config file"
```

---

### Task 3: loopback OAuth server + helpers

Serve the OAuth callback on a loopback listener, validate `state`, and capture the authorization code. Plus `randomState`, the callback HTML page, and `openBrowser`.

**Files:**
- Modify: `login.go`
- Test: `login_test.go`

**Interfaces:**
- Consumes: an `*oauth2.Config` with `RedirectURL` set by the caller, and a bound `net.Listener`.
- Produces:
  - `func runLoopbackOAuth(ctx context.Context, conf *oauth2.Config, openFn func(string) error, ln net.Listener) (string, error)` — returns the captured authorization code. Closes `ln` before returning.
  - `func openBrowser(url string) error`

- [ ] **Step 1: Write the failing tests**

Add to `login_test.go` (add imports `"context"`, `"net"`, `"net/http"`, `"net/url"`):

```go
// fireCallback returns an openFn that simulates the browser redirect by GETting
// the loopback callback with the given query, reusing the state from authURL.
func fireCallback(ln net.Listener, query func(state string) string) func(string) error {
	return func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		state := u.Query().Get("state")
		go http.Get("http://" + ln.Addr().String() + "/?" + query(state))
		return nil
	}
}

func newLoopbackConf(t *testing.T) (*oauth2.Config, net.Listener) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &oauth2.Config{RedirectURL: "http://" + ln.Addr().String()}, ln
}

func TestRunLoopbackOAuth_CapturesCode(t *testing.T) {
	conf, ln := newLoopbackConf(t)
	openFn := fireCallback(ln, func(state string) string {
		return "code=testcode&state=" + state
	})
	code, err := runLoopbackOAuth(context.Background(), conf, openFn, ln)
	if err != nil {
		t.Fatal(err)
	}
	if code != "testcode" {
		t.Fatalf("got code %q", code)
	}
}

func TestRunLoopbackOAuth_StateMismatch(t *testing.T) {
	conf, ln := newLoopbackConf(t)
	openFn := fireCallback(ln, func(string) string { return "code=x&state=wrong" })
	_, err := runLoopbackOAuth(context.Background(), conf, openFn, ln)
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected state mismatch error, got %v", err)
	}
}

func TestRunLoopbackOAuth_AuthError(t *testing.T) {
	conf, ln := newLoopbackConf(t)
	openFn := fireCallback(ln, func(string) string {
		return "error=access_denied&error_description=denied"
	})
	_, err := runLoopbackOAuth(context.Background(), conf, openFn, ln)
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("expected auth error, got %v", err)
	}
}
```

Add `"golang.org/x/oauth2"` to the `login_test.go` imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'RunLoopbackOAuth' -count=1`
Expected: FAIL — `undefined: runLoopbackOAuth`

- [ ] **Step 3: Write the minimal implementation**

Add to `login.go` (add imports `"context"`, `"crypto/rand"`, `"encoding/hex"`, `"html"`, `"io"`, `"net"`, `"net/http"`, `"os/exec"`, `"runtime"`, `"time"`, `"golang.org/x/oauth2"`):

```go
// runLoopbackOAuth opens the browser to Google's consent screen and captures the
// authorization code on a loopback HTTP server. conf.RedirectURL and ln must
// agree on the port. It returns once the callback arrives, errors, or times out.
func runLoopbackOAuth(ctx context.Context, conf *oauth2.Config, openFn func(string) error, ln net.Listener) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", err
	}
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("error") != "":
			msg := q.Get("error") + ": " + q.Get("error_description")
			writeCallbackPage(w, false, msg)
			resultCh <- result{err: fmt.Errorf("authorization failed: %s", msg)}
		case q.Get("state") != state:
			writeCallbackPage(w, false, "state mismatch")
			resultCh <- result{err: errors.New("state parameter mismatch — aborting (possible CSRF)")}
		case q.Get("code") == "":
			writeCallbackPage(w, false, "no authorization code in callback")
			resultCh <- result{err: errors.New("no authorization code in callback")}
		default:
			writeCallbackPage(w, true, "")
			resultCh <- result{code: q.Get("code")}
		}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	if err := openFn(authURL); err != nil {
		return "", err
	}

	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case res := <-resultCh:
		return res.code, res.err
	case <-timer.C:
		return "", errors.New("no authorization received within 2m — did you approve in the browser?")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func writeCallbackPage(w http.ResponseWriter, ok bool, msg string) {
	w.Header().Set("Content-Type", "text/html")
	if ok {
		_, _ = io.WriteString(w, "<h1>Authorization successful</h1><p>You can close this tab and return to the terminal.</p>")
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, "<h1>Authorization failed</h1><p>"+html.EscapeString(msg)+"</p>")
}

// openBrowser opens url in the user's default browser (best effort).
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'RunLoopbackOAuth' -count=1`
Expected: PASS (all three subtests)

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login.go login_test.go
git commit -m "feat: loopback OAuth callback server for goads login"
```

---

### Task 4: token exchange wrapper

Exchange the authorization code for a refresh token, with an actionable error when none comes back.

**Files:**
- Modify: `login.go`
- Test: `login_test.go`

**Interfaces:**
- Produces: `func exchangeRefreshToken(ctx context.Context, conf *oauth2.Config, code string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `login_test.go` (add imports `"io"`, `"net/http/httptest"`):

```go
func TestExchangeRefreshToken_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"Bearer","expires_in":3600}`)
	}))
	defer ts.Close()

	conf := &oauth2.Config{ClientID: "id", ClientSecret: "sec", Endpoint: oauth2.Endpoint{TokenURL: ts.URL}}
	rt, err := exchangeRefreshToken(context.Background(), conf, "code")
	if err != nil {
		t.Fatal(err)
	}
	if rt != "rt" {
		t.Fatalf("got refresh token %q", rt)
	}
}

func TestExchangeRefreshToken_NoRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"Bearer","expires_in":3600}`)
	}))
	defer ts.Close()

	conf := &oauth2.Config{ClientID: "id", ClientSecret: "sec", Endpoint: oauth2.Endpoint{TokenURL: ts.URL}}
	_, err := exchangeRefreshToken(context.Background(), conf, "code")
	if err == nil || !strings.Contains(err.Error(), "Desktop app") {
		t.Fatalf("expected actionable no-refresh-token error, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'ExchangeRefreshToken' -count=1`
Expected: FAIL — `undefined: exchangeRefreshToken`

- [ ] **Step 3: Write the minimal implementation**

Add to `login.go`:

```go
// exchangeRefreshToken trades an authorization code for tokens and returns the
// refresh token. A missing refresh token almost always means a misconfigured
// OAuth client, so the error spells out the usual causes.
func exchangeRefreshToken(ctx context.Context, conf *oauth2.Config, code string) (string, error) {
	tok, err := conf.Exchange(ctx, code)
	if err != nil {
		return "", fmt.Errorf("exchange authorization code: %w", err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("no refresh_token in response — common causes: wrong OAuth client type (need Desktop app, not Web application), the loopback redirect URI is not authorized, or the Google Ads API is not enabled in the project")
	}
	return tok.RefreshToken, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'ExchangeRefreshToken' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login.go login_test.go
git commit -m "feat: token exchange wrapper for goads login"
```

---

### Task 5: `goads login` command + wiring

Wire the units into a cobra command, register it, and add an offline end-to-end test via the `authorized_user` path.

**Files:**
- Modify: `login.go`, `main.go`
- Test: `login_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4, `loadConfig` (config.go), `configPath` global (main.go).
- Produces: `var loginCmd *cobra.Command`, registered via `rootCmd.AddCommand(loginCmd)`.

- [ ] **Step 1: Write the failing test**

Add to `login_test.go`:

Do **not** pass `--config` here. The global `--config` flag is bound to the
package-level `configPath`; setting it would (a) make `loadConfig` fail, since it
errors on an explicit path that doesn't exist yet, and (b) leak into later tests
that assume `configPath == ""`. Instead use the default path under the temp HOME
that `useTempState` sets up, and pre-create it so the merge path is exercised.
Add `"os"` and `"path/filepath"` to the test imports.

```go
func TestCLI_LoginAuthorizedUser(t *testing.T) {
	useTempState(t) // HOME/XDG → temp, so the default config path is sandboxed
	clearAdsEnv(t)

	credPath := t.TempDir() + "/creds.json"
	if err := writeFileHelper(credPath, `{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rtok"}`); err != nil {
		t.Fatal(err)
	}

	// Pre-create the default config with an existing field to verify the merge.
	target, err := configWriteTarget("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileHelper(target, "developer_token = \"pre\"\n"); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, "login", "--credentials", credPath)
	if err != nil {
		t.Fatalf("login failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Wrote credentials") || !strings.Contains(out, "GOOGLE_ADS_REFRESH_TOKEN") {
		t.Errorf("unexpected output:\n%s", out)
	}

	var cfg Config
	if _, err := toml.DecodeFile(target, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DeveloperToken != "pre" {
		t.Errorf("existing developer_token not preserved: %+v", cfg)
	}
	if cfg.ClientID != "cid" || cfg.RefreshToken != "rtok" {
		t.Errorf("oauth fields not written: %+v", cfg)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'CLI_LoginAuthorizedUser' -count=1`
Expected: FAIL — `undefined: loginCmd` (command not registered → unknown command error)

- [ ] **Step 3: Write the minimal implementation**

Add to `login.go` (add `"github.com/spf13/cobra"` and `"golang.org/x/oauth2/google"` to imports — `google.Endpoint` is the production OAuth endpoint, same as `auth.go` uses):

```go
var (
	loginCredentialsPath string
	loginPort            int
	loginNoBrowser       bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Sign in to Google Ads via OAuth2 and save a refresh token",
	Long:  "login runs Google's loopback OAuth2 flow: it opens your browser, captures the\nauthorization code on localhost, exchanges it for a refresh token, and writes\nthe credentials into your goads config. The developer token is still required\nseparately (see `goads doctor`).",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig(configPath)
		if err != nil {
			return err
		}
		creds, err := resolveLoginCreds(cfg, loginCredentialsPath)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "=== Google Ads OAuth2 sign-in ===")

		refreshToken := creds.refreshToken
		if creds.kind == "authorized_user" {
			if refreshToken == "" {
				return errors.New("authorized_user credentials file has no refresh_token")
			}
			fmt.Fprintln(out, "Credentials file already contains a refresh token; skipping browser sign-in.")
		} else {
			if creds.kind == "web" {
				fmt.Fprintln(out, "Warning: this is a Web-application client; loopback sign-in expects a Desktop-app client. Trying anyway.")
			}
			conf := &oauth2.Config{
				ClientID:     creds.clientID,
				ClientSecret: creds.clientSecret,
				Endpoint:     google.Endpoint,
				RedirectURL:  fmt.Sprintf("http://localhost:%d", loginPort),
				Scopes:       []string{adwordsScope},
			}
			addr := fmt.Sprintf("127.0.0.1:%d", loginPort)
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w — is the port busy? pass --port", addr, err)
			}
			openFn := openBrowser
			if loginNoBrowser {
				openFn = func(u string) error {
					fmt.Fprintf(out, "Open this URL in your browser:\n  %s\n", u)
					return nil
				}
			} else {
				fmt.Fprintln(out, "Opening browser for Google sign-in…")
			}
			fmt.Fprintf(out, "Waiting for callback on http://localhost:%d …\n", loginPort)
			code, err := runLoopbackOAuth(cmd.Context(), conf, openFn, ln)
			if err != nil {
				return err
			}
			refreshToken, err = exchangeRefreshToken(cmd.Context(), conf, code)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ Authorized. Exchanged code for refresh token.")
		}

		target, err := configWriteTarget(configPath)
		if err != nil {
			return err
		}
		if err := writeOAuthToConfig(target, creds, refreshToken); err != nil {
			return err
		}
		fmt.Fprintf(out, "✓ Wrote credentials to %s\n\n", target)
		fmt.Fprintln(out, "For CI / MCP host config, set:")
		fmt.Fprintf(out, "  export GOOGLE_ADS_CLIENT_ID=%q\n", creds.clientID)
		fmt.Fprintf(out, "  export GOOGLE_ADS_CLIENT_SECRET=%q\n", creds.clientSecret)
		fmt.Fprintf(out, "  export GOOGLE_ADS_REFRESH_TOKEN=%q\n\n", refreshToken)
		fmt.Fprintln(out, "Run `goads doctor` to verify. (developer token still required.)")
		return nil
	},
}

func init() {
	loginCmd.Flags().StringVar(&loginCredentialsPath, "credentials", "", "path to a Desktop-app OAuth client JSON downloaded from Google Cloud Console")
	loginCmd.Flags().IntVar(&loginPort, "port", 8085, "loopback port for the OAuth callback")
	loginCmd.Flags().BoolVar(&loginNoBrowser, "no-browser", false, "print the auth URL instead of opening a browser")
}
```

Register the command in `main.go` `init()` — add after `rootCmd.AddCommand(doctorCmd)`:

```go
	rootCmd.AddCommand(loginCmd)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./... -run 'CLI_LoginAuthorizedUser' -count=1`
Expected: PASS

- [ ] **Step 5: Full verification**

Run each and confirm clean:

```bash
go fmt ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck ./...
go test ./... -count=1
go build -o build/goads . && ./build/goads login --help
```

Expected: vet/staticcheck silent, all tests PASS, `login --help` shows the command with `--credentials`, `--port`, `--no-browser`.

- [ ] **Step 6: Commit**

```bash
git add login.go main.go login_test.go
git commit -m "feat: goads login command for seamless OAuth2 sign-in"
```

---

## Notes for the implementer

- **Import hygiene:** `login.go` ends up importing: `bytes`, `context`, `crypto/rand`, `encoding/hex`, `encoding/json`, `errors`, `fmt`, `html`, `io`, `net`, `net/http`, `os`, `os/exec`, `path/filepath`, `runtime`, `time`, `github.com/BurntSushi/toml`, `github.com/spf13/cobra`, `golang.org/x/oauth2`, `golang.org/x/oauth2/google`. Let `go fmt`/`goimports` group them; remove any you didn't end up using.
- **Two `init()` funcs** in `package main` (main.go and login.go) are fine in Go — both run. Keep the login flag wiring in login.go's `init()` and only the `AddCommand` in main.go to match how other commands are registered.
- **Why a listener, not a port, into `runLoopbackOAuth`:** binding before the call lets the command surface a friendly "port busy" error, and lets tests use an ephemeral port (`127.0.0.1:0`) instead of a hard-coded one.
- **Don't** touch `mcp.go` — login is intentionally CLI-only.
```
