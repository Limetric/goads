# Interactive `goads login` Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `goads login` into a guided, fool-proof interactive wizard that walks a newcomer through every Google Cloud / Google Ads prerequisite, collects OAuth + developer token + MCC ID, writes the config, and verifies with a live API call — while preserving the existing non-interactive behavior for CI and the agent skill.

**Architecture:** A new `login_wizard.go` holds the interactive orchestration (`runLoginWizard`) and a small `prompter` abstraction (real TTY impl + test fake), keeping `login.go` focused on OAuth primitives. `login.go` gains a startup branch (TTY && !`--no-input` && no `--credentials` → wizard) and a `--no-input` flag. `client.go` gains `ListAccessibleCustomers` for the live verify. One new dependency: `golang.org/x/term` for masked secret input.

**Tech Stack:** Go, cobra, golang.org/x/oauth2, golang.org/x/term (new), BurntSushi/toml, net/http (loopback + httptest).

## Global Constraints

- All code is `package main` at the repo root. No `cmd/` or `internal/`.
- `goads login` is CLI-only — never registered in `registerTools` (mcp.go).
- `go fmt ./...` before every commit (CI rejects unformatted code).
- Tests are offline and table-driven. No network — use `net/http/httptest`, injected `prompter`/`openFn` seams, and the `googleOAuthEndpoint` package var.
- Errors wrap with `%w` and the message tells the user the fix.
- Secrets: config files `0o600`, dirs `0o700`. Never print a secret in full — truncate to a suffix hint.
- Wizard runs ONLY when stdin is a TTY AND `--no-input` is unset AND `--credentials` was not passed. Otherwise the existing non-interactive flow runs unchanged.
- OAuth: scope `https://www.googleapis.com/auth/adwords` (the `adwordsScope` const), `access_type=offline` + `prompt=consent`, redirect `http://localhost:<port>`, default port 8085.
- Live verify uses `customers:listAccessibleCustomers` (needs only OAuth + developer token; no customer/MCC id).
- New dependency `golang.org/x/term` only; run `go mod tidy` after adding it.
- Branch: `feat/goads-login-wizard` (already created and checked out).
- After every task: `go fmt`, `go vet`, `go test ./...`, and `staticcheck ./...` must be clean.

---

### Task 1: `Client.ListAccessibleCustomers` + GET helper

The live-verify endpoint. A GET mirroring the existing `post`, plus the method that parses `resourceNames` into bare customer IDs.

**Files:**
- Modify: `client.go`
- Test: `client_test.go`

**Interfaces:**
- Consumes: `Client.buildHeaders`, `Client.cfg.BaseURL`, `apiError` (all in client.go).
- Produces:
  - `func (c *Client) get(ctx context.Context, path string, out any) error`
  - `func (c *Client) ListAccessibleCustomers(ctx context.Context) ([]string, error)`

- [ ] **Step 1: Write the failing test**

Add to `client_test.go` (it already imports `context`, `net/http`, `net/http/httptest`, `testing`; add `strings` if not present):

```go
func TestListAccessibleCustomers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/customers:listAccessibleCustomers" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("developer-token") == "" {
			t.Error("developer-token header not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resourceNames":["customers/1234567890","customers/9876543210"]}`))
	}))
	defer srv.Close()

	cfg := &Config{BaseURL: srv.URL} // non-prod base → isTest(), lenient auth
	c, err := NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := c.ListAccessibleCustomers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1234567890", "9876543210"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("got %v, want %v", ids, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'ListAccessibleCustomers' -count=1`
Expected: FAIL — `c.ListAccessibleCustomers undefined`

- [ ] **Step 3: Write the minimal implementation**

Add `"strings"` to `client.go`'s imports. Add the GET helper next to `post` (after the `post` func, before `apiError`):

```go
// get issues a GET to {baseURL}/{path} and decodes the JSON response into out.
// Mirrors post for read-only endpoints that take no body.
func (c *Client) get(ctx context.Context, path string, out any) error {
	url := c.cfg.BaseURL + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if err := c.buildHeaders(req); err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return apiError(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}
```

Add the method in the Operations section (after `Search`, say):

```go
// ListAccessibleCustomers returns the bare customer IDs the authenticated user
// can access. It calls customers:listAccessibleCustomers, which needs only a
// valid OAuth token and developer token — no customer or login-customer-id — so
// it is the right call to verify a fresh setup works end to end.
func (c *Client) ListAccessibleCustomers(ctx context.Context) ([]string, error) {
	var out struct {
		ResourceNames []string `json:"resourceNames"`
	}
	if err := c.get(ctx, "customers:listAccessibleCustomers", &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.ResourceNames))
	for _, rn := range out.ResourceNames {
		ids = append(ids, strings.TrimPrefix(rn, "customers/"))
	}
	return ids, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./... -run 'ListAccessibleCustomers' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add client.go client_test.go
git commit -m "feat: Client.ListAccessibleCustomers for login verification"
```

---

### Task 2: `mergeConfigValues` — write arbitrary config keys

The wizard writes five fields (client id/secret, refresh token, developer token, login-customer-id), not just the three `writeOAuthToConfig` handles. Generalize the merge writer and make `writeOAuthToConfig` delegate to it (keeping its signature and tests green).

**Files:**
- Modify: `login.go`
- Test: `login_test.go`

**Interfaces:**
- Produces: `func mergeConfigValues(path string, values map[string]string) error` — merges the non-empty entries of `values` into the TOML at `path`, preserving existing keys; `0o600` / dir `0o700`.
- Changes: `writeOAuthToConfig` now delegates to `mergeConfigValues` (same external behavior for non-empty inputs).

- [ ] **Step 1: Write the failing test**

Add to `login_test.go`:

```go
func TestMergeConfigValues_WritesAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"
	if err := writeFileHelper(path, "base_url = \"https://example/v23\"\n"); err != nil {
		t.Fatal(err)
	}
	err := mergeConfigValues(path, map[string]string{
		"developer_token":   "devtok",
		"login_customer_id": "1234567890",
		"client_id":         "", // empty → must NOT be written
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://example/v23" {
		t.Errorf("preserved key lost: %+v", cfg)
	}
	if cfg.DeveloperToken != "devtok" || cfg.LoginCustomerID != "1234567890" {
		t.Errorf("values not written: %+v", cfg)
	}
	if cfg.ClientID != "" {
		t.Errorf("empty value should not be written, got %q", cfg.ClientID)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'MergeConfigValues' -count=1`
Expected: FAIL — `undefined: mergeConfigValues`

- [ ] **Step 3: Write the minimal implementation**

In `login.go`, replace the body of `writeOAuthToConfig` and add `mergeConfigValues`:

```go
// mergeConfigValues merges the non-empty entries of values into the TOML config
// at path, preserving any keys already present. Empty values are skipped (so a
// skipped optional field never overwrites an existing one). 0600 file, 0700 dir.
func mergeConfigValues(path string, values map[string]string) error {
	m := map[string]any{}
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &m); err != nil {
			return fmt.Errorf("read existing config %q: %w", path, err)
		}
	}
	for k, v := range values {
		if v != "" {
			m[k] = v
		}
	}
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

// writeOAuthToConfig merges the OAuth client id/secret and refresh token into the
// TOML config at path, preserving any other keys already present.
func writeOAuthToConfig(path string, c clientCreds, refreshToken string) error {
	return mergeConfigValues(path, map[string]string{
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"refresh_token": refreshToken,
	})
}
```

Remove the old `writeOAuthToConfig` body (the inline map/stat/encode) — it now lives in `mergeConfigValues`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'MergeConfigValues|WriteOAuthToConfig' -count=1`
Expected: PASS (the new test and both pre-existing `WriteOAuthToConfig` tests)

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login.go login_test.go
git commit -m "refactor: extract mergeConfigValues for multi-key config writes"
```

---

### Task 3: `prompter` + `ttyPrompter` + input helpers (adds `golang.org/x/term`)

The input layer: an interface the wizard talks to, a real terminal implementation with masked secret reads, and two small string helpers. This task adds the `golang.org/x/term` dependency.

**Files:**
- Create: `login_wizard.go`
- Test: `login_wizard_test.go`
- Modify: `go.mod` / `go.sum` (via `go get` + `go mod tidy`)

**Interfaces:**
- Produces:
  - `type prompter interface { line(prompt string) (string, error); secret(prompt string) (string, error); confirm(prompt string, def bool) (bool, error) }`
  - `type ttyPrompter struct { in io.Reader; out io.Writer; fd int }` and `func newTTYPrompter(in io.Reader, out io.Writer, fd int) *ttyPrompter`
  - `func secretHint(s string) string` — `"…"+last 6 chars`, or `"…"` if short
  - `func expandHome(path string) string` — expands a leading `~/` and trims quotes/space
  - `func dashCustomerID(id string) string` — `1234567890` → `123-456-7890` (else unchanged)

- [ ] **Step 1: Add the dependency**

Run:
```bash
go get golang.org/x/term@latest
```
Expected: `go.mod` gains a `golang.org/x/term vX.Y.Z` require line.

- [ ] **Step 2: Write the failing tests**

Create `login_wizard_test.go`:

```go
package main

import (
	"io"
	"strings"
	"testing"
)

func TestTTYPrompter_LineAndConfirm(t *testing.T) {
	// Two reads: a line, then a confirm answered with empty (→ default).
	in := strings.NewReader("  hello \n\n")
	var out strings.Builder
	p := newTTYPrompter(in, &out, -1) // fd<0 → no masking

	got, err := p.line("name: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello" {
		t.Errorf("line = %q, want %q", got, "hello")
	}
	yes, err := p.confirm("ok?", true)
	if err != nil {
		t.Fatal(err)
	}
	if !yes {
		t.Error("empty answer should take the default (true)")
	}
	if !strings.Contains(out.String(), "name: ") || !strings.Contains(out.String(), "[Y/n]") {
		t.Errorf("prompts not written: %q", out.String())
	}
}

func TestTTYPrompter_ConfirmNo(t *testing.T) {
	p := newTTYPrompter(strings.NewReader("n\n"), io.Discard, -1)
	yes, err := p.confirm("ok?", true)
	if err != nil {
		t.Fatal(err)
	}
	if yes {
		t.Error("'n' should be false")
	}
}

func TestTTYPrompter_SecretFallback(t *testing.T) {
	// fd<0 → not a terminal → plain line read (no masking).
	p := newTTYPrompter(strings.NewReader("s3cret\n"), io.Discard, -1)
	got, err := p.secret("token: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "s3cret" {
		t.Errorf("secret = %q", got)
	}
}

func TestSecretHint(t *testing.T) {
	if got := secretHint("abcdefghij"); got != "…efghij" {
		t.Errorf("got %q", got)
	}
	if got := secretHint("abc"); got != "…" {
		t.Errorf("short hint = %q", got)
	}
}

func TestDashCustomerID(t *testing.T) {
	if got := dashCustomerID("1234567890"); got != "123-456-7890" {
		t.Errorf("got %q", got)
	}
	if got := dashCustomerID("123"); got != "123" {
		t.Errorf("non-10-digit unchanged, got %q", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./... -run 'TTYPrompter|SecretHint|DashCustomerID' -count=1`
Expected: FAIL — `undefined: newTTYPrompter` etc.

- [ ] **Step 4: Write the minimal implementation**

Create `login_wizard.go`:

```go
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// prompter is the wizard's input surface. The real implementation reads a TTY;
// tests inject a fake with scripted answers.
type prompter interface {
	line(prompt string) (string, error)
	secret(prompt string) (string, error)
	confirm(prompt string, def bool) (bool, error)
}

// ttyPrompter reads line-oriented input from a terminal. It reads one byte at a
// time (no buffering ahead) so a masked term.ReadPassword on the same fd never
// loses input that a buffered reader would have swallowed.
type ttyPrompter struct {
	in  io.Reader
	out io.Writer
	fd  int // terminal fd for masked reads; <0 means "not a terminal"
}

func newTTYPrompter(in io.Reader, out io.Writer, fd int) *ttyPrompter {
	return &ttyPrompter{in: in, out: out, fd: fd}
}

// readLine reads up to (not including) the next '\n'. Returns io.EOF only when
// the stream ends with no data read (so the caller can treat it as an abort).
func readLine(r io.Reader) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return sb.String(), nil
			}
			if buf[0] != '\r' {
				sb.WriteByte(buf[0])
			}
		}
		if err != nil {
			if err == io.EOF {
				if sb.Len() == 0 {
					return "", io.EOF
				}
				return sb.String(), nil
			}
			return "", err
		}
	}
}

func (p *ttyPrompter) line(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	s, err := readLine(p.in)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func (p *ttyPrompter) secret(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	if p.fd >= 0 && term.IsTerminal(p.fd) {
		b, err := term.ReadPassword(p.fd)
		fmt.Fprintln(p.out) // ReadPassword swallows the newline; restore it
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	s, err := readLine(p.in)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func (p *ttyPrompter) confirm(prompt string, def bool) (bool, error) {
	suffix := " [y/N]: "
	if def {
		suffix = " [Y/n]: "
	}
	fmt.Fprint(p.out, prompt+suffix)
	s, err := readLine(p.in)
	if err != nil {
		return false, err
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return def, nil
	}
	return s == "y" || s == "yes", nil
}

// secretHint returns a non-revealing suffix hint for an existing secret.
func secretHint(s string) string {
	if len(s) <= 6 {
		return "…"
	}
	return "…" + s[len(s)-6:]
}

// expandHome expands a leading ~/ and strips surrounding quotes/space (so a
// drag-and-dropped path pastes cleanly).
func expandHome(path string) string {
	path = strings.Trim(strings.TrimSpace(path), "'\"")
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// dashCustomerID formats a 10-digit customer ID as 123-456-7890 for display.
func dashCustomerID(id string) string {
	if len(id) == 10 {
		return id[0:3] + "-" + id[3:6] + "-" + id[6:10]
	}
	return id
}
```

- [ ] **Step 5: Tidy modules and run the tests**

Run:
```bash
go mod tidy
go test ./... -run 'TTYPrompter|SecretHint|DashCustomerID' -count=1
```
Expected: `go mod tidy` resolves `golang.org/x/term`; tests PASS.

- [ ] **Step 6: Full check + commit**

```bash
go fmt ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck ./...
git add login_wizard.go login_wizard_test.go go.mod go.sum
git commit -m "feat: prompter abstraction and TTY input layer for login wizard"
```
Note: `staticcheck` may report the new helpers as unused until Task 4/5 wire them. That is expected mid-feature; do NOT add suppression sentinels — Task 5 leaves staticcheck fully clean. If you prefer a clean intermediate, you may proceed; the final task verifies cleanliness.

---

### Task 4: wizard step helpers + `offerToOpen` + `fakePrompter`

The gather steps, each driven by the `prompter`. Includes the test fake. These orchestrate already-built units (`parseCredentialsJSON`, `normalizeCustomerID`) and read/reuse from `cfg`.

**Files:**
- Modify: `login_wizard.go`
- Test: `login_wizard_test.go`

**Interfaces:**
- Consumes: `prompter`, `clientCreds`, `parseCredentialsJSON`, `normalizeCustomerID`, `secretHint`, `expandHome`, the `loginNoBrowser` flag var.
- Produces (all package-level funcs in login_wizard.go):
  - `func offerToOpen(p prompter, out io.Writer, instruction, url string, openFn func(string) error) error`
  - `func wizardGatherClient(p prompter, out io.Writer, cfg *Config, openFn func(string) error) (clientCreds, error)`
  - `func wizardGatherDeveloperToken(p prompter, out io.Writer, cfg *Config, openFn func(string) error) (string, error)`
  - `func wizardGatherLoginCustomerID(p prompter, out io.Writer, cfg *Config) (string, error)`
  - URL consts: `urlEnableAPI`, `urlCredentials`, `urlConsent`, `urlAPICenter`
  - Test fake: `fakePrompter` (in `login_wizard_test.go`)

- [ ] **Step 1: Write the failing tests**

Add to `login_wizard_test.go` (add imports `os`, `io` already present; add `strings` already present):

```go
// fakePrompter returns scripted answers per method, in order.
type fakePrompter struct {
	lines    []string
	secrets  []string
	confirms []bool
	li, si, ci int
}

func (f *fakePrompter) line(string) (string, error) {
	v := f.lines[f.li]
	f.li++
	return v, nil
}
func (f *fakePrompter) secret(string) (string, error) {
	v := f.secrets[f.si]
	f.si++
	return v, nil
}
func (f *fakePrompter) confirm(string, bool) (bool, error) {
	v := f.confirms[f.ci]
	f.ci++
	return v, nil
}

func TestWizardGatherClient_FromPath(t *testing.T) {
	dir := t.TempDir()
	jsonPath := dir + "/c.json"
	if err := writeFileHelper(jsonPath, `{"installed":{"client_id":"cid","client_secret":"csec"}}`); err != nil {
		t.Fatal(err)
	}
	// offerToOpen: confirm "Open this now?" → no, then a "Press Enter" line.
	// Then wizardGatherClient prompts for the path. So lines = [press-enter, path].
	p := &fakePrompter{confirms: []bool{false}, lines: []string{"", jsonPath}}
	creds, err := wizardGatherClient(p, io.Discard, &Config{}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if creds.clientID != "cid" || creds.clientSecret != "csec" {
		t.Fatalf("got %+v", creds)
	}
}

func TestWizardGatherClient_RepromptsOnBadPath(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/c.json"
	if err := writeFileHelper(good, `{"installed":{"client_id":"cid","client_secret":"csec"}}`); err != nil {
		t.Fatal(err)
	}
	// offerToOpen: open? no, then press-enter line. Then path prompts:
	// first missing → reprompt, second good. lines = [press-enter, missing, good].
	p := &fakePrompter{confirms: []bool{false}, lines: []string{"", dir + "/missing.json", good}}
	var out strings.Builder
	creds, err := wizardGatherClient(p, &out, &Config{}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if creds.clientID != "cid" {
		t.Fatalf("got %+v", creds)
	}
	if !strings.Contains(out.String(), "try again") {
		t.Errorf("expected a retry message, got: %s", out.String())
	}
}

func TestWizardGatherClient_ReuseExisting(t *testing.T) {
	cfg := &Config{ClientID: "existing", ClientSecret: "esec"}
	// confirm "Keep it?" → yes. No line reads.
	p := &fakePrompter{confirms: []bool{true}}
	creds, err := wizardGatherClient(p, io.Discard, cfg, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if creds.clientID != "existing" || creds.kind != "config" {
		t.Fatalf("got %+v", creds)
	}
}

func TestWizardGatherDeveloperToken_FreshAndEmptyReprompt(t *testing.T) {
	// open? no; first secret empty → reprompt; second secret valid.
	p := &fakePrompter{confirms: []bool{false}, lines: []string{""}, secrets: []string{"", "devtok"}}
	var out strings.Builder
	tok, err := wizardGatherDeveloperToken(p, &out, &Config{}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if tok != "devtok" {
		t.Fatalf("got %q", tok)
	}
	if !strings.Contains(out.String(), "can't be empty") {
		t.Errorf("expected empty-token message, got %s", out.String())
	}
}

func TestWizardGatherDeveloperToken_Reuse(t *testing.T) {
	p := &fakePrompter{confirms: []bool{true}} // Keep it? → yes
	tok, err := wizardGatherDeveloperToken(p, io.Discard, &Config{DeveloperToken: "old"}, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if tok != "old" {
		t.Fatalf("got %q", tok)
	}
}

func TestWizardGatherLoginCustomerID(t *testing.T) {
	// Provided value, dashes stripped.
	p := &fakePrompter{lines: []string{"123-456-7890"}}
	id, err := wizardGatherLoginCustomerID(p, io.Discard, &Config{})
	if err != nil {
		t.Fatal(err)
	}
	if id != "1234567890" {
		t.Fatalf("got %q", id)
	}
	// Empty input keeps the existing default.
	p2 := &fakePrompter{lines: []string{""}}
	id2, err := wizardGatherLoginCustomerID(p2, io.Discard, &Config{LoginCustomerID: "999"})
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "999" {
		t.Fatalf("got %q", id2)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./... -run 'WizardGather' -count=1`
Expected: FAIL — `undefined: wizardGatherClient` etc.

- [ ] **Step 3: Write the minimal implementation**

Add to `login_wizard.go` (add `"net"`, `"context"`, `"golang.org/x/oauth2"` later in Task 5; for THIS task you only need the imports already present plus none new):

```go
const (
	urlEnableAPI   = "https://console.cloud.google.com/apis/library/googleads.googleapis.com"
	urlCredentials = "https://console.cloud.google.com/apis/credentials"
	urlConsent     = "https://console.cloud.google.com/apis/credentials/consent"
	urlAPICenter   = "https://ads.google.com/aw/apicenter"
)

// offerToOpen prints an instruction + URL and (unless --no-browser) offers to
// open it, then waits for the user to press Enter.
func offerToOpen(p prompter, out io.Writer, instruction, url string, openFn func(string) error) error {
	fmt.Fprintf(out, "   %s\n   → %s\n", instruction, url)
	if loginNoBrowser {
		_, err := p.line("   Press Enter when done.")
		return err
	}
	open, err := p.confirm("   Open this now?", true)
	if err != nil {
		return err
	}
	if open {
		if e := openFn(url); e != nil {
			fmt.Fprintf(out, "   (couldn't open a browser: %v — open the URL above manually)\n", e)
		}
	}
	_, err = p.line("   Press Enter when done.")
	return err
}

// wizardGatherClient resolves the OAuth client: reuse an existing one from cfg,
// or guide the user to download a Desktop-app JSON and read it (re-prompting on
// a bad path).
func wizardGatherClient(p prompter, out io.Writer, cfg *Config, openFn func(string) error) (clientCreds, error) {
	if cfg.ClientID != "" && cfg.ClientSecret != "" {
		keep, err := p.confirm(fmt.Sprintf("   Found an OAuth client (%s). Keep it?", secretHint(cfg.ClientID)), true)
		if err != nil {
			return clientCreds{}, err
		}
		if keep {
			return clientCreds{clientID: cfg.ClientID, clientSecret: cfg.ClientSecret, kind: "config"}, nil
		}
	}
	if err := offerToOpen(p, out, "Create Credentials → OAuth client ID → \"Desktop app\" → Download JSON.", urlCredentials, openFn); err != nil {
		return clientCreds{}, err
	}
	for {
		raw, err := p.line("   Path to the downloaded JSON: ")
		if err != nil {
			return clientCreds{}, err
		}
		path := expandHome(raw)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(out, "   couldn't read %q: %v — try again.\n", path, err)
			continue
		}
		creds, err := parseCredentialsJSON(data)
		if err != nil {
			fmt.Fprintf(out, "   %v — try again.\n", err)
			continue
		}
		if creds.clientID == "" || creds.clientSecret == "" {
			fmt.Fprintln(out, "   that file has no client_id/client_secret — try again.")
			continue
		}
		if creds.kind == "web" {
			fmt.Fprintln(out, "   note: this is a Web-application client; a Desktop-app client is recommended. Continuing.")
		}
		fmt.Fprintf(out, "   ✓ Read client %q\n", secretHint(creds.clientID))
		return creds, nil
	}
}

// wizardGatherDeveloperToken reuses an existing developer token or prompts for a
// new one (masked), re-prompting until non-empty.
func wizardGatherDeveloperToken(p prompter, out io.Writer, cfg *Config, openFn func(string) error) (string, error) {
	if cfg.DeveloperToken != "" {
		keep, err := p.confirm(fmt.Sprintf("   Found a developer token (%s). Keep it?", secretHint(cfg.DeveloperToken)), true)
		if err != nil {
			return "", err
		}
		if keep {
			return cfg.DeveloperToken, nil
		}
	}
	if err := offerToOpen(p, out, "In Google Ads: Tools & Settings → API Center.", urlAPICenter, openFn); err != nil {
		return "", err
	}
	for {
		tok, err := p.secret("   Paste your developer token: ")
		if err != nil {
			return "", err
		}
		if tok != "" {
			return tok, nil
		}
		fmt.Fprintln(out, "   developer token can't be empty — try again.")
	}
}

// wizardGatherLoginCustomerID prompts for an optional manager (MCC) account ID,
// defaulting to the existing value and stripping dashes.
func wizardGatherLoginCustomerID(p prompter, out io.Writer, cfg *Config) (string, error) {
	prompt := "   Login customer ID [skip]: "
	if cfg.LoginCustomerID != "" {
		prompt = fmt.Sprintf("   Login customer ID [%s]: ", cfg.LoginCustomerID)
	}
	v, err := p.line(prompt)
	if err != nil {
		return "", err
	}
	if v == "" {
		return cfg.LoginCustomerID, nil
	}
	return normalizeCustomerID(v), nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./... -run 'WizardGather' -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
go fmt ./...
git add login_wizard.go login_wizard_test.go
git commit -m "feat: wizard gather-steps for client, developer token, and MCC id"
```

---

### Task 5: `runLoginWizard` orchestration, verify, `--no-input` flag, and command wiring

The integration: the refresh-token gather step (live OAuth via the testable `googleOAuthEndpoint` seam), the orchestrator that writes config and runs the live verify, the `--no-input` flag, and the `loginCmd` branch that selects wizard vs non-interactive. Ends with a full offline happy-path test.

**Files:**
- Modify: `login_wizard.go`, `login.go`
- Test: `login_wizard_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4 (`ListAccessibleCustomers`, `mergeConfigValues`, `prompter`/`ttyPrompter`, the gather steps), plus `runLoopbackOAuth`, `exchangeRefreshToken`, `configWriteTarget`, `loadLoginConfig`, `NewClient`, `normalizeCustomerID`, `dashCustomerID`, `openBrowser`, and the `configPath`/`loginPort`/`loginNoBrowser` globals.
- Produces:
  - `var googleOAuthEndpoint = google.Endpoint` (in login_wizard.go) — the OAuth endpoint, overridable by tests.
  - `func wizardGatherRefreshToken(ctx context.Context, p prompter, out io.Writer, creds clientCreds, cfg *Config, openFn func(string) error, port int) (string, error)`
  - `func runLoginWizard(ctx context.Context, out io.Writer, p prompter, cfg *Config, openFn func(string) error, port int) error`
  - `func isInteractiveLogin() bool` (in login.go)
  - New flag var `loginNoInput bool` and `--no-input`.

- [ ] **Step 1: Write the failing test (full happy-path wizard, offline)**

Add to `login_wizard_test.go` (add imports `context`, `net`, `net/http`, `net/url`, `path/filepath`, `golang.org/x/oauth2`):

```go
// freePort returns a currently-free localhost port for the loopback OAuth server.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestRunLoginWizard_HappyPath(t *testing.T) {
	useTempState(t)
	clearAdsEnv(t)

	// One httptest server doubles as the OAuth token endpoint and the Ads API.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"Bearer","expires_in":3600}`)
		case r.URL.Path == "/customers:listAccessibleCustomers":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"resourceNames":["customers/1234567890"]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	// Point OAuth token exchange and the Ads API at the test server.
	oldEndpoint := googleOAuthEndpoint
	googleOAuthEndpoint = oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token"}
	t.Cleanup(func() { googleOAuthEndpoint = oldEndpoint })
	t.Setenv("GOOGLE_ADS_API_BASE_URL", srv.URL)

	cfg, err := loadLoginConfig("")
	if err != nil {
		t.Fatal(err)
	}

	// OAuth client JSON the wizard will read.
	jsonPath := t.TempDir() + "/c.json"
	if err := writeFileHelper(jsonPath, `{"installed":{"client_id":"cid","client_secret":"csec"}}`); err != nil {
		t.Fatal(err)
	}

	port := freePort(t)
	// openFn fires the loopback callback with the state extracted from the auth URL.
	openFn := func(authURL string) error {
		u, perr := url.Parse(authURL)
		if perr != nil {
			return perr
		}
		st := u.Query().Get("state")
		go http.Get(fmt.Sprintf("http://127.0.0.1:%d/?code=testcode&state=%s", port, st))
		return nil
	}

	// Scripted answers, in call order:
	//   confirms: step1 open? n, step2 open? n, step4 open? n
	//   lines:    step1 enter, step2 enter, JSON path, step4 enter, login id (skip)
	//   secrets:  developer token
	p := &fakePrompter{
		confirms: []bool{false, false, false},
		lines:    []string{"", "", jsonPath, "", ""},
		secrets:  []string{"devtok"},
	}

	var out strings.Builder
	if err := runLoginWizard(context.Background(), &out, p, cfg, openFn, port); err != nil {
		t.Fatalf("wizard failed: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "Connected") || !strings.Contains(out.String(), "123-456-7890") {
		t.Errorf("missing verify line:\n%s", out.String())
	}

	target, err := configWriteTarget("")
	if err != nil {
		t.Fatal(err)
	}
	var written Config
	if _, err := toml.DecodeFile(target, &written); err != nil {
		t.Fatal(err)
	}
	if written.ClientID != "cid" || written.RefreshToken != "rt" || written.DeveloperToken != "devtok" {
		t.Errorf("config not fully written: %+v", written)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./... -run 'RunLoginWizard_HappyPath' -count=1`
Expected: FAIL — `undefined: googleOAuthEndpoint` / `runLoginWizard`

- [ ] **Step 3: Implement the OAuth gather step + orchestrator**

Add to `login_wizard.go`. Add imports `context`, `net`, and `golang.org/x/oauth2`, `golang.org/x/oauth2/google`:

```go
// googleOAuthEndpoint is the production OAuth endpoint. It is a package var so
// tests can point the token exchange at an httptest server.
var googleOAuthEndpoint = google.Endpoint

// wizardGatherRefreshToken reuses an existing sign-in or runs the loopback OAuth
// flow to mint a new refresh token.
func wizardGatherRefreshToken(ctx context.Context, p prompter, out io.Writer, creds clientCreds, cfg *Config, openFn func(string) error, port int) (string, error) {
	if creds.kind == "authorized_user" && creds.refreshToken != "" {
		return creds.refreshToken, nil
	}
	if cfg.RefreshToken != "" && creds.kind == "config" {
		choice, err := p.line("   Reuse your existing sign-in, or sign in again? [reuse/new]: ")
		if err != nil {
			return "", err
		}
		if choice == "" || strings.HasPrefix(strings.ToLower(choice), "r") {
			fmt.Fprintln(out, "   reusing existing sign-in.")
			return cfg.RefreshToken, nil
		}
	}
	conf := &oauth2.Config{
		ClientID:     creds.clientID,
		ClientSecret: creds.clientSecret,
		Endpoint:     googleOAuthEndpoint,
		RedirectURL:  fmt.Sprintf("http://localhost:%d", port),
		Scopes:       []string{adwordsScope},
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("listen on %s: %w — is the port busy? pass --port", addr, err)
	}
	fmt.Fprintln(out, "   Opening Google sign-in…")
	fmt.Fprintf(out, "   Waiting for callback on http://localhost:%d …\n", port)
	code, err := runLoopbackOAuth(ctx, conf, openFn, ln)
	if err != nil {
		return "", err
	}
	rt, err := exchangeRefreshToken(ctx, conf, code)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(out, "   ✓ Signed in. Got your refresh token.")
	return rt, nil
}

// runLoginWizard guides first-time setup end to end: prerequisites, OAuth client,
// sign-in, developer token, optional MCC id, then writes config and verifies with
// a live API call.
func runLoginWizard(ctx context.Context, out io.Writer, p prompter, cfg *Config, openFn func(string) error, port int) error {
	fmt.Fprintln(out, "Welcome to goads. Let's get you connected to Google Ads.")
	fmt.Fprintln(out, "You'll need: a Google Cloud project, a Desktop-app OAuth client, and a")
	fmt.Fprintln(out, "Google Ads developer token. I'll walk you through each — about 5 minutes.")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Step 1/5 · Google Cloud project + Google Ads API")
	if err := offerToOpen(p, out, "Sign in, pick or create a project, and click Enable.", urlEnableAPI, openFn); err != nil {
		return err
	}

	fmt.Fprintln(out, "\nStep 2/5 · Desktop-app OAuth client")
	creds, err := wizardGatherClient(p, out, cfg, openFn)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "\nStep 3/5 · Sign in (browser)")
	refreshToken, err := wizardGatherRefreshToken(ctx, p, out, creds, cfg, openFn, port)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "\nStep 4/5 · Developer token")
	devToken, err := wizardGatherDeveloperToken(p, out, cfg, openFn)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "\nStep 5/5 · Manager (MCC) account ID — optional")
	loginCID, err := wizardGatherLoginCustomerID(p, out, cfg)
	if err != nil {
		return err
	}

	target, err := configWriteTarget(configPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nSaving to %s …\n", target)
	if err := mergeConfigValues(target, map[string]string{
		"client_id":         creds.clientID,
		"client_secret":     creds.clientSecret,
		"refresh_token":     refreshToken,
		"developer_token":   devToken,
		"login_customer_id": loginCID,
	}); err != nil {
		return err
	}

	// Verify with the live config (config is already saved — nothing is lost on failure).
	final := *cfg
	final.ClientID = creds.clientID
	final.ClientSecret = creds.clientSecret
	final.RefreshToken = refreshToken
	final.DeveloperToken = devToken
	final.LoginCustomerID = loginCID
	fmt.Fprint(out, "Verifying… ")
	client, err := NewClient(ctx, &final)
	if err == nil {
		var ids []string
		ids, err = client.ListAccessibleCustomers(ctx)
		if err == nil {
			dashed := make([]string, len(ids))
			for i, id := range ids {
				dashed[i] = dashCustomerID(id)
			}
			fmt.Fprintf(out, "✓ Connected — %d accessible account(s): %s\n", len(ids), strings.Join(dashed, ", "))
			fmt.Fprintln(out, "\nYou're ready. Try:  goads accounts")
			return nil
		}
	}
	fmt.Fprintln(out, "✗")
	fmt.Fprintf(out, "Saved your config, but verification failed: %v\n", err)
	fmt.Fprintln(out, "Likely: the developer token isn't approved yet or was mistyped, or an OAuth problem.")
	fmt.Fprintln(out, "Fix it and re-run `goads login`, or run `goads doctor`.")
	return fmt.Errorf("verification failed: %w", err)
}
```

- [ ] **Step 4: Run the happy-path test**

Run: `go test ./... -run 'RunLoginWizard_HappyPath' -count=1`
Expected: PASS

- [ ] **Step 5: Wire the command branch + `--no-input` flag**

In `login.go`, add the flag var near the other login flag vars:

```go
	loginNoInput bool
```

Add to `login.go`'s `init()`:

```go
	loginCmd.Flags().BoolVar(&loginNoInput, "no-input", false, "never prompt; fail if credentials are missing (for scripts/CI)")
```

Add `"golang.org/x/term"` to `login.go`'s imports, and add the detector + branch. Add this helper above `loginCmd`:

```go
// isInteractiveLogin reports whether `goads login` should run the guided wizard:
// stdin is a real terminal, --no-input was not passed, and the non-interactive
// --credentials shortcut was not used.
func isInteractiveLogin() bool {
	if loginNoInput || loginCredentialsPath != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}
```

Make `loginCmd.RunE` branch at the very top (before the existing body):

```go
	RunE: func(cmd *cobra.Command, _ []string) error {
		if isInteractiveLogin() {
			cfg, err := loadLoginConfig(configPath)
			if err != nil {
				return err
			}
			openFn := openBrowser
			if loginNoBrowser {
				openFn = func(u string) error {
					fmt.Fprintf(cmd.OutOrStdout(), "Open this URL:\n  %s\n", u)
					return nil
				}
			}
			p := newTTYPrompter(os.Stdin, cmd.OutOrStdout(), int(os.Stdin.Fd()))
			return runLoginWizard(cmd.Context(), cmd.OutOrStdout(), p, cfg, openFn, loginPort)
		}

		// --- non-interactive path (unchanged) ---
		cfg, err := loadLoginConfig(configPath)
		// ... the existing body stays exactly as-is from here down ...
```

Leave the entire existing non-interactive body in place after the branch. (The existing `cfg, err := loadLoginConfig(configPath)` line becomes the first line of the non-interactive branch.)

Then make the OAuth endpoint a single source of truth: in the non-interactive body, change `Endpoint: google.Endpoint,` to `Endpoint: googleOAuthEndpoint,`. That removes login.go's last direct use of the `google` package, so DELETE `"golang.org/x/oauth2/google"` from `login.go`'s imports (it now lives only in login_wizard.go where `googleOAuthEndpoint` is defined). `go build` will fail on the unused import if you forget — that's the safety net.

- [ ] **Step 6: Add the non-interactive guard test**

Add to `login_wizard_test.go`:

```go
func TestIsInteractiveLogin_NoInputFlag(t *testing.T) {
	// --no-input forces non-interactive regardless of TTY.
	loginNoInput = true
	t.Cleanup(func() { loginNoInput = false })
	if isInteractiveLogin() {
		t.Error("--no-input must force non-interactive")
	}
}

func TestIsInteractiveLogin_CredentialsFlag(t *testing.T) {
	loginCredentialsPath = "/some/file.json"
	t.Cleanup(func() { loginCredentialsPath = "" })
	if isInteractiveLogin() {
		t.Error("--credentials must force non-interactive")
	}
}
```

- [ ] **Step 7: Full verification**

Run each and confirm clean:

```bash
go fmt ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck ./...
go mod tidy
go test ./... -count=1
go build -o build/goads . && ./build/goads login --help
```
Expected: vet/staticcheck silent (zero warnings — every wizard symbol now has a call site), `go mod tidy` makes no further changes, all tests PASS, and `login --help` lists `--credentials`, `--port`, `--no-browser`, `--no-input`.

- [ ] **Step 8: Commit**

```bash
git add login.go login_wizard.go login_wizard_test.go go.mod go.sum
git commit -m "feat: interactive goads login wizard with live verification"
```

---

## Notes for the implementer

- **Two `prompter` consumers, one fake:** the wizard only ever talks to the `prompter` interface and an `openFn` and an `io.Writer`. That is what makes every step testable offline — never read `os.Stdin` directly inside a wizard step.
- **`googleOAuthEndpoint` is the only OAuth seam:** production keeps `google.Endpoint`; tests reassign it (with `t.Cleanup` to restore). Do not thread an endpoint parameter through every function.
- **Config is written once, before verify:** so a verify failure never loses collected input. Do not write incrementally.
- **`offerToOpen` is for prerequisite pages only.** The OAuth step (Step 3) auto-opens via `openFn` directly. Don't route Step 3 through `offerToOpen`.
- **Don't touch `mcp.go`.** The wizard is CLI-only.
- **Global flag vars leak between tests** — any test that sets `loginNoInput` / `loginCredentialsPath` / `configPath` must reset them with `t.Cleanup`.
- **Import grouping:** let `go fmt`/`goimports` order imports; remove any you didn't use.
```
