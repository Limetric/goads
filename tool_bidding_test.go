package main

import "testing"

func TestCreatePortfolioBidding_TargetCPA(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := PortfolioBiddingArgs{CustomerID: "1", Name: "CPA", StrategyType: "TARGET_CPA", TargetCPA: 5.0}
	prev, err := runCreatePortfolioBidding(t.Context(), c, args)
	if err != nil || prev.Token == "" {
		t.Fatalf("preview: %+v err=%v", prev, err)
	}
	args.Confirm = prev.Token
	if _, err := runCreatePortfolioBidding(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "biddingStrategyOperation")
	cpa, _ := create["targetCpa"].(map[string]any)
	if cpa == nil || cpa["targetCpaMicros"] != "5000000" {
		t.Errorf("targetCpaMicros wrong: %v", create)
	}
}

func TestCreatePortfolioBidding_Validation(t *testing.T) {
	cases := []PortfolioBiddingArgs{
		{CustomerID: "1", Name: "x", StrategyType: "BOGUS"},
		{CustomerID: "1", StrategyType: "TARGET_CPA", TargetCPA: 5}, // empty name
		{CustomerID: "1", Name: "x", StrategyType: "TARGET_CPA"},    // missing cpa
		{CustomerID: "1", Name: "x", StrategyType: "TARGET_ROAS"},   // missing roas
	}
	for i, args := range cases {
		if _, err := runCreatePortfolioBidding(t.Context(), nil, args); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestCreatePortfolioBidding_ImpressionShare(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	args := PortfolioBiddingArgs{CustomerID: "1", Name: "IS", StrategyType: "TARGET_IMPRESSION_SHARE"}
	prev, _ := runCreatePortfolioBidding(t.Context(), c, args)
	args.Confirm = prev.Token
	if _, err := runCreatePortfolioBidding(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "biddingStrategyOperation")
	is, _ := create["targetImpressionShare"].(map[string]any)
	if is == nil || is["location"] != "ANYWHERE_ON_PAGE" {
		t.Errorf("impression share config wrong: %v", create)
	}
}

func TestUpdateKeywordBid_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := UpdateKeywordBidArgs{CustomerID: "1", AdGroupID: "10", CriterionID: "20", CurrentBid: 1.0, NewBid: 1.5}
	prev, err := runUpdateKeywordBid(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runUpdateKeywordBid(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	upd := opUpdate(t, cap.firstOp(t), "adGroupCriterionOperation")
	if upd["cpcBidMicros"] != "1500000" || upd["resourceName"] != "customers/1/adGroupCriteria/10~20" {
		t.Errorf("unexpected bid update: %v", upd)
	}
}

func TestUpdateKeywordBid_RejectsExcessiveIncrease(t *testing.T) {
	// Default cap is 100%; 1.0 -> 2.5 is +150% and must be rejected.
	if _, err := runUpdateKeywordBid(t.Context(), nil, UpdateKeywordBidArgs{CustomerID: "1", AdGroupID: "10", CriterionID: "20", CurrentBid: 1.0, NewBid: 2.5}); err == nil {
		t.Fatal("expected bid-increase guard to reject +150%")
	}
}
