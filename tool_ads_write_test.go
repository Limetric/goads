package main

import (
	"strings"
	"testing"
)

func headlines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "Headline"
	}
	return out
}

func descriptions(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "A description"
	}
	return out
}

func TestDraftRSA_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftRsaArgs{
		CustomerID: "123-456-7890", AdGroupID: "111",
		Headlines: headlines(3), Descriptions: descriptions(2),
		FinalURL: "https://example.com", Path1: "shop",
	}
	prev, err := runDraftResponsiveSearchAd(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.StatusAfterApply != "PAUSED" || prev.NextActionHint == nil {
		t.Errorf("expected PAUSED with hint, got %+v", prev)
	}
	if prev.NextActionHint.Params["entity_id"] != "111~<resolve ad_id from apply response>" {
		t.Errorf("hint entity_id = %v", prev.NextActionHint.Params)
	}

	args.Confirm = prev.Token
	if _, err := runDraftResponsiveSearchAd(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "adGroupAdOperation")
	if create["status"] != "PAUSED" || create["adGroup"] != "customers/1234567890/adGroups/111" {
		t.Errorf("unexpected create: %v", create)
	}
	ad, _ := create["ad"].(map[string]any)
	rsa, _ := ad["responsiveSearchAd"].(map[string]any)
	hs, _ := rsa["headlines"].([]any)
	if len(hs) != 3 || rsa["path1"] != "shop" {
		t.Errorf("unexpected rsa: %v", rsa)
	}
	urls, _ := ad["finalUrls"].([]any)
	if len(urls) != 1 || urls[0] != "https://example.com" {
		t.Errorf("finalUrls = %v", ad["finalUrls"])
	}
}

func TestDraftRSA_Validation(t *testing.T) {
	base := DraftRsaArgs{CustomerID: "1", AdGroupID: "1", FinalURL: "https://x.com"}
	cases := []struct {
		name string
		args DraftRsaArgs
	}{
		{"too few headlines", withHD(base, headlines(2), descriptions(2))},
		{"too many headlines", withHD(base, headlines(16), descriptions(2))},
		{"too few descriptions", withHD(base, headlines(3), descriptions(1))},
		{"too many descriptions", withHD(base, headlines(3), descriptions(5))},
		{"no final url", DraftRsaArgs{CustomerID: "1", AdGroupID: "1", Headlines: headlines(3), Descriptions: descriptions(2)}},
		{"long headline", withHD(base, append(headlines(2), strings.Repeat("X", 31)), descriptions(2))},
	}
	for _, tc := range cases {
		if _, err := runDraftResponsiveSearchAd(t.Context(), nil, tc.args); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}
}

func withHD(a DraftRsaArgs, h, d []string) DraftRsaArgs {
	a.Headlines = h
	a.Descriptions = d
	return a
}

func TestDraftRSA_Blocked(t *testing.T) {
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "draft_responsive_search_ad")
	args := DraftRsaArgs{CustomerID: "1", AdGroupID: "1", Headlines: headlines(3), Descriptions: descriptions(2), FinalURL: "https://x.com"}
	if _, err := runDraftResponsiveSearchAd(t.Context(), nil, args); err == nil {
		t.Fatal("expected blocked-operation error")
	}
}
