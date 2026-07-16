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
		{"target spend", "TARGET_SPEND", 0, 0, "targetSpend", map[string]any{}},
		{"maximize clicks maps to target spend", "MAXIMIZE_CLICKS", 0, 0, "targetSpend", map[string]any{}},
		{"percent cpc", "PERCENT_CPC", 0, 0, "percentCpc", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			campaign := map[string]any{}
			var mask []string
			if err := applyBiddingStrategyUpdate(campaign, &mask, tc.strategy, tc.cpa, tc.roas); err != nil {
				t.Fatalf("applyBiddingStrategyUpdate: %v", err)
			}
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

	t.Run("target cpa/roas without a value error at preview", func(t *testing.T) {
		// Zero targets used to stage an op with an empty updateMask that the
		// API rejected only at confirm time (issue #8).
		for _, strategy := range []string{"TARGET_CPA", "TARGET_ROAS"} {
			campaign := map[string]any{}
			var mask []string
			if err := applyBiddingStrategyUpdate(campaign, &mask, strategy, 0, 0); err == nil {
				t.Errorf("%s with zero value should error", strategy)
			}
		}
	})

	t.Run("target impression share errors with guidance", func(t *testing.T) {
		// An empty targetImpressionShare object is rejected by v23 at confirm
		// time (it requires location/fraction parameters).
		campaign := map[string]any{}
		var mask []string
		if err := applyBiddingStrategyUpdate(campaign, &mask, "TARGET_IMPRESSION_SHARE", 0, 0); err == nil || !strings.Contains(err.Error(), "create_portfolio_bidding_strategy") {
			t.Fatalf("expected guidance error, got %v", err)
		}
	})

	t.Run("unknown strategy errors instead of writing output-only field", func(t *testing.T) {
		// The old fallback wrote biddingStrategyType, which is OUTPUT_ONLY in
		// v23 — Google rejected the confirmed mutate every time (issue #8).
		campaign := map[string]any{}
		var mask []string
		if err := applyBiddingStrategyUpdate(campaign, &mask, "SUPER_BIDDING", 0, 0); err == nil {
			t.Fatal("unknown strategy should error at preview")
		}
		if _, ok := campaign["biddingStrategyType"]; ok {
			t.Fatal("output-only biddingStrategyType must never be written")
		}
	})
}

func TestUpdateCampaign_RejectsNonNumericCampaignID(t *testing.T) {
	useTempState(t)
	// campaign_id is interpolated into GAQL when resolving the budget
	// resource; a crafted ID must be rejected before any query (issue #8).
	_, err := runUpdateCampaign(t.Context(), nil, UpdateCampaignArgs{
		CustomerID: "1", CampaignID: "1 OR campaign.id > 0", DailyBudget: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("expected numeric-ID rejection, got %v", err)
	}
}

func TestUpdateCampaign_HonorsBlockedOps(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "update_campaign")
	if _, err := runUpdateCampaign(t.Context(), nil, UpdateCampaignArgs{CustomerID: "1", CampaignID: "5", DailyBudget: 5}); err == nil {
		t.Fatal("blocked operation should be rejected")
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
