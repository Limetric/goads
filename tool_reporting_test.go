package main

import (
	"strings"
	"testing"
)

func TestRunReport_JSON(t *testing.T) {
	srv := gaqlServer(t, `[{"campaign":{"id":"1","name":"A"}}]`, "FROM campaign")
	defer srv.Close()
	res, err := runReport(t.Context(), newTestClient(t, srv), ReportArgs{
		CustomerID: "1", Query: "SELECT campaign.id, campaign.name FROM campaign",
	})
	if err != nil {
		t.Fatalf("runReport: %v", err)
	}
	if res.Format != "json" || res.TotalCount != 1 {
		t.Errorf("unexpected: %+v", res)
	}
	if len(res.Fields) != 2 || res.Fields[0] != "campaign.id" {
		t.Errorf("fields = %v", res.Fields)
	}
	if len(res.Results) != 1 {
		t.Errorf("expected 1 result row")
	}
}

func TestRunReport_Table(t *testing.T) {
	srv := gaqlServer(t, `[{"campaign":{"id":"1","name":"A"}}]`)
	defer srv.Close()
	res, err := runReport(t.Context(), newTestClient(t, srv), ReportArgs{
		CustomerID: "1", Query: "SELECT campaign.id, campaign.name FROM campaign", Format: "table",
	})
	if err != nil {
		t.Fatalf("runReport: %v", err)
	}
	if res.Format != "table" || !strings.Contains(res.Formatted, "campaign.id") {
		t.Errorf("table output wrong: %q", res.Formatted)
	}
	if len(res.Results) != 0 {
		t.Error("table format should not populate Results")
	}
}

func TestRunReport_CSV(t *testing.T) {
	srv := gaqlServer(t, `[{"campaign":{"id":"1","name":"A"}}]`)
	defer srv.Close()
	res, err := runReport(t.Context(), newTestClient(t, srv), ReportArgs{
		CustomerID: "1", Query: "SELECT campaign.id, campaign.name FROM campaign", Format: "CSV",
	})
	if err != nil {
		t.Fatalf("runReport: %v", err)
	}
	if !strings.HasPrefix(res.Formatted, "campaign.id,campaign.name") {
		t.Errorf("csv output wrong: %q", res.Formatted)
	}
}

func TestRunReport_RejectsBadQuery(t *testing.T) {
	if _, err := runReport(t.Context(), nil, ReportArgs{CustomerID: "1", Query: "DROP TABLE x"}); err == nil {
		t.Fatal("expected validation error for non-SELECT query")
	}
}

func TestRunReport_APIError(t *testing.T) {
	srv := errServer(t)
	defer srv.Close()
	if _, err := runReport(t.Context(), newTestClient(t, srv), ReportArgs{CustomerID: "1", Query: "SELECT campaign.id FROM campaign"}); err == nil {
		t.Fatal("expected API error")
	}
}
