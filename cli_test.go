package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI executes the root command with args, capturing combined output. It
// isolates config to a temp HOME and resets the shared command's args after.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	configPath = ""
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		configPath = ""
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return out.String(), err
}

func TestCLI_ConfigPath(t *testing.T) {
	t.Run("explicit path", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "custom config.toml")
		out, err := runCLI(t, "config", "path", "--config", path)
		if err != nil {
			t.Fatalf("execute config path: %v\noutput: %s", err, out)
		}
		if want := path + "\n"; out != want {
			t.Errorf("config path output = %q, want %q", out, want)
		}
	})

	t.Run("default path", func(t *testing.T) {
		useTempState(t)
		dir, err := userConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, defaultConfigFile)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		out, err := runCLI(t, "config", "path")
		if err != nil {
			t.Fatalf("execute config path: %v\noutput: %s", err, out)
		}
		if want := path + "\n"; out != want {
			t.Errorf("config path output = %q, want %q", out, want)
		}
	})

	t.Run("environment only", func(t *testing.T) {
		useTempState(t)
		t.Setenv("GOOGLE_ADS_DEVELOPER_TOKEN", "must-not-be-printed")
		out, err := runCLI(t, "config", "path")
		if err != nil {
			t.Fatalf("execute config path: %v\noutput: %s", err, out)
		}
		if want := "environment only (no config file)\n"; out != want {
			t.Errorf("config path output = %q, want %q", out, want)
		}
		if strings.Contains(out, "must-not-be-printed") {
			t.Errorf("config path output leaked a credential: %q", out)
		}
	})

	t.Run("rejects arguments", func(t *testing.T) {
		if out, err := runCLI(t, "config", "path", "extra"); err == nil {
			t.Fatalf("config path should reject arguments; output: %s", out)
		}
	})
}

func TestCLI_AccountsCommand(t *testing.T) {
	useTempState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"customerClient":{"id":"111"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("GOOGLE_ADS_API_BASE_URL", srv.URL) // non-prod → skips OAuth/creds
	t.Setenv("GOOGLE_ADS_LOGIN_CUSTOMER_ID", "1234567890")

	out, err := runCLI(t, "accounts")
	if err != nil {
		t.Fatalf("execute accounts: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "customer_ids") || !strings.Contains(out, "111") {
		t.Errorf("unexpected accounts output:\n%s", out)
	}
}

func TestCLI_DoctorReportsMissingCredentials(t *testing.T) {
	useTempState(t)
	clearAdsEnv(t) // production base URL, no creds → NOT READY

	out, err := runCLI(t, "doctor")
	if err == nil {
		t.Errorf("doctor should fail when credentials are missing")
	}
	for _, want := range []string{"developer token:    MISSING", "login customer id:  (none)", "NOT READY"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestCLI_RejectsUnknownCommand(t *testing.T) {
	if _, err := runCLI(t, "definitely-not-a-command"); err == nil {
		t.Error("expected an error for an unknown command")
	}
}
