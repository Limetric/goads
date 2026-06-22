package main

import "testing"

func TestValidateGAQL(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"simple select", "SELECT campaign.id FROM campaign", false},
		{"lowercase select", "select campaign.id from campaign", false},
		{"trailing semicolon ok", "SELECT campaign.id FROM campaign;", false},
		{"empty", "   ", true},
		{"not a select", "DELETE FROM campaign", true},
		{"stacked statements", "SELECT campaign.id FROM campaign; SELECT 1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGAQL(tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateGAQL(%q) error = %v, wantErr = %v", tt.query, err, tt.wantErr)
			}
		})
	}
}

func TestQuoteGAQLString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "'plain'"},
		{"O'Brien", `'O\'Brien'`},
		{`back\slash`, `'back\\slash'`},
	}
	for _, tt := range tests {
		if got := quoteGAQLString(tt.in); got != tt.want {
			t.Errorf("quoteGAQLString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildSelect(t *testing.T) {
	got, err := buildSelect([]string{"campaign.id", "campaign.name"}, "campaign", "campaign.status = 'ENABLED'", 50)
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT campaign.id, campaign.name FROM campaign WHERE campaign.status = 'ENABLED' LIMIT 50"
	if got != want {
		t.Errorf("buildSelect =\n  %q\nwant\n  %q", got, want)
	}
	if _, err := buildSelect(nil, "campaign", "", 0); err == nil {
		t.Error("expected error for empty field list")
	}
}
