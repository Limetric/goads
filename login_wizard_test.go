package main

import (
	"io"
	"os"
	"path/filepath"
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
