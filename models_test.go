package main

import (
	"encoding/json"
	"testing"
)

func TestParseAdStatus(t *testing.T) {
	tests := []struct {
		in      string
		want    AdStatus
		wantErr bool
	}{
		{"", AdStatusPaused, false},
		{"enabled", AdStatusEnabled, false},
		{"PAUSED", AdStatusPaused, false},
		{" Removed ", AdStatusRemoved, false},
		{"DRAFT", "", true},
	}
	for _, tt := range tests {
		got, err := parseAdStatus(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseAdStatus(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("parseAdStatus(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDefaultAdStatusIsPaused(t *testing.T) {
	if defaultAdStatus != AdStatusPaused {
		t.Errorf("defaultAdStatus = %q, want PAUSED", defaultAdStatus)
	}
}

func TestParseAdRotationMode(t *testing.T) {
	if m, err := parseAdRotationMode("rotate_forever"); err != nil || m != AdRotationRotateForever {
		t.Errorf("parseAdRotationMode(rotate_forever) = %q, %v", m, err)
	}
	if _, err := parseAdRotationMode("ROTATE_INDEFINITELY"); err == nil {
		t.Error("expected error for unknown rotation mode")
	}
}

func TestEnableAdHint(t *testing.T) {
	h := enableAdHint("111", "999")
	if h.Tool != "enable_entity" {
		t.Errorf("tool = %q", h.Tool)
	}
	if h.Params["entity_type"] != "ad" || h.Params["entity_id"] != "111~999" {
		t.Errorf("params = %v", h.Params)
	}
	// Round-trips to the expected JSON shape.
	b, _ := json.Marshal(h)
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back["tool"] != "enable_entity" {
		t.Errorf("marshaled tool = %v", back["tool"])
	}
}

func TestEnableCampaignAndAdGroupHints(t *testing.T) {
	c := enableCampaignHint("CAMP1")
	if c.Params["entity_type"] != "campaign" || c.Params["entity_id"] != "CAMP1" {
		t.Errorf("campaign hint params = %v", c.Params)
	}
	g := enableAdGroupHint("AG1")
	if g.Params["entity_type"] != "ad_group" || g.Params["entity_id"] != "AG1" {
		t.Errorf("ad group hint params = %v", g.Params)
	}
}

func TestParseCreateStatus_RejectsRemoved(t *testing.T) {
	// An entity can never be created in REMOVED status (issue #14).
	if _, err := parseCreateStatus("REMOVED"); err == nil {
		t.Fatal("REMOVED should be rejected for create tools")
	}
	if s, err := parseCreateStatus(""); err != nil || s != AdStatusPaused {
		t.Fatalf("default should stay PAUSED, got %v %v", s, err)
	}
	if s, err := parseCreateStatus("enabled"); err != nil || s != AdStatusEnabled {
		t.Fatalf("ENABLED should pass, got %v %v", s, err)
	}
}
