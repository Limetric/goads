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
		if err != nil {
			return "", err
		}
		fmt.Fprintln(p.out) // ReadPassword swallows the newline; restore it on success
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
// Secrets here are ASCII (OAuth client IDs, developer tokens), so byte slicing is safe.
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
