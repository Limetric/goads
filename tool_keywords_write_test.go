package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDraftKeywords_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftKeywordsArgs{
		CustomerID: "123-456-7890", AdGroupID: "111",
		Keywords: []KeywordWithMatchType{{Text: "running shoes", MatchType: "EXACT"}, {Text: "trainers", MatchType: "PHRASE"}},
	}
	prev, err := runDraftKeywords(t.Context(), c, args)
	if err != nil || cap.calls != 0 {
		t.Fatalf("preview: %+v err=%v calls=%d", prev, err, cap.calls)
	}
	args.Confirm = prev.Token
	if _, err := runDraftKeywords(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cap.lastOps()) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(cap.lastOps()))
	}
	create := opCreate(t, cap.firstOp(t), "adGroupCriterionOperation")
	kw, _ := create["keyword"].(map[string]any)
	if kw["text"] != "running shoes" || kw["matchType"] != "EXACT" {
		t.Errorf("unexpected keyword op: %v", kw)
	}
	if create["adGroup"] != "customers/1234567890/adGroups/111" {
		t.Errorf("adGroup = %v", create["adGroup"])
	}
}

func TestDraftKeywords_Validation(t *testing.T) {
	if _, err := runDraftKeywords(t.Context(), nil, DraftKeywordsArgs{CustomerID: "1", AdGroupID: "1"}); err == nil {
		t.Error("empty keywords should error")
	}
	if _, err := runDraftKeywords(t.Context(), nil, DraftKeywordsArgs{CustomerID: "1", AdGroupID: "1", Keywords: []KeywordWithMatchType{{Text: "x", MatchType: "FUZZY"}}}); err == nil {
		t.Error("invalid match type should error")
	}
}

func TestAddNegativeKeywords_SetsNegative(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := AddNegativeKeywordsArgs{CustomerID: "1", CampaignID: "5", Keywords: []string{"free"}, MatchType: "BROAD"}
	prev, _ := runAddNegativeKeywords(t.Context(), c, args)
	args.Confirm = prev.Token
	if _, err := runAddNegativeKeywords(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "campaignCriterionOperation")
	if create["negative"] != true {
		t.Errorf("expected negative=true, got %v", create)
	}
}

func TestRemoveKeywords_UsesRemoveOp(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := RemoveKeywordsArgs{CustomerID: "1", AdGroupID: "10", CriterionIDs: []string{"20", "21"}}
	prev, err := runRemoveKeywords(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	// Destructive: first confirm re-stages (issue #12), second applies.
	args.Confirm = prev.Token
	second, err := runRemoveKeywords(t.Context(), c, args)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	args.Confirm = second.Token
	if _, err := runRemoveKeywords(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	op := cap.firstOp(t)
	inner, _ := op["adGroupCriterionOperation"].(map[string]any)
	if inner["remove"] != "customers/1/adGroupCriteria/10~20" {
		t.Errorf("unexpected remove op: %v", inner)
	}
}

func TestRemoveNegativeKeywords_UsesCampaignCriteria(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := RemoveNegativeKeywordsArgs{CustomerID: "1", CampaignID: "5", CriterionIDs: []string{"99"}}
	prev, _ := runRemoveNegativeKeywords(t.Context(), c, args)
	// Destructive: first confirm re-stages (issue #12), second applies.
	args.Confirm = prev.Token
	second, err := runRemoveNegativeKeywords(t.Context(), c, args)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	args.Confirm = second.Token
	if _, err := runRemoveNegativeKeywords(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	inner, _ := cap.firstOp(t)["campaignCriterionOperation"].(map[string]any)
	if inner["remove"] != "customers/1/campaignCriteria/5~99" {
		t.Errorf("unexpected remove op: %v", inner)
	}
}

func TestKeywordRemoves_RequireIDs(t *testing.T) {
	if _, err := runRemoveKeywords(t.Context(), nil, RemoveKeywordsArgs{CustomerID: "1", AdGroupID: "1"}); err == nil {
		t.Error("remove keywords should require criterion IDs")
	}
	if _, err := runRemoveNegativeKeywords(t.Context(), nil, RemoveNegativeKeywordsArgs{CustomerID: "1", CampaignID: "1"}); err == nil {
		t.Error("remove negative keywords should require criterion IDs")
	}
}

func TestParseKeywordFlag(t *testing.T) {
	if kw := parseKeywordFlag("shoes|exact"); kw.Text != "shoes" || kw.MatchType != "EXACT" {
		t.Errorf("parse with type wrong: %+v", kw)
	}
	if kw := parseKeywordFlag("running shoes"); kw.MatchType != "BROAD" {
		t.Errorf("default match type should be BROAD: %+v", kw)
	}
}

func TestDraftKeywords_BlocksBroadIntoManualCPCCampaign(t *testing.T) {
	useTempState(t)
	// The campaign's strategy is looked up from the ad group; BROAD keywords
	// into a MANUAL_CPC campaign must be blocked (issue #12).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "googleAds:search") {
			_, _ = w.Write([]byte(`{"results":[{"campaign":{"biddingStrategyType":"MANUAL_CPC"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	_, err := runDraftKeywords(t.Context(), c, DraftKeywordsArgs{
		CustomerID: "1", AdGroupID: "10",
		Keywords: []KeywordWithMatchType{{Text: "shoes", MatchType: "BROAD"}},
	})
	if err == nil || !strings.Contains(err.Error(), "BROAD") {
		t.Fatalf("expected the BROAD+MANUAL_CPC guard to block, got %v", err)
	}

	// EXACT keywords into the same campaign are fine.
	if _, err := runDraftKeywords(t.Context(), c, DraftKeywordsArgs{
		CustomerID: "1", AdGroupID: "10",
		Keywords: []KeywordWithMatchType{{Text: "shoes", MatchType: "EXACT"}},
	}); err != nil {
		t.Fatalf("EXACT keywords should pass: %v", err)
	}
}
