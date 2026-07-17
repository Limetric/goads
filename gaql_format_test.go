package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestParseSelectFields(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"with where", "SELECT campaign.id, campaign.name FROM campaign WHERE campaign.status = 'ENABLED'", []string{"campaign.id", "campaign.name"}},
		{"no from", "SELECT campaign.id, campaign.name", []string{"campaign.id", "campaign.name"}},
		{"empty", "FROM campaign", nil},
		{"lowercase", "select campaign.id from campaign", []string{"campaign.id"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSelectFields(tt.query)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("parseSelectFields(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestDateClause(t *testing.T) {
	got := dateClause("2024-01-01", "2024-01-31")
	want := "segments.date BETWEEN '2024-01-01' AND '2024-01-31'"
	if got != want {
		t.Errorf("dateClause = %q, want %q", got, want)
	}
}

func TestEnrichCostFields(t *testing.T) {
	t.Run("string micros", func(t *testing.T) {
		rows := enrichCostFields([]json.RawMessage{raw(`{"metrics":{"cost_micros":"1500000"}}`)})
		var got struct {
			Metrics struct {
				CostReadable string `json:"cost_readable"`
			} `json:"metrics"`
		}
		if err := json.Unmarshal(rows[0], &got); err != nil {
			t.Fatal(err)
		}
		if got.Metrics.CostReadable != "1.50" {
			t.Errorf("cost_readable = %q, want 1.50", got.Metrics.CostReadable)
		}
	})

	t.Run("numeric micros", func(t *testing.T) {
		rows := enrichCostFields([]json.RawMessage{raw(`{"metrics":{"cost_micros":2500000}}`)})
		if !strings.Contains(string(rows[0]), `"cost_readable":"2.50"`) {
			t.Errorf("expected cost_readable 2.50, got %s", rows[0])
		}
	})

	t.Run("camelCase micros", func(t *testing.T) {
		rows := enrichCostFields([]json.RawMessage{raw(`{"metrics":{"costMicros":"3000000"}}`)})
		if !strings.Contains(string(rows[0]), `"cost_readable":"3.00"`) {
			t.Errorf("expected cost_readable 3.00, got %s", rows[0])
		}
	})

	t.Run("preserves large id strings", func(t *testing.T) {
		rows := enrichCostFields([]json.RawMessage{raw(`{"campaign":{"id":"123456789012345"}}`)})
		if !strings.Contains(string(rows[0]), `"id":"123456789012345"`) {
			t.Errorf("id mangled: %s", rows[0])
		}
	})
}

func TestResolveField(t *testing.T) {
	row, ok := decodeRow(raw(`{"campaign":{"id":123456789012345,"name":"Test","active":true,"ratio":1.5,"labels":["a","b"],"empty":null}}`))
	if !ok {
		t.Fatal("decodeRow failed")
	}
	cases := []struct {
		name, field, want string
	}{
		{"string", "campaign.name", "Test"},
		{"large integer keeps precision", "campaign.id", "123456789012345"},
		{"decimal number", "campaign.ratio", "1.5"},
		{"bool", "campaign.active", "true"},
		{"null", "campaign.empty", ""},
		{"non-scalar marshals to json", "campaign.labels", `["a","b"]`},
		{"missing leaf", "campaign.missing", ""},
		{"missing root", "nope.field", ""},
		{"path through a non-map", "campaign.name.deeper", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveField(row, tc.field); got != tc.want {
				t.Errorf("resolveField(%q) = %q, want %q", tc.field, got, tc.want)
			}
		})
	}

	t.Run("float64 value", func(t *testing.T) {
		if got := resolveField(map[string]any{"v": 2.5}, "v"); got != "2.5" {
			t.Errorf("resolveField(float64) = %q, want 2.5", got)
		}
	})

	// GAQL SELECT paths are snake_case but REST rows carry camelCase keys —
	// every segment must fall back to its camelCase form or built-in reads
	// render blank cells for fields like metrics.cost_micros (issue #17).
	t.Run("snake_case path resolves camelCase keys", func(t *testing.T) {
		row := map[string]any{
			"adGroupAd": map[string]any{"ad": map[string]any{"id": "42"}},
			"metrics":   map[string]any{"costMicros": "5000000"},
		}
		if got := resolveField(row, "ad_group_ad.ad.id"); got != "42" {
			t.Errorf("resolveField(ad_group_ad.ad.id) = %q, want 42", got)
		}
		if got := resolveField(row, "metrics.cost_micros"); got != "5000000" {
			t.Errorf("resolveField(metrics.cost_micros) = %q, want 5000000", got)
		}
	})
}

func TestParseSelectFields_FromInsideFieldName(t *testing.T) {
	// "FROM" as a substring of a field name must not terminate the SELECT
	// clause — only the standalone FROM keyword does.
	got := parseSelectFields("SELECT metrics.conversions_from_interactions_rate, campaign.id FROM campaign")
	want := []string{"metrics.conversions_from_interactions_rate", "campaign.id"}
	if len(got) != len(want) {
		t.Fatalf("parseSelectFields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSnakeToCamel(t *testing.T) {
	cases := map[string]string{
		"cost_micros":          "costMicros",
		"ad_group_ad":          "adGroupAd",
		"metrics":              "metrics",
		"avg_monthly_searches": "avgMonthlySearches",
		"a__b":                 "aB",
	}
	for in, want := range cases {
		if got := snakeToCamel(in); got != want {
			t.Errorf("snakeToCamel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatTable(t *testing.T) {
	rows := []json.RawMessage{
		raw(`{"campaign":{"id":"123","name":"Test"}}`),
		raw(`{"campaign":{"id":"456","name":"Another"}}`),
	}
	fields := []string{"campaign.id", "campaign.name"}
	table := formatTable(rows, fields)
	for _, want := range []string{"campaign.id", "123", "Another"} {
		if !strings.Contains(table, want) {
			t.Errorf("table missing %q:\n%s", want, table)
		}
	}
}

func TestFormatTable_UnicodeWidth(t *testing.T) {
	// Column widths are measured in runes, not bytes: "Café" (5 bytes) must be
	// padded like the 4-character cell it is, or every non-ASCII name pushes
	// its column out of alignment (issue #17).
	rows := []json.RawMessage{
		raw(`{"campaign":{"name":"Café","id":"1"}}`),
		raw(`{"campaign":{"name":"Shop","id":"2"}}`),
	}
	table := formatTable(rows, []string{"campaign.name", "campaign.id"})
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header+separator+2 rows, got %d lines:\n%s", len(lines), table)
	}
	// Visual alignment = equal rune count before the column separator.
	prefixWidth := func(line string) int {
		return utf8.RuneCountInString(line[:strings.Index(line, "|")])
	}
	cafe, shop := lines[2], lines[3]
	if prefixWidth(cafe) != prefixWidth(shop) {
		t.Errorf("columns misaligned for non-ASCII cell:\n%s", table)
	}
}

// failingWriter always errors, standing in for a closed stdout.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailingWriter
}

var errFailingWriter = errors.New("write failed")

func TestPrintResult(t *testing.T) {
	res := CampaignsResult{
		Campaigns:    []json.RawMessage{raw(`{"campaign":{"id":"1"}}`)},
		TotalCount:   1,
		selectFields: []string{"campaign.id"},
	}

	t.Run("unknown format errors", func(t *testing.T) {
		if err := printResult(&strings.Builder{}, "yaml", res); err == nil {
			t.Error("unknown format should error")
		}
	})

	t.Run("table on a non-row result errors", func(t *testing.T) {
		if err := printResult(&strings.Builder{}, "table", struct{}{}); err == nil {
			t.Error("non-rowSource result should reject table output")
		}
	})

	t.Run("write failures propagate", func(t *testing.T) {
		for _, format := range []string{"json", "table", "csv"} {
			if err := printResult(failingWriter{}, format, res); err == nil {
				t.Errorf("printResult(%s) should surface the write error", format)
			}
		}
	})
}

func TestFormatTableEmpty(t *testing.T) {
	if got := formatTable(nil, []string{"field"}); got != "No results found." {
		t.Errorf("formatTable(empty) = %q", got)
	}
}

func TestFormatCSV(t *testing.T) {
	rows := []json.RawMessage{raw(`{"campaign":{"id":"123","name":"Test"}}`)}
	csv := formatCSV(rows, []string{"campaign.id", "campaign.name"})
	if !strings.HasPrefix(csv, "campaign.id,campaign.name\n") {
		t.Errorf("csv header wrong:\n%s", csv)
	}
	if !strings.Contains(csv, "123,Test") {
		t.Errorf("csv row wrong:\n%s", csv)
	}
}

func TestFormatCSVEscaping(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"comma", `{"name":"Hello, World"}`, `"Hello, World"`},
		{"newline", `{"name":"Hello\nWorld"}`, "\"Hello\nWorld\""},
		{"quotes", `{"name":"Say \"hello\""}`, `"Say ""hello"""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			csv := formatCSV([]json.RawMessage{raw(tt.in)}, []string{"name"})
			if !strings.Contains(csv, tt.want) {
				t.Errorf("csv = %q, want substring %q", csv, tt.want)
			}
		})
	}
}

func TestGetErrorHint(t *testing.T) {
	if hint := getErrorHint("UNRECOGNIZED_FIELD in query"); !strings.Contains(hint, "field name") {
		t.Errorf("expected hint for UNRECOGNIZED_FIELD, got %q", hint)
	}
	if hint := getErrorHint("some random error"); hint != "" {
		t.Errorf("expected no hint, got %q", hint)
	}
}
