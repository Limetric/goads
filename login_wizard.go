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
