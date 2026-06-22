package main

import "testing"

func validPmaxArgs() CreatePmaxArgs {
	return CreatePmaxArgs{
		CustomerID: "123-456-7890", CampaignName: "PMax1", DailyBudget: 40, BiddingStrategy: "MAXIMIZE_CONVERSIONS",
		FinalURLs: []string{"https://example.com"},
		Headlines: headlines(3), LongHeadlines: descriptions(1), Descriptions: descriptions(2),
		BusinessName: "Acme", GeoTargetIDs: []string{"2840"},
	}
}

func TestCreatePmax_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := validPmaxArgs()
	prev, err := runCreatePmaxCampaign(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// Defaults to paused -> carries a next-action hint.
	if prev.StatusAfterApply != "PAUSED" || prev.NextActionHint == nil {
		t.Errorf("expected PAUSED with hint, got %+v", prev)
	}

	args.Confirm = prev.Token
	if _, err := runCreatePmaxCampaign(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	ops := cap.lastOps()
	// budget + campaign + 1 geo + asset group + (3 headlines + 1 long + 2 desc + 1 biz)*2 = 18
	if len(ops) != 18 {
		t.Fatalf("expected 18 ops, got %d", len(ops))
	}
	budget := opCreate(t, ops[0].(map[string]any), "campaignBudgetOperation")
	if budget["amountMicros"] != "40000000" || budget["resourceName"] != "customers/1234567890/campaignBudgets/-1" {
		t.Errorf("budget op wrong: %v", budget)
	}
	camp := opCreate(t, ops[1].(map[string]any), "campaignOperation")
	if camp["advertisingChannelType"] != "PERFORMANCE_MAX" || camp["status"] != "PAUSED" {
		t.Errorf("campaign op wrong: %v", camp)
	}
	if _, ok := camp["maximizeConversions"]; !ok {
		t.Errorf("expected maximizeConversions strategy: %v", camp)
	}
}

func TestCreatePmax_StartEnabled(t *testing.T) {
	useTempState(t)
	srv, _ := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := validPmaxArgs()
	enabled := false
	args.StartPaused = &enabled
	prev, err := runCreatePmaxCampaign(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.StatusAfterApply != "ENABLED" || prev.NextActionHint != nil {
		t.Errorf("ENABLED pmax should carry no hint, got %+v", prev)
	}
}

func TestCreatePmax_Validation(t *testing.T) {
	mut := func(f func(*CreatePmaxArgs)) CreatePmaxArgs {
		a := validPmaxArgs()
		f(&a)
		return a
	}
	cases := map[string]CreatePmaxArgs{
		"too few headlines":      mut(func(a *CreatePmaxArgs) { a.Headlines = headlines(2) }),
		"too many long":          mut(func(a *CreatePmaxArgs) { a.LongHeadlines = descriptions(6) }),
		"too few descriptions":   mut(func(a *CreatePmaxArgs) { a.Descriptions = descriptions(1) }),
		"no final url":           mut(func(a *CreatePmaxArgs) { a.FinalURLs = nil }),
		"business name too long": mut(func(a *CreatePmaxArgs) { a.BusinessName = "ThisBusinessNameIsWayTooLongToFit" }),
		"budget over cap":        mut(func(a *CreatePmaxArgs) { a.DailyBudget = 100 }), // default cap 50
	}
	for name, args := range cases {
		if _, err := runCreatePmaxCampaign(t.Context(), nil, args); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestCreatePmax_Blocked(t *testing.T) {
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "create_pmax_campaign")
	if _, err := runCreatePmaxCampaign(t.Context(), nil, validPmaxArgs()); err == nil {
		t.Fatal("expected blocked-operation error")
	}
}
