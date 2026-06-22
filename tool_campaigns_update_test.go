package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateCampaign_BudgetResolvesResource(t *testing.T) {
	useTempState(t)
	var mutateBody map[string]any
	var mutateCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "googleAds:search"):
			// Budget-resource resolution query.
			_, _ = w.Write([]byte(`{"results":[{"campaign":{"campaignBudget":"customers/1/campaignBudgets/777"}}]}`))
		case strings.HasSuffix(r.URL.Path, "googleAds:mutate"):
			mutateCalls++
			_ = decodeJSONBody(r, &mutateBody)
			_, _ = w.Write([]byte(`{"results":[{}]}`))
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	args := UpdateCampaignArgs{CustomerID: "1", CampaignID: "555", DailyBudget: 25}
	prev, err := runUpdateCampaign(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if mutateCalls != 0 {
		t.Fatal("preview must not call mutate")
	}
	args.Confirm = prev.Token
	if _, err := runUpdateCampaign(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	ops, _ := mutateBody["mutateOperations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %v", mutateBody)
	}
	op, _ := ops[0].(map[string]any)
	bud, _ := op["campaignBudgetOperation"].(map[string]any)
	upd, _ := bud["update"].(map[string]any)
	if upd["resourceName"] != "customers/1/campaignBudgets/777" || upd["amountMicros"] != "25000000" {
		t.Errorf("budget update wrong: %v", upd)
	}
}

func TestUpdateCampaign_BiddingAndTargeting(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := UpdateCampaignArgs{CustomerID: "1", CampaignID: "5", BiddingStrategy: "TARGET_CPA", TargetCPA: 10, GeoTargetIDs: []string{"2840"}, LanguageIDs: []string{"1000"}}
	prev, err := runUpdateCampaign(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runUpdateCampaign(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// campaign update + geo + language = 3
	if len(cap.lastOps()) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(cap.lastOps()))
	}
	op, _ := cap.firstOp(t)["campaignOperation"].(map[string]any)
	if op["updateMask"] != "targetCpa" {
		t.Errorf("updateMask = %v", op["updateMask"])
	}
}

func TestUpdateCampaign_NoChanges(t *testing.T) {
	useTempState(t)
	srv, _ := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := runUpdateCampaign(t.Context(), c, UpdateCampaignArgs{CustomerID: "1", CampaignID: "5"}); err == nil {
		t.Fatal("expected error when no changes specified")
	}
}

func TestUpdateCampaign_BudgetCap(t *testing.T) {
	useTempState(t)
	srv, _ := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := runUpdateCampaign(t.Context(), c, UpdateCampaignArgs{CustomerID: "1", CampaignID: "5", DailyBudget: 100}); err == nil {
		t.Fatal("expected budget cap rejection")
	}
}
