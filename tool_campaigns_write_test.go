package main

import "testing"

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
	}{
		{"MANUAL_CPC", "manualCpc"},
		{"MAXIMIZE_CLICKS", "targetSpend"},
		{"TARGET_IMPRESSION_SHARE", "targetImpressionShare"},
		{"PERCENT_CPC", "percentCpc"},
	}
	for _, tc := range cases {
		m := map[string]any{}
		applyBiddingStrategyCreate(m, tc.strategy, 0, 0)
		if _, ok := m[tc.wantKey]; !ok {
			t.Errorf("%s should set %s, got %v", tc.strategy, tc.wantKey, m)
		}
	}
}
