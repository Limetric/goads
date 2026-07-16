package main

import (
	"context"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// staticTokenSource always returns the same token. Test mode uses it so the
// suite never needs real OAuth credentials.
type staticTokenSource struct{ tok *oauth2.Token }

func (s staticTokenSource) Token() (*oauth2.Token, error) { return s.tok, nil }

// newTokenSource builds an oauth2.TokenSource for the configured credentials.
//
// Google Ads uses the installed-app refresh-token flow: client_id +
// client_secret + a long-lived refresh_token are exchanged for short-lived
// access tokens. oauth2.TokenSource caches the access token and refreshes it
// automatically when it expires.
func newTokenSource(ctx context.Context, cfg *Config) oauth2.TokenSource {
	if cfg.isTest() {
		return staticTokenSource{tok: &oauth2.Token{AccessToken: "test-access-token"}}
	}
	conf := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
	}
	// A token carrying only a refresh token; the source mints access tokens.
	return conf.TokenSource(ctx, &oauth2.Token{RefreshToken: cfg.RefreshToken})
}
