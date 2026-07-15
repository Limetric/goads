package main

import "testing"

func TestCreateAdGroup_DefaultsPausedWithHint(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := CreateAdGroupArgs{CustomerID: "123-456-7890", CampaignID: "111", Name: "AG1", CpcBidMicros: 2_000_000}
	prev, err := runCreateAdGroup(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.StatusAfterApply != "PAUSED" || prev.NextActionHint == nil {
		t.Errorf("expected PAUSED with hint, got %+v", prev)
	}
	if prev.NextActionHint.Params["entity_type"] != "ad_group" {
		t.Errorf("hint entity_type = %v", prev.NextActionHint.Params)
	}

	args.Confirm = prev.Token
	if _, err := runCreateAdGroup(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "adGroupOperation")
	if create["status"] != "PAUSED" || create["type"] != "SEARCH_STANDARD" {
		t.Errorf("unexpected create: %v", create)
	}
	if create["cpcBidMicros"] != "2000000" {
		t.Errorf("cpcBidMicros = %v", create["cpcBidMicros"])
	}
	if create["campaign"] != "customers/1234567890/campaigns/111" {
		t.Errorf("campaign = %v", create["campaign"])
	}
}

func TestCreateAdGroup_ExplicitEnabledNoHint(t *testing.T) {
	useTempState(t)
	prev, err := runCreateAdGroup(t.Context(), nil, CreateAdGroupArgs{CustomerID: "1", CampaignID: "1", Name: "AG", Status: "ENABLED"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.StatusAfterApply != "ENABLED" || prev.NextActionHint != nil {
		t.Errorf("ENABLED should carry no hint, got %+v", prev)
	}
}

func TestCreateAdGroup_CanOmitTypeForAppCampaign(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := CreateAdGroupArgs{CustomerID: "1", CampaignID: "2", Name: "App creatives", OmitType: true}
	preview, err := runCreateAdGroup(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = preview.Token
	if _, err := runCreateAdGroup(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "adGroupOperation")
	if _, exists := create["type"]; exists {
		t.Fatalf("App ad group must omit type: %v", create)
	}
}

func TestCreateAdGroup_EmptyName(t *testing.T) {
	if _, err := runCreateAdGroup(t.Context(), nil, CreateAdGroupArgs{CustomerID: "1", CampaignID: "1"}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestUpdateAdGroup_BuildsMask(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := UpdateAdGroupArgs{CustomerID: "1", AdGroupID: "9", Name: "New", CpcBidMicros: 500000, AdRotationMode: "ROTATE_FOREVER"}
	prev, err := runUpdateAdGroup(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runUpdateAdGroup(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	op, _ := cap.firstOp(t)["adGroupOperation"].(map[string]any)
	if op["updateMask"] != "name,cpcBidMicros,adRotationMode" {
		t.Errorf("updateMask = %v", op["updateMask"])
	}
	upd, _ := op["update"].(map[string]any)
	if upd["adRotationMode"] != "ROTATE_FOREVER" {
		t.Errorf("adRotationMode = %v", upd["adRotationMode"])
	}
}

func TestUpdateAdGroup_RequiresAField(t *testing.T) {
	if _, err := runUpdateAdGroup(t.Context(), nil, UpdateAdGroupArgs{CustomerID: "1", AdGroupID: "9"}); err == nil {
		t.Fatal("expected error when no fields provided")
	}
}

func TestUpdateAdGroup_InvalidRotationMode(t *testing.T) {
	if _, err := runUpdateAdGroup(t.Context(), nil, UpdateAdGroupArgs{CustomerID: "1", AdGroupID: "9", AdRotationMode: "NOPE"}); err == nil {
		t.Fatal("expected error for invalid rotation mode")
	}
}
