package main

import "testing"

func TestCreateCustomAudience_Preview(t *testing.T) {
	useTempState(t)
	// customAudienceOperation is not an allowed mutate key, so we only exercise
	// the preview path here (mirroring upstream's test coverage).
	prev, err := runCreateCustomAudience(t.Context(), nil, CreateAudienceArgs{
		CustomerID: "123-456-7890", AudienceName: "My Audience", AudienceType: "WEBSITE_VISITORS",
		URLsOrRules: []string{"example.com/products"},
	})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.Applied || prev.Token == "" {
		t.Errorf("expected preview with token, got %+v", prev)
	}
}

func TestCreateCustomAudience_ApplyGatedByAllowList(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	prev, _ := runCreateCustomAudience(t.Context(), c, CreateAudienceArgs{
		CustomerID: "1", AudienceName: "A", AudienceType: "WEBSITE_VISITORS", URLsOrRules: []string{"x"},
	})
	// Applying must be rejected client-side (customAudienceOperation is not a
	// valid googleAds:mutate key) and never reach the server.
	if _, err := runCreateCustomAudience(t.Context(), c, CreateAudienceArgs{
		CustomerID: "1", AudienceName: "A", AudienceType: "WEBSITE_VISITORS", URLsOrRules: []string{"x"}, Confirm: prev.Token,
	}); err == nil {
		t.Fatal("expected allow-list to reject customAudienceOperation on apply")
	}
	if cap.calls != 0 {
		t.Errorf("server should not be called for a disallowed op")
	}
}

func TestCreateCustomAudience_Validation(t *testing.T) {
	cases := []CreateAudienceArgs{
		{CustomerID: "1", AudienceName: "n", AudienceType: "BOGUS", URLsOrRules: []string{"r"}},
		{CustomerID: "1", AudienceType: "WEBSITE_VISITORS", URLsOrRules: []string{"r"}}, // empty name
		{CustomerID: "1", AudienceName: "n", AudienceType: "WEBSITE_VISITORS"},          // no rules
	}
	for i, args := range cases {
		if _, err := runCreateCustomAudience(t.Context(), nil, args); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestAddAudienceTargeting_Observation(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := AddAudienceTargetingArgs{CustomerID: "1", CampaignID: "5", AudienceID: "9", TargetingMode: "OBSERVATION"}
	prev, err := runAddAudienceTargeting(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	if _, err := runAddAudienceTargeting(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "campaignCriterionOperation")
	if _, ok := create["bidModifier"]; !ok {
		t.Errorf("OBSERVATION mode should set bidModifier: %v", create)
	}
	ul, _ := create["userList"].(map[string]any)
	if ul == nil || ul["userList"] != "customers/1/userLists/9" {
		t.Errorf("userList resource wrong: %v", create)
	}
}

func TestAddAudienceTargeting_TargetingNoBidModifier(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	args := AddAudienceTargetingArgs{CustomerID: "1", CampaignID: "5", AudienceID: "9", TargetingMode: "TARGETING"}
	prev, _ := runAddAudienceTargeting(t.Context(), c, args)
	args.Confirm = prev.Token
	if _, err := runAddAudienceTargeting(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	create := opCreate(t, cap.firstOp(t), "campaignCriterionOperation")
	if _, ok := create["bidModifier"]; ok {
		t.Errorf("TARGETING mode should not set bidModifier: %v", create)
	}
}

func TestAddAudienceTargeting_InvalidMode(t *testing.T) {
	if _, err := runAddAudienceTargeting(t.Context(), nil, AddAudienceTargetingArgs{CustomerID: "1", CampaignID: "5", AudienceID: "9", TargetingMode: "BOGUS"}); err == nil {
		t.Fatal("expected error for invalid targeting mode")
	}
}
