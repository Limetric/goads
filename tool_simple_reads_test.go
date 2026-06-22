package main

import "testing"

func TestRunConversions(t *testing.T) {
	srv := gaqlServer(t, `[{"conversionAction":{"id":"1","name":"Purchase"}}]`,
		"FROM conversion_action", "conversion_action.status != 'REMOVED'", "LIMIT 200")
	defer srv.Close()
	res, err := runConversions(t.Context(), newTestClient(t, srv), ConversionsArgs{CustomerID: "1"})
	if err != nil || res.TotalCount != 1 {
		t.Fatalf("runConversions: res=%+v err=%v", res, err)
	}
}

func TestRunPolicy(t *testing.T) {
	srv := gaqlServer(t, `[{"adGroupAd":{"ad":{"id":"9"}}}]`,
		"FROM ad_group_ad", "approval_status != 'APPROVED'", "LIMIT 200")
	defer srv.Close()
	res, err := runPolicy(t.Context(), newTestClient(t, srv), PolicyArgs{CustomerID: "1"})
	if err != nil || res.TotalCount != 1 {
		t.Fatalf("runPolicy: res=%+v err=%v", res, err)
	}
}

func TestRunExtensions(t *testing.T) {
	srv := gaqlServer(t, `[{"campaignAsset":{"fieldType":"SITELINK"}}]`,
		"FROM campaign_asset", "campaign_asset.status != 'REMOVED'", "LIMIT 500")
	defer srv.Close()
	res, err := runExtensions(t.Context(), newTestClient(t, srv), ExtensionsArgs{CustomerID: "1"})
	if err != nil || res.TotalCount != 1 {
		t.Fatalf("runExtensions: res=%+v err=%v", res, err)
	}
}

func TestSimpleReads_RequireCustomerID(t *testing.T) {
	if _, err := runConversions(t.Context(), nil, ConversionsArgs{}); err == nil {
		t.Error("conversions should require customer_id")
	}
	if _, err := runPolicy(t.Context(), nil, PolicyArgs{}); err == nil {
		t.Error("policy should require customer_id")
	}
	if _, err := runExtensions(t.Context(), nil, ExtensionsArgs{}); err == nil {
		t.Error("extensions should require customer_id")
	}
}

func TestSimpleReads_APIError(t *testing.T) {
	srv := errServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	if _, err := runConversions(t.Context(), c, ConversionsArgs{CustomerID: "1"}); err == nil {
		t.Error("conversions expected API error")
	}
	if _, err := runPolicy(t.Context(), c, PolicyArgs{CustomerID: "1"}); err == nil {
		t.Error("policy expected API error")
	}
	if _, err := runExtensions(t.Context(), c, ExtensionsArgs{CustomerID: "1"}); err == nil {
		t.Error("extensions expected API error")
	}
}
