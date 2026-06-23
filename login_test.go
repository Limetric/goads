package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestParseCredentialsJSON(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		wantKind    string
		wantID      string
		wantSecret  string
		wantRefresh string
		wantErr     bool
	}{
		{
			name:     "installed desktop app",
			data:     `{"installed":{"client_id":"cid","client_secret":"csec"}}`,
			wantKind: "installed", wantID: "cid", wantSecret: "csec",
		},
		{
			name:     "web application",
			data:     `{"web":{"client_id":"wid","client_secret":"wsec"}}`,
			wantKind: "web", wantID: "wid", wantSecret: "wsec",
		},
		{
			name:     "authorized_user",
			data:     `{"type":"authorized_user","client_id":"aid","client_secret":"asec","refresh_token":"rtok"}`,
			wantKind: "authorized_user", wantID: "aid", wantSecret: "asec", wantRefresh: "rtok",
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
			if got.kind != tt.wantKind || got.clientID != tt.wantID || got.clientSecret != tt.wantSecret || got.refreshToken != tt.wantRefresh {
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
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("fresh file perm = %v, want 0600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm = %v, want 0700", perm)
	}
}
