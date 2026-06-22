package main

import (
	"strings"
	"testing"
)

func TestRunGeoTargets(t *testing.T) {
	srv := gaqlServer(t, `[{"geoTargetConstant":{"id":"21137","name":"California"}}]`,
		"FROM geo_target_constant", "geo_target_constant.name LIKE '%California%'")
	defer srv.Close()
	res, err := runGeoTargets(t.Context(), newTestClient(t, srv), GeoTargetsArgs{CustomerID: "1", Query: "California"})
	if err != nil {
		t.Fatalf("runGeoTargets: %v", err)
	}
	if res.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1", res.TotalCount)
	}
}

func TestRunGeoTargets_EscapesQuote(t *testing.T) {
	srv := gaqlServer(t, `[]`, `LIKE '%O\'Fallon%'`)
	defer srv.Close()
	if _, err := runGeoTargets(t.Context(), newTestClient(t, srv), GeoTargetsArgs{CustomerID: "1", Query: "O'Fallon"}); err != nil {
		t.Fatalf("runGeoTargets: %v", err)
	}
}

func TestRunGeoTargets_RequiresQuery(t *testing.T) {
	if _, err := runGeoTargets(t.Context(), nil, GeoTargetsArgs{CustomerID: "1"}); err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestRunGeoPerformance_DefaultDates(t *testing.T) {
	srv := gaqlServer(t, `[{"metrics":{"cost_micros":"7000000"}}]`, "FROM geographic_view", "DURING LAST_30_DAYS")
	defer srv.Close()
	res, err := runGeoPerformance(t.Context(), newTestClient(t, srv), GeoPerformanceArgs{CustomerID: "1"})
	if err != nil {
		t.Fatalf("runGeoPerformance: %v", err)
	}
	if !strings.Contains(string(res.GeoPerformance[0]), `"cost_readable":"7.00"`) {
		t.Errorf("cost not enriched: %s", res.GeoPerformance[0])
	}
}

func TestRunGeoPerformance_ExplicitDates(t *testing.T) {
	srv := gaqlServer(t, `[]`, "segments.date BETWEEN '2024-04-01' AND '2024-04-30'")
	defer srv.Close()
	if _, err := runGeoPerformance(t.Context(), newTestClient(t, srv), GeoPerformanceArgs{CustomerID: "1", DateStart: "2024-04-01", DateEnd: "2024-04-30"}); err != nil {
		t.Fatalf("runGeoPerformance: %v", err)
	}
}
