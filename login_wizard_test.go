package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"golang.org/x/oauth2"
)

// fakePrompter returns scripted answers per method, in order.
type fakePrompter struct {
	lines      []string
	secrets    []string
	confirms   []bool
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
	if creds.kind != "installed" {
		t.Errorf("kind = %q, want \"installed\"", creds.kind)
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

func TestExpandHome(t *testing.T) {
	if got := expandHome(`  "/tmp/x.json"  `); got != "/tmp/x.json" {
		t.Errorf("quote/space strip: got %q", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandHome("~/Downloads/c.json"); got != filepath.Join(home, "Downloads/c.json") {
		t.Errorf("~/ expansion: got %q", got)
	}
}

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
