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
			return clientCreds{}, fmt.Errorf("credentials file %q: %w", credentialsPath, err)
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
