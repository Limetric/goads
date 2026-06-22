package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gaqlServer returns a fake Ads API that asserts the request hits a search
// endpoint, optionally checks the GAQL query contains each wantInQuery
// substring, and replies with the given result rows.
func gaqlServer(t *testing.T, results string, wantInQuery ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "googleAds:search") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, want := range wantInQuery {
			if !strings.Contains(body.Query, want) {
				t.Errorf("query missing %q:\n%s", want, body.Query)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":` + results + `}`))
	}))
}

// errServer returns a fake Ads API that always replies with an API error.
func errServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"bad field"}}`))
	}))
}

func TestRunCampaigns(t *testing.T) {
	results := `[{"campaign":{"id":"1","name":"A"},"metrics":{"cost_micros":"5000000","conversions":2}}]`
	srv := gaqlServer(t, results, "FROM campaign", "campaign.status != 'REMOVED'", "ORDER BY metrics.cost_micros DESC")
	defer srv.Close()

	res, err := runCampaigns(t.Context(), newTestClient(t, srv), CampaignsArgs{CustomerID: "123-456-7890"})
	if err != nil {
		t.Fatalf("runCampaigns: %v", err)
	}
	if res.TotalCount != 1 {
		t.Fatalf("TotalCount = %d, want 1", res.TotalCount)
	}
	// Cost enriched to readable and CPA computed (5.00 / 2 conversions = 2.50).
	row := string(res.Campaigns[0])
	if !strings.Contains(row, `"cost_readable":"5.00"`) {
		t.Errorf("cost not enriched: %s", row)
	}
	if !strings.Contains(row, `"cpa":"2.50"`) {
		t.Errorf("cpa not computed: %s", row)
	}
}

func TestRunCampaigns_WithDates(t *testing.T) {
	srv := gaqlServer(t, `[]`, "segments.date BETWEEN '2024-01-01' AND '2024-01-31'")
	defer srv.Close()
	if _, err := runCampaigns(t.Context(), newTestClient(t, srv), CampaignsArgs{
		CustomerID: "1", DateStart: "2024-01-01", DateEnd: "2024-01-31",
	}); err != nil {
		t.Fatalf("runCampaigns: %v", err)
	}
}

func TestRunCampaigns_APIError(t *testing.T) {
	srv := errServer(t)
	defer srv.Close()
	if _, err := runCampaigns(t.Context(), newTestClient(t, srv), CampaignsArgs{CustomerID: "1"}); err == nil {
		t.Fatal("expected API error")
	}
}

func TestRunCampaigns_RequiresCustomerID(t *testing.T) {
	if _, err := runCampaigns(t.Context(), nil, CampaignsArgs{}); err == nil {
		t.Fatal("expected error for missing customer_id")
	}
}
