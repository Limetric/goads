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

func TestDraftAppAd_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftAppAdArgs{
		CustomerID: "123-456-7890",
		AdGroupID:  "111",
		Headlines:  []string{"Chat met nieuwe mensen", "Start direct een gesprek"},
		Descriptions: []string{
			"Ontmoet nieuwe mensen in Nederland en Vlaanderen.",
		},
		ImageAssets:        []string{"customers/1234567890/assets/10"},
		YouTubeVideoAssets: []string{"customers/1234567890/assets/20"},
		Status:             "ENABLED",
	}
	preview, err := runDraftAppAd(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Applied || preview.Token == "" || preview.StatusAfterApply != "ENABLED" || preview.NextActionHint != nil {
		t.Fatalf("bad preview: %+v", preview)
	}

	args.Confirm = preview.Token
	if _, err := runDraftAppAd(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "adGroupAdOperation")
	if create["status"] != "ENABLED" || create["adGroup"] != "customers/1234567890/adGroups/111" {
		t.Fatalf("unexpected create: %v", create)
	}
	ad := create["ad"].(map[string]any)
	appAd := ad["appAd"].(map[string]any)
	images := appAd["images"].([]any)
	videos := appAd["youtubeVideos"].([]any)
	if images[0].(map[string]any)["asset"] != "customers/1234567890/assets/10" {
		t.Errorf("images = %v", images)
	}
	if videos[0].(map[string]any)["asset"] != "customers/1234567890/assets/20" {
		t.Errorf("videos = %v", videos)
	}
}

func TestDraftAppAd_Validation(t *testing.T) {
	base := DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Headlines: []string{"Headline"}, Descriptions: []string{"Description"}}
	cases := []struct {
		name string
		args DraftAppAdArgs
	}{
		{"missing ad group", DraftAppAdArgs{CustomerID: "1", Headlines: base.Headlines, Descriptions: base.Descriptions}},
		{"no headlines", DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Descriptions: base.Descriptions}},
		{"too many headlines", DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Headlines: headlines(6), Descriptions: base.Descriptions}},
		{"no descriptions", DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Headlines: base.Headlines}},
		{"too many descriptions", DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Headlines: base.Headlines, Descriptions: descriptions(6)}},
		{"empty image asset", DraftAppAdArgs{CustomerID: "1", AdGroupID: "1", Headlines: base.Headlines, Descriptions: base.Descriptions, ImageAssets: []string{""}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := runDraftAppAd(t.Context(), nil, testCase.args); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
