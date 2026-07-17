package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds everything needed to talk to the Google Ads API.
//
// Resolution order (later wins): TOML file (if present) < environment
// variables. Credentials are most commonly supplied via the environment so the
// MCP host config and CI never need a file on disk.
type Config struct {
	// DeveloperToken is the Google Ads API developer token.
	DeveloperToken string `toml:"developer_token"`
	// ClientID / ClientSecret / RefreshToken are the installed-app OAuth2
	// credentials used to mint access tokens (see auth.go).
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	RefreshToken string `toml:"refresh_token"`
	// LoginCustomerID is the manager (MCC) account used for the
	// `login-customer-id` header. Optional; dashes are stripped.
	LoginCustomerID string `toml:"login_customer_id"`
	// DefaultCustomerID is the customer ID used when a command/tool call does
	// not pass one explicitly. Optional; dashes are stripped. Set it with
	// `goads config set-customer` or GOOGLE_ADS_CUSTOMER_ID.
	DefaultCustomerID string `toml:"default_customer_id"`
	// BaseURL overrides the API base (default defaultBaseURL). Set this to an
	// httptest server in tests, or to a regional endpoint if needed.
	BaseURL string `toml:"base_url"`
}

// loadConfig reads configuration from the given file (optional) and overlays
// environment variables on top. An empty path means "use the default path if it
// exists, otherwise env only".
func loadConfig(path string) (*Config, error) {
	cfg := &Config{}

	resolved, err := resolveConfigPath(path)
	if err != nil {
		return nil, err
	}
	if resolved != "" {
		if _, err := toml.DecodeFile(resolved, cfg); err != nil {
			return nil, fmt.Errorf("read config %q: %w", resolved, err)
		}
	}

	cfg.finalize()
	return cfg, nil
}

// finalize overlays environment variables on top of any file values and applies
// defaults/normalization. It is the shared tail of config loading so callers
// (e.g. loadLoginConfig) can build an env-only config without a file.
func (cfg *Config) finalize() {
	overlayEnv(cfg)
	cfg.LoginCustomerID = normalizeCustomerID(cfg.LoginCustomerID)
	cfg.DefaultCustomerID = normalizeCustomerID(cfg.DefaultCustomerID)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
}

func overlayEnv(cfg *Config) {
	for env, dst := range map[string]*string{
		"GOOGLE_ADS_DEVELOPER_TOKEN":   &cfg.DeveloperToken,
		"GOOGLE_ADS_CLIENT_ID":         &cfg.ClientID,
		"GOOGLE_ADS_CLIENT_SECRET":     &cfg.ClientSecret,
		"GOOGLE_ADS_REFRESH_TOKEN":     &cfg.RefreshToken,
		"GOOGLE_ADS_LOGIN_CUSTOMER_ID": &cfg.LoginCustomerID,
		"GOOGLE_ADS_CUSTOMER_ID":       &cfg.DefaultCustomerID,
		"GOOGLE_ADS_API_BASE_URL":      &cfg.BaseURL,
	} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			*dst = v
		}
	}
}

// validate reports whether the config is usable for real API calls. It is
// intentionally lenient when BaseURL points away from production (tests).
func (c *Config) validate() error {
	if c.isTest() {
		return nil
	}
	var missing []string
	if c.DeveloperToken == "" {
		missing = append(missing, "GOOGLE_ADS_DEVELOPER_TOKEN")
	}
	if c.ClientID == "" {
		missing = append(missing, "GOOGLE_ADS_CLIENT_ID")
	}
	if c.ClientSecret == "" {
		missing = append(missing, "GOOGLE_ADS_CLIENT_SECRET")
	}
	if c.RefreshToken == "" {
		missing = append(missing, "GOOGLE_ADS_REFRESH_TOKEN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing credentials: %s — set them in the environment or a --config TOML file (see `goads doctor`)", strings.Join(missing, ", "))
	}
	return nil
}

// isTest reports whether we're pointed at a local/offline base URL (httptest
// servers and the like), in which case auth and credential checks are relaxed.
//
// Only loopback hosts count as test mode. Any remote endpoint — a regional
// endpoint, proxy, future API version, even plain-HTTP — is a real target and
// keeps the user's real credentials: inferring test mode from any URL
// difference used to silently swap in fake credentials (issue #5).
func (c *Config) isTest() bool {
	if c.BaseURL == "" || c.BaseURL == defaultBaseURL {
		return false
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// normalizeCustomerID strips dashes and whitespace ("123-456-7890" -> "1234567890").
func normalizeCustomerID(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), "-", "")
}
