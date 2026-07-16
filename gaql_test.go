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

func TestValidateGAQL_SemicolonInsideStringLiteral(t *testing.T) {
	// Semicolons inside quoted literals are valid GAQL; only a statement
	// separator is not (issue #13).
	if err := validateGAQL(`SELECT campaign.id FROM campaign WHERE campaign.name = 'a;b' LIMIT 5`); err != nil {
		t.Fatalf("semicolon in string literal should be accepted: %v", err)
	}
	if err := validateGAQL(`SELECT campaign.id FROM campaign; DROP x`); err == nil {
		t.Fatal("stacked statements should be rejected")
	}
	if err := validateGAQL(`SELECTfoo FROM campaign`); err == nil {
		t.Fatal("SELECT requires a word boundary")
	}
}

func TestEscapeGAQLString(t *testing.T) {
	// Escaping only quotes let a trailing backslash break out of the literal:
	// 'foo\' + escaped quote = 'foo\\' which terminates the string (issue #13).
	cases := map[string]string{
		`plain`:      `plain`,
		`O'Brien`:    `O\'Brien`,
		`back\slash`: `back\\slash`,
		`trailing\`:  `trailing\\`,
		`evil\'`:     `evil\\\'`,
	}
	for in, want := range cases {
		if got := escapeGAQLString(in); got != want {
			t.Errorf("escapeGAQLString(%q) = %q, want %q", in, got, want)
		}
	}
	if got := quoteGAQLString(`a'b`); got != `'a\'b'` {
		t.Errorf("quoteGAQLString = %q", got)
	}
}

func TestNumericID(t *testing.T) {
	if _, err := numericID("campaign_id", "12345"); err != nil {
		t.Fatalf("plain numeric ID should pass: %v", err)
	}
	for _, bad := range []string{"", "12a", "1 OR campaign.id > 0", "12~34", "-1"} {
		if _, err := numericID("campaign_id", bad); err == nil {
			t.Errorf("id %q should be rejected", bad)
		}
	}
}
