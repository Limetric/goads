package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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

// get issues a GET to {baseURL}/{path} and decodes the JSON response into out.
// Mirrors post for read-only endpoints that take no body.
func (c *Client) get(ctx context.Context, path string, out any) error {
	url := c.cfg.BaseURL + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

// apiStatusError is a non-2xx Google Ads API response. It carries the HTTP
// status so callers can distinguish a definitive client error (4xx — the
// request or credentials are wrong) from a transient server error (5xx). A
// transport failure (no response at all) is a plain error, not this type.
type apiStatusError struct {
	status int
	msg    string
}

func (e *apiStatusError) Error() string { return e.msg }

// isClientError reports whether the status is 4xx: the server understood the
// request and rejected it based on what we sent (bad credentials, permission,
// arguments), i.e. a setup problem the user must fix.
func (e *apiStatusError) isClientError() bool { return e.status >= 400 && e.status < 500 }

// apiErrorDetail mirrors one entry of a Google Ads error's details[] array
// (the GoogleAdsFailure payload) — the part that carries the specific,
// actionable error code and message.
type apiErrorDetail struct {
	Errors []struct {
		ErrorCode map[string]any `json:"errorCode"`
		Message   string         `json:"message"`
	} `json:"errors"`
}

// apiError turns a non-2xx Google Ads response into a readable error.
//
// The top-level message is often generic ("The caller does not have
// permission"). The actionable detail — the specific errorCode and its
// human-readable message (e.g. DEVELOPER_TOKEN_NOT_APPROVED, "apply for Basic
// or Standard access") — lives in error.details[].errors[] of a GoogleAdsFailure.
// We surface those so the CLI tells the user what to actually fix.
func apiError(status int, body []byte) error {
	var e struct {
		Error struct {
			Code    int              `json:"code"`
			Message string           `json:"message"`
			Status  string           `json:"status"`
			Details []apiErrorDetail `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		msg := fmt.Sprintf("google ads API %d (%s): %s", status, e.Error.Status, e.Error.Message)
		if detail := formatAdsFailures(e.Error.Details); detail != "" {
			msg += " — " + detail
		}
		return &apiStatusError{status: status, msg: msg}
	}
	return &apiStatusError{status: status, msg: fmt.Sprintf("google ads API %d: %s", status, string(body))}
}

// formatAdsFailures flattens the GoogleAdsFailure error list into a single
// readable string, e.g.
// "authorizationError.DEVELOPER_TOKEN_NOT_APPROVED: The developer token is …".
// Multiple errors are joined with " | ". Returns "" when there is nothing extra
// to add beyond the top-level message.
func formatAdsFailures(details []apiErrorDetail) string {
	var parts []string
	for _, d := range details {
		for _, e := range d.Errors {
			code := formatErrorCode(e.ErrorCode)
			switch {
			case code != "" && e.Message != "":
				parts = append(parts, code+": "+e.Message)
			case code != "":
				parts = append(parts, code)
			case e.Message != "":
				parts = append(parts, e.Message)
			}
		}
	}
	return strings.Join(parts, " | ")
}

// formatErrorCode renders an errorCode object like
// {"authorizationError":"DEVELOPER_TOKEN_NOT_APPROVED"} as
// "authorizationError.DEVELOPER_TOKEN_NOT_APPROVED". Keys are sorted so the
// output is deterministic when more than one is present (rare).
func formatErrorCode(code map[string]any) string {
	keys := make([]string, 0, len(code))
	for k := range code {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s.%v", k, code[k]))
	}
	return strings.Join(parts, ",")
}

// --- Operations -----------------------------------------------------------
//
// The upstream server uses exactly five REST endpoints, all implemented here:
//
//   customers/{id}/googleAds:search          -> Search
//   customers/{id}/googleAds:mutate          -> Mutate
//   customers/{id}:generateKeywordIdeas      -> GenerateKeywordIdeas
//   customers/{id}/recommendations:apply     -> ApplyRecommendations
//   customers/{id}/recommendations:dismiss   -> DismissRecommendations

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

// ListAccessibleCustomers returns the bare customer IDs the authenticated user
// can access. It calls customers:listAccessibleCustomers, which needs only a
// valid OAuth token and developer token — no customer or login-customer-id — so
// it is the right call to verify a fresh setup works end to end.
func (c *Client) ListAccessibleCustomers(ctx context.Context) ([]string, error) {
	var out struct {
		ResourceNames []string `json:"resourceNames"`
	}
	if err := c.get(ctx, "customers:listAccessibleCustomers", &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.ResourceNames))
	for _, rn := range out.ResourceNames {
		ids = append(ids, strings.TrimPrefix(rn, "customers/"))
	}
	return ids, nil
}

// MutateResponse is the result of a googleAds:mutate call.
type MutateResponse struct {
	Results       []json.RawMessage `json:"results"`
	PartialErrors json.RawMessage   `json:"partialFailureError,omitempty"`
}

// Mutate applies a batch of mutate operations for one customer. Callers are
// responsible for guarding writes (see safety.go) before reaching this point.
//
// Unknown top-level operation keys are rejected client-side (validateMutateOps)
// before any HTTP traffic, and partialFailure is enabled so a bad op in a batch
// surfaces as a per-op error rather than failing the whole request.
func (c *Client) Mutate(ctx context.Context, customerID string, ops []any) (*MutateResponse, error) {
	if err := validateMutateOps(ops); err != nil {
		return nil, err
	}
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s/googleAds:mutate", customerID)
	body := map[string]any{"mutateOperations": ops, "partialFailure": true}
	var out MutateResponse
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateKeywordIdeas calls the Keyword Planner generateKeywordIdeas endpoint
// for a set of seed keywords, returning the raw idea result rows.
func (c *Client) GenerateKeywordIdeas(ctx context.Context, customerID string, seedKeywords []string, pageSize int) ([]json.RawMessage, error) {
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s:generateKeywordIdeas", customerID)
	if pageSize <= 0 {
		pageSize = 50
	}
	body := map[string]any{
		"keywordSeed":        map[string]any{"keywords": seedKeywords},
		"language":           "languageConstants/1000",
		"pageSize":           pageSize,
		"keywordPlanNetwork": "GOOGLE_SEARCH",
	}
	var out struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// RecommendationResponse is the result of a recommendations:apply or
// recommendations:dismiss call.
type RecommendationResponse struct {
	Results       []json.RawMessage `json:"results"`
	PartialErrors json.RawMessage   `json:"partialFailureError,omitempty"`
}

// recommendationOps turns full recommendation resource names into the
// {"resourceName": …} operation objects both RPCs expect.
func recommendationOps(resourceNames []string) []map[string]any {
	ops := make([]map[string]any, len(resourceNames))
	for i, rn := range resourceNames {
		ops[i] = map[string]any{"resourceName": rn}
	}
	return ops
}

// ApplyRecommendations applies recommendations via the dedicated
// recommendations:apply RPC. resourceNames must be full paths
// (customers/{cid}/recommendations/{id}).
func (c *Client) ApplyRecommendations(ctx context.Context, customerID string, resourceNames []string) (*RecommendationResponse, error) {
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s/recommendations:apply", customerID)
	body := map[string]any{"operations": recommendationOps(resourceNames), "partialFailure": true}
	var out RecommendationResponse
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DismissRecommendations dismisses recommendations via the dedicated
// recommendations:dismiss RPC.
func (c *Client) DismissRecommendations(ctx context.Context, customerID string, resourceNames []string) (*RecommendationResponse, error) {
	customerID = normalizeCustomerID(customerID)
	path := fmt.Sprintf("customers/%s/recommendations:dismiss", customerID)
	body := map[string]any{"operations": recommendationOps(resourceNames)}
	var out RecommendationResponse
	if err := c.post(ctx, path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
