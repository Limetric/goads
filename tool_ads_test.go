package main

import (
	"strings"
	"testing"
)

func TestRunAds(t *testing.T) {
	results := `[{"adGroupAd":{"ad":{"id":"7"}},"metrics":{"cost_micros":"1000000"}}]`
	srv := gaqlServer(t, results, "FROM ad_group_ad", "ad_group_ad.status != 'REMOVED'", "ORDER BY metrics.cost_micros DESC")
	defer srv.Close()

	res, err := runAds(t.Context(), newTestClient(t, srv), AdsArgs{CustomerID: "1"})
	if err != nil {
		t.Fatalf("runAds: %v", err)
	}
	if res.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1", res.TotalCount)
	}
	if !strings.Contains(string(res.Ads[0]), `"cost_readable":"1.00"`) {
		t.Errorf("cost not enriched: %s", res.Ads[0])
	}
}

func TestRunAds_WithDates(t *testing.T) {
	srv := gaqlServer(t, `[]`, "segments.date BETWEEN '2024-02-01' AND '2024-02-28'")
	defer srv.Close()
	if _, err := runAds(t.Context(), newTestClient(t, srv), AdsArgs{CustomerID: "1", DateStart: "2024-02-01", DateEnd: "2024-02-28"}); err != nil {
		t.Fatalf("runAds: %v", err)
	}
}

func TestRunAds_APIError(t *testing.T) {
	srv := errServer(t)
	defer srv.Close()
	if _, err := runAds(t.Context(), newTestClient(t, srv), AdsArgs{CustomerID: "1"}); err == nil {
		t.Fatal("expected API error")
	}
}

func TestRunAds_RequiresCustomerID(t *testing.T) {
	if _, err := runAds(t.Context(), nil, AdsArgs{}); err == nil {
		t.Fatal("expected error for missing customer_id")
	}
}
