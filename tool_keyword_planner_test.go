package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunDiscoverKeywords(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":generateKeywordIdeas") {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"text":"running shoes"},{"text":"trainers"}]}`))
	}))
	defer srv.Close()

	res, err := runDiscoverKeywords(t.Context(), newTestClient(t, srv), DiscoverKeywordsArgs{
		CustomerID: "1", SeedKeywords: []string{"shoes"},
	})
	if err != nil {
		t.Fatalf("runDiscoverKeywords: %v", err)
	}
	if res.TotalCount != 2 {
		t.Errorf("TotalCount = %d, want 2", res.TotalCount)
	}
	seed, _ := gotBody["keywordSeed"].(map[string]any)
	if seed == nil {
		t.Fatalf("keywordSeed missing: %v", gotBody)
	}
}

func TestRunDiscoverKeywords_RequiresSeed(t *testing.T) {
	if _, err := runDiscoverKeywords(t.Context(), nil, DiscoverKeywordsArgs{CustomerID: "1"}); err == nil {
		t.Fatal("expected error for missing seed keywords")
	}
}

func TestRunKeywordForecasts_NoKeywords(t *testing.T) {
	res, err := runKeywordForecasts(t.Context(), nil, KeywordForecastsArgs{CustomerID: "1"})
	if err != nil {
		t.Fatalf("runKeywordForecasts: %v", err)
	}
	if res.TotalCount != 0 || res.Message == "" {
		t.Errorf("expected empty result with message, got %+v", res)
	}
}

func TestRunKeywordForecasts_Found(t *testing.T) {
	srv := gaqlServer(t, `[{"adGroupCriterion":{"keyword":{"text":"shoes"}},"metrics":{"cost_micros":"4000000"}}]`,
		"FROM keyword_view", "ad_group_criterion.keyword.text IN ('shoes')", "DURING LAST_30_DAYS")
	defer srv.Close()
	res, err := runKeywordForecasts(t.Context(), newTestClient(t, srv), KeywordForecastsArgs{CustomerID: "1", KeywordTexts: []string{"shoes"}})
	if err != nil {
		t.Fatalf("runKeywordForecasts: %v", err)
	}
	if res.TotalCount != 1 || !strings.Contains(string(res.KeywordForecasts[0]), `"cost_readable":"4.00"`) {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestRunKeywordForecasts_NoMatches(t *testing.T) {
	srv := gaqlServer(t, `[]`)
	defer srv.Close()
	res, err := runKeywordForecasts(t.Context(), newTestClient(t, srv), KeywordForecastsArgs{CustomerID: "1", KeywordTexts: []string{"nonexistent"}})
	if err != nil {
		t.Fatalf("runKeywordForecasts: %v", err)
	}
	if res.TotalCount != 0 || !strings.Contains(res.Message, "No matching keywords") {
		t.Errorf("expected no-match message, got %+v", res)
	}
}
