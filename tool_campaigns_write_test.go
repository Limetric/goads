package main

import (
	"strings"
	"testing"
)

func TestDraftCampaign_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftCampaignArgs{
		CustomerID: "123-456-7890", CampaignName: "Spring Sale", DailyBudget: 30,
		BiddingStrategy: "MAXIMIZE_CONVERSIONS", TargetCPA: 5, ChannelType: "SEARCH", AdGroupName: "AG1",
		Keywords:     []KeywordWithMatchType{{Text: "shoes", MatchType: "EXACT"}},
		GeoTargetIDs: []string{"2840"}, LanguageIDs: []string{"1000"},
	}
	prev, err := runDraftCampaign(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.StatusAfterApply != "PAUSED" || prev.NextActionHint == nil {
		t.Errorf("expected PAUSED with hint, got %+v", prev)
	}

	args.Confirm = prev.Token
	if _, err := runDraftCampaign(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// budget + campaign + geo + language + ad group + keyword = 6
	ops := cap.lastOps()
	if len(ops) != 6 {
		t.Fatalf("expected 6 ops, got %d", len(ops))
	}
	camp := opCreate(t, ops[1].(map[string]any), "campaignOperation")
	if camp["advertisingChannelType"] != "SEARCH" || camp["status"] != "PAUSED" {
		t.Errorf("campaign op wrong: %v", camp)
	}
	if camp["containsEuPoliticalAdvertising"] != "DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING" {
		t.Errorf("missing EU political advertising default: %v", camp)
	}
	mc, _ := camp["maximizeConversions"].(map[string]any)
	if mc == nil || mc["targetCpaMicros"] != "5000000" {
		t.Errorf("bidding strategy wrong: %v", camp)
	}
	ag := opCreate(t, ops[4].(map[string]any), "adGroupOperation")
	if ag["type"] != "SEARCH_STANDARD" || ag["status"] != "PAUSED" {
		t.Errorf("ad group op wrong: %v", ag)
	}
}

func TestDraftCampaign_BudgetCap(t *testing.T) {
	// Default cap is 50; 100/day must be rejected before any op is built.
	if _, err := runDraftCampaign(t.Context(), nil, DraftCampaignArgs{CustomerID: "1", CampaignName: "x", DailyBudget: 100, AdGroupName: "ag"}); err == nil {
		t.Fatal("expected budget cap rejection")
	}
}

func TestDraftCampaign_InvalidKeywordMatchType(t *testing.T) {
	args := DraftCampaignArgs{CustomerID: "1", CampaignName: "x", DailyBudget: 10, AdGroupName: "ag",
		Keywords: []KeywordWithMatchType{{Text: "k", MatchType: "FUZZY"}}}
	if _, err := runDraftCampaign(t.Context(), nil, args); err == nil {
		t.Fatal("expected invalid match type error")
	}
}

func TestDraftCampaign_DisplayChannelAdGroupType(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftCampaignArgs{CustomerID: "1", CampaignName: "x", DailyBudget: 10, ChannelType: "DISPLAY", AdGroupName: "ag", BiddingStrategy: "MANUAL_CPC"}
	prev, _ := runDraftCampaign(t.Context(), c, args)
	args.Confirm = prev.Token
	if _, err := runDraftCampaign(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	ops := cap.lastOps()
	ag := opCreate(t, ops[len(ops)-1].(map[string]any), "adGroupOperation")
	if ag["type"] != "DISPLAY_STANDARD" {
		t.Errorf("expected DISPLAY_STANDARD, got %v", ag["type"])
	}
}

func TestApplyBiddingStrategyCreate(t *testing.T) {
	cases := []struct {
		strategy, wantKey string
		cpa, roas         float64
		wantSub           map[string]any
	}{
		{"MANUAL_CPC", "manualCpc", 0, 0, map[string]any{}},
		{"MAXIMIZE_CLICKS", "targetSpend", 0, 0, map[string]any{}},
		{"TARGET_SPEND", "targetSpend", 0, 0, map[string]any{}},
		{"TARGET_IMPRESSION_SHARE", "targetImpressionShare", 0, 0, map[string]any{}},
		{"PERCENT_CPC", "percentCpc", 0, 0, map[string]any{}},
		{"MAXIMIZE_CONVERSIONS", "maximizeConversions", 5, 0, map[string]any{"targetCpaMicros": "5000000"}},
		{"MAXIMIZE_CONVERSIONS", "maximizeConversions", 0, 0, map[string]any{}},
		{"MAXIMIZE_CONVERSION_VALUE", "maximizeConversionValue", 0, 4, map[string]any{"targetRoas": 4.0}},
		{"MAXIMIZE_CONVERSION_VALUE", "maximizeConversionValue", 0, 0, map[string]any{}},
		{"TARGET_CPA", "targetCpa", 2.5, 0, map[string]any{"targetCpaMicros": "2500000"}},
		{"TARGET_ROAS", "targetRoas", 0, 3, map[string]any{"targetRoas": 3.0}},
	}
	for _, tc := range cases {
		m := map[string]any{}
		applyBiddingStrategyCreate(m, tc.strategy, tc.cpa, tc.roas)
		sub, ok := m[tc.wantKey].(map[string]any)
		if !ok {
			t.Errorf("%s (cpa=%v roas=%v) should set %s, got %v", tc.strategy, tc.cpa, tc.roas, tc.wantKey, m)
			continue
		}
		if len(sub) != len(tc.wantSub) {
			t.Errorf("%s sub-field = %v, want %v", tc.strategy, sub, tc.wantSub)
			continue
		}
		for k, want := range tc.wantSub {
			if sub[k] != want {
				t.Errorf("%s %s[%s] = %v, want %v", tc.strategy, tc.wantKey, k, sub[k], want)
			}
		}
	}

	t.Run("target strategies without a value leave bidding unset", func(t *testing.T) {
		for _, strategy := range []string{"TARGET_CPA", "TARGET_ROAS"} {
			m := map[string]any{}
			applyBiddingStrategyCreate(m, strategy, 0, 0)
			if len(m) != 0 {
				t.Errorf("%s with zero value should leave bidding unset, got %v", strategy, m)
			}
		}
	})

	t.Run("unknown strategy leaves bidding unset", func(t *testing.T) {
		m := map[string]any{}
		applyBiddingStrategyCreate(m, "NOT_A_STRATEGY", 1, 1)
		if len(m) != 0 {
			t.Errorf("unknown strategy should leave bidding unset, got %v", m)
		}
	})
}

func TestDraftCampaign_BlocksBroadManualCPC(t *testing.T) {
	useTempState(t)
	// The BROAD+MANUAL_CPC guard existed but was never wired in (issue #12).
	_, err := runDraftCampaign(t.Context(), nil, DraftCampaignArgs{
		CustomerID: "1", CampaignName: "C", DailyBudget: 5,
		BiddingStrategy: "MANUAL_CPC", AdGroupName: "AG",
		Keywords: []KeywordWithMatchType{{Text: "shoes", MatchType: "BROAD"}},
	})
	if err == nil || !strings.Contains(err.Error(), "BROAD") {
		t.Fatalf("expected the BROAD+MANUAL_CPC guard to block, got %v", err)
	}
}
