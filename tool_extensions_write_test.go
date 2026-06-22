package main

import (
	"strings"
	"testing"
)

func TestDraftSitelinks_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := DraftSitelinksArgs{
		CustomerID: "123-456-7890", CampaignID: "555",
		Sitelinks: []SitelinkInput{
			{LinkText: "Shop", FinalURL: "https://x.com/shop", Description1: "Great deals", Description2: "Today only"},
			{LinkText: "About", FinalURL: "https://x.com/about", Description1: "Our story", Description2: "Since 2020"},
		},
	}
	prev, err := runDraftSitelinks(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runDraftSitelinks(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// 2 sitelinks -> 4 ops (asset create + campaign link each).
	ops := cap.lastOps()
	if len(ops) != 4 {
		t.Fatalf("expected 4 ops, got %d", len(ops))
	}
	asset := opCreate(t, cap.firstOp(t), "assetOperation")
	if asset["resourceName"] != "customers/1234567890/assets/-100" {
		t.Errorf("temp asset resource = %v", asset["resourceName"])
	}
	link := opCreate(t, ops[1].(map[string]any), "campaignAssetOperation")
	if link["fieldType"] != "SITELINK" || link["asset"] != "customers/1234567890/assets/-100" {
		t.Errorf("link op wrong: %v", link)
	}
}

func TestDraftSitelinks_Validation(t *testing.T) {
	if _, err := runDraftSitelinks(t.Context(), nil, DraftSitelinksArgs{CustomerID: "1", CampaignID: "5"}); err == nil {
		t.Error("empty sitelinks should error")
	}
	long := SitelinkInput{LinkText: strings.Repeat("X", 26), FinalURL: "u", Description1: "a", Description2: "b"}
	if _, err := runDraftSitelinks(t.Context(), nil, DraftSitelinksArgs{CustomerID: "1", CampaignID: "5", Sitelinks: []SitelinkInput{long}}); err == nil {
		t.Error("over-long link text should error")
	}
}

func TestCreateCallouts(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := CreateCalloutsArgs{CustomerID: "1", CampaignID: "5", Callouts: []string{"Free shipping"}}
	prev, _ := runCreateCallouts(t.Context(), c, args)
	args.Confirm = prev.Token
	if _, err := runCreateCallouts(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	asset := opCreate(t, cap.firstOp(t), "assetOperation")
	co, _ := asset["calloutAsset"].(map[string]any)
	if co["calloutText"] != "Free shipping" {
		t.Errorf("callout asset wrong: %v", asset)
	}
	if asset["resourceName"] != "customers/1/assets/-200" {
		t.Errorf("temp id should start at -200: %v", asset["resourceName"])
	}
}

func TestCreateStructuredSnippets(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := CreateSnippetsArgs{CustomerID: "1", CampaignID: "5", Header: "Brands", Values: []string{"Nike", "Adidas"}}
	prev, err := runCreateStructuredSnippets(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runCreateStructuredSnippets(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	asset := opCreate(t, cap.firstOp(t), "assetOperation")
	snip, _ := asset["structuredSnippetAsset"].(map[string]any)
	if snip["header"] != "Brands" {
		t.Errorf("snippet header wrong: %v", snip)
	}
}

func TestCreateStructuredSnippets_InvalidHeader(t *testing.T) {
	if _, err := runCreateStructuredSnippets(t.Context(), nil, CreateSnippetsArgs{CustomerID: "1", CampaignID: "5", Header: "Bogus", Values: []string{"x"}}); err == nil {
		t.Fatal("expected error for invalid header")
	}
}

func TestRemoveExtension(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := RemoveExtensionArgs{CustomerID: "1", CampaignID: "555", AssetID: "999", FieldType: "SITELINK"}
	prev, err := runRemoveExtension(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runRemoveExtension(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	inner, _ := cap.firstOp(t)["campaignAssetOperation"].(map[string]any)
	if inner["remove"] != "customers/1/campaignAssets/555~999~SITELINK" {
		t.Errorf("unexpected remove op: %v", inner)
	}
}
