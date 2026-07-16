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

func TestApplyBiddingStrategyUpdate(t *testing.T) {
	cases := []struct {
		name      string
		strategy  string
		cpa, roas float64
		wantKey   string
		wantSub   map[string]any
	}{
		{"maximize conversions with cpa", "MAXIMIZE_CONVERSIONS", 5, 0, "maximizeConversions", map[string]any{"targetCpaMicros": "5000000"}},
		{"maximize conversions without cpa", "MAXIMIZE_CONVERSIONS", 0, 0, "maximizeConversions", map[string]any{}},
		{"maximize conversion value with roas", "MAXIMIZE_CONVERSION_VALUE", 0, 3.5, "maximizeConversionValue", map[string]any{"targetRoas": 3.5}},
		{"maximize conversion value without roas", "MAXIMIZE_CONVERSION_VALUE", 0, 0, "maximizeConversionValue", map[string]any{}},
		{"target cpa", "TARGET_CPA", 10, 0, "targetCpa", map[string]any{"targetCpaMicros": "10000000"}},
		{"target roas", "TARGET_ROAS", 0, 2, "targetRoas", map[string]any{"targetRoas": 2.0}},
		{"manual cpc", "MANUAL_CPC", 0, 0, "manualCpc", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaign := map[string]any{}
			var mask []string
			applyBiddingStrategyUpdate(campaign, &mask, tc.strategy, tc.cpa, tc.roas)
			if len(mask) != 1 || mask[0] != tc.wantKey {
				t.Fatalf("mask = %v, want [%s]", mask, tc.wantKey)
			}
			sub, _ := campaign[tc.wantKey].(map[string]any)
			if sub == nil {
				t.Fatalf("campaign[%s] missing: %v", tc.wantKey, campaign)
			}
			if len(sub) != len(tc.wantSub) {
				t.Fatalf("campaign[%s] = %v, want %v", tc.wantKey, sub, tc.wantSub)
			}
			for k, want := range tc.wantSub {
				if sub[k] != want {
					t.Errorf("campaign[%s][%s] = %v, want %v", tc.wantKey, k, sub[k], want)
				}
			}
		})
	}

	t.Run("target cpa/roas without a value change nothing", func(t *testing.T) {
		for _, strategy := range []string{"TARGET_CPA", "TARGET_ROAS"} {
			campaign := map[string]any{}
			var mask []string
			applyBiddingStrategyUpdate(campaign, &mask, strategy, 0, 0)
			if len(campaign) != 0 || len(mask) != 0 {
				t.Errorf("%s with zero value should be a no-op, got campaign=%v mask=%v", strategy, campaign, mask)
			}
		}
	})

	t.Run("unknown strategy falls back to biddingStrategyType", func(t *testing.T) {
		campaign := map[string]any{}
		var mask []string
		applyBiddingStrategyUpdate(campaign, &mask, "TARGET_SPEND", 0, 0)
		if campaign["biddingStrategyType"] != "TARGET_SPEND" {
			t.Errorf("expected biddingStrategyType fallback, got %v", campaign)
		}
		if len(mask) != 1 || mask[0] != "biddingStrategyType" {
			t.Errorf("mask = %v, want [biddingStrategyType]", mask)
		}
	})
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
