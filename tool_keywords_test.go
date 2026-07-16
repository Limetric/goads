package main

import (
	"strings"
	"testing"
)

func TestRunKeywordPerformance(t *testing.T) {
	results := `[{"adGroupCriterion":{"keyword":{"text":"shoes"}},"metrics":{"cost_micros":"2000000"}}]`
	srv := gaqlServer(t, results, "FROM keyword_view", "ad_group_criterion.status != 'REMOVED'", "segments.date DURING LAST_30_DAYS")
	defer srv.Close()

	res, err := runKeywordPerformance(t.Context(), newTestClient(t, srv), KeywordPerformanceArgs{CustomerID: "1"})
	if err != nil {
		t.Fatalf("runKeywordPerformance: %v", err)
	}
	if res.TotalCount != 1 || !strings.Contains(string(res.Keywords[0]), `"cost_readable":"2.00"`) {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestRunKeywordPerformance_WithDates(t *testing.T) {
	srv := gaqlServer(t, `[]`, "segments.date BETWEEN '2024-02-01' AND '2024-02-28'")
	defer srv.Close()
	if _, err := runKeywordPerformance(t.Context(), newTestClient(t, srv), KeywordPerformanceArgs{CustomerID: "1", DateStart: "2024-02-01", DateEnd: "2024-02-28"}); err != nil {
		t.Fatalf("runKeywordPerformance: %v", err)
	}
}

func TestRunSearchTerms_DefaultDateRange(t *testing.T) {
	srv := gaqlServer(t, `[]`, "FROM search_term_view", "segments.date DURING LAST_30_DAYS", "LIMIT 200")
	defer srv.Close()
	if _, err := runSearchTerms(t.Context(), newTestClient(t, srv), SearchTermsArgs{CustomerID: "1"}); err != nil {
		t.Fatalf("runSearchTerms: %v", err)
	}
}

func TestRunSearchTerms_ExplicitDates(t *testing.T) {
	srv := gaqlServer(t, `[]`, "segments.date BETWEEN '2024-03-01' AND '2024-03-31'")
	defer srv.Close()
	if _, err := runSearchTerms(t.Context(), newTestClient(t, srv), SearchTermsArgs{CustomerID: "1", DateStart: "2024-03-01", DateEnd: "2024-03-31"}); err != nil {
		t.Fatalf("runSearchTerms: %v", err)
	}
}

func TestRunNegativeKeywords(t *testing.T) {
	srv := gaqlServer(t, `[{"campaignCriterion":{"negative":true}}]`, "FROM campaign_criterion", "campaign_criterion.negative = TRUE")
	defer srv.Close()
	res, err := runNegativeKeywords(t.Context(), newTestClient(t, srv), NegativeKeywordsArgs{CustomerID: "1"})
	if err != nil {
		t.Fatalf("runNegativeKeywords: %v", err)
	}
	if res.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1", res.TotalCount)
	}
}

func TestKeywordTools_RequireCustomerID(t *testing.T) {
	if _, err := runKeywordPerformance(t.Context(), nil, KeywordPerformanceArgs{}); err == nil {
		t.Error("keyword performance should require customer_id")
	}
	if _, err := runSearchTerms(t.Context(), nil, SearchTermsArgs{}); err == nil {
		t.Error("search terms should require customer_id")
	}
	if _, err := runNegativeKeywords(t.Context(), nil, NegativeKeywordsArgs{}); err == nil {
		t.Error("negative keywords should require customer_id")
	}
}

func TestKeywordTools_APIError(t *testing.T) {
	srv := errServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := runKeywordPerformance(t.Context(), c, KeywordPerformanceArgs{CustomerID: "1"}); err == nil {
		t.Error("expected API error")
	}
}
