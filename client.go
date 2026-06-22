package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// defaultBaseURL is the Google Ads REST API v23 base. Override via
// GOOGLE_ADS_API_BASE_URL (config.BaseURL) — tests point it at httptest.
const defaultBaseURL = "https://googleads.googleapis.com/v23"

// Client talks to the Google Ads REST API. It is safe for concurrent use.
//
// Note: this is the REST/JSON API (HTTP POST), not gRPC — there is no generated
// protobuf code. Request and response bodies are plain JSON.
type Client struct {
	cfg    *Config
	http   *http.Client
	tokens oauth2.TokenSource
}

// NewClient builds a Client from config, validating credentials first.
func NewClient(ctx context.Context, cfg *Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Client{
		cfg:    cfg,
		http:   &http.Client{Timeout: 60 * time.Second},
		tokens: newTokenSource(ctx, cfg),
	}, nil
}

// buildHeaders sets the three headers every Google Ads REST call needs:
// the OAuth bearer token, the developer token, and (optionally) the
// login-customer-id of the manager account.
func (c *Client) buildHeaders(req *http.Request) error {
	tok, err := c.tokens.Token()
	if err != nil {
		return fmt.Errorf("obtain access token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	dev := c.cfg.DeveloperToken
	if dev == "" && c.cfg.isTest() {
		dev = "test-developer-token"
	}
	req.Header.Set("developer-token", dev)

	if c.cfg.LoginCustomerID != "" {
		req.Header.Set("login-customer-id", c.cfg.LoginCustomerID)
	}
	return nil
}

// post sends a JSON body to {baseURL}/{path} and decodes the JSON response into
// out. It surfaces Google Ads API error payloads as Go errors.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}
	url := c.cfg.BaseURL + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
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

// apiError turns a non-2xx Google Ads response into a readable error.
func apiError(status int, body []byte) error {
	var e struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("google ads API %d (%s): %s", status, e.Error.Status, e.Error.Message)
	}
	return fmt.Errorf("google ads API %d: %s", status, string(body))
}

// --- Operations -----------------------------------------------------------
//
// Only the operations needed by the currently-ported tools are implemented.
// The upstream server uses exactly five REST endpoints; add the rest as tools
// are ported (see docs/PORTING.md):
//
//   customers/{id}/googleAds:search          -> Search
//   customers/{id}/googleAds:mutate          -> Mutate
//   customers/{id}:generateKeywordIdeas      -> (todo)
//   customers/{id}/recommendations:apply     -> (todo)
//   customers/{id}/recommendations:dismiss   -> (todo)

// searchResponse is one page of a googleAds:search call.
type searchResponse struct {
	Results       []json.RawMessage `json:"results"`
	NextPageToken string            `json:"nextPageToken"`
}

// Search runs a GAQL query for one customer and returns every result row,
// transparently following pagination.
func (c *Client) Search(ctx context.Context, customerID, query string) ([]json.RawMessage, error) {
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s/googleAds:search", customerID)

	var rows []json.RawMessage
	pageToken := ""
	for {
		body := map[string]any{"query": query}
		if pageToken != "" {
			body["pageToken"] = pageToken
		}
		var page searchResponse
		if err := c.post(ctx, path, body, &page); err != nil {
			return nil, err
		}
		rows = append(rows, page.Results...)
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return rows, nil
}

// MutateResponse is the result of a googleAds:mutate call.
type MutateResponse struct {
	Results       []json.RawMessage `json:"results"`
	PartialErrors json.RawMessage   `json:"partialFailureError,omitempty"`
}

// Mutate applies a batch of mutate operations for one customer. Callers are
// responsible for guarding writes (see safety.go) before reaching this point.
func (c *Client) Mutate(ctx context.Context, customerID string, ops []any) (*MutateResponse, error) {
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s/googleAds:mutate", customerID)
	body := map[string]any{"mutateOperations": ops}
	var out MutateResponse
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
