package main

import (
	"strings"
	"testing"
)

func TestCreateCustomAudience_ErrorsInsteadOfDeadToken(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	// v23 custom audiences need the dedicated customAudiences:mutate service;
	// the tool used to hand out a confirm token that could never be applied
	// (issue #9). It must now fail at preview time with guidance, issue no
	// token, and never reach the API.
	res, err := runCreateCustomAudience(t.Context(), c, CreateAudienceArgs{
		CustomerID: "123-456-7890", AudienceName: "My Audience", AudienceType: "WEBSITE_VISITORS",
		URLsOrRules: []string{"example.com/products"},
	})
	if err == nil || !strings.Contains(err.Error(), "customAudiences:mutate") {
		t.Fatalf("expected a not-supported error with guidance, got result=%+v err=%v", res, err)
	}
	if res.Token != "" {
		t.Errorf("no confirm token may be issued: %+v", res)
	}
	if cap.calls != 0 {
		t.Errorf("server should never be called")
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
