package main

import "testing"

func TestMinuteEnum(t *testing.T) {
	for in, want := range map[int]string{0: "ZERO", 15: "FIFTEEN", 30: "THIRTY", 45: "FORTY_FIVE"} {
		if got := minuteEnum(in); got != want {
			t.Errorf("minuteEnum(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSetCampaignSchedule_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := SetScheduleArgs{
		CustomerID: "123-456-7890", CampaignID: "555",
		Schedules: []ScheduleEntry{
			{DayOfWeek: "MONDAY", StartHour: 9, StartMinute: 0, EndHour: 17, EndMinute: 30},
			{DayOfWeek: "FRIDAY", StartHour: 8, StartMinute: 15, EndHour: 18, EndMinute: 45},
		},
	}
	prev, err := runSetCampaignSchedule(t.Context(), c, args)
	if err != nil || prev.Token == "" || cap.calls != 0 {
		t.Fatalf("preview: %+v err=%v calls=%d", prev, err, cap.calls)
	}
	args.Confirm = prev.Token
	if _, err := runSetCampaignSchedule(t.Context(), c, args); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(cap.lastOps()) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(cap.lastOps()))
	}
	create := opCreate(t, cap.firstOp(t), "campaignCriterionOperation")
	sched, _ := create["adSchedule"].(map[string]any)
	if sched["dayOfWeek"] != "MONDAY" || sched["startMinute"] != "ZERO" || sched["endMinute"] != "THIRTY" {
		t.Errorf("unexpected adSchedule: %v", sched)
	}
	if create["campaign"] != "customers/1234567890/campaigns/555" {
		t.Errorf("campaign resource = %v", create["campaign"])
	}
}

func TestSetCampaignSchedule_Validation(t *testing.T) {
	cases := []SetScheduleArgs{
		{CustomerID: "1", CampaignID: "5"}, // empty schedules
		{CustomerID: "1", CampaignID: "5", Schedules: []ScheduleEntry{{DayOfWeek: "FUNDAY", EndHour: 17}}},
		{CustomerID: "1", CampaignID: "5", Schedules: []ScheduleEntry{{DayOfWeek: "MONDAY", StartHour: 25}}},
		{CustomerID: "1", CampaignID: "5", Schedules: []ScheduleEntry{{DayOfWeek: "MONDAY", StartMinute: 10}}},
	}
	for i, args := range cases {
		if _, err := runSetCampaignSchedule(t.Context(), nil, args); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestParseScheduleFlag(t *testing.T) {
	e, err := parseScheduleFlag("monday,9,0,17,30")
	if err != nil {
		t.Fatal(err)
	}
	if e.DayOfWeek != "MONDAY" || e.StartHour != 9 || e.EndMinute != 30 {
		t.Errorf("parsed wrong: %+v", e)
	}
	if _, err := parseScheduleFlag("monday,9,0"); err == nil {
		t.Error("expected error for wrong field count")
	}
	if _, err := parseScheduleFlag("monday,x,0,17,30"); err == nil {
		t.Error("expected error for non-numeric field")
	}
}

func TestSetCampaignSchedule_Blocked(t *testing.T) {
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "set_campaign_schedule")
	args := SetScheduleArgs{CustomerID: "1", CampaignID: "5", Schedules: []ScheduleEntry{{DayOfWeek: "MONDAY"}}}
	if _, err := runSetCampaignSchedule(t.Context(), nil, args); err == nil {
		t.Fatal("expected blocked-operation error")
	}
}
