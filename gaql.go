package main

import (
	"fmt"
	"strings"
)

// GAQL (Google Ads Query Language) is read-only, but tool inputs still flow
// into queries, so we validate and escape carefully.

// validateGAQL performs cheap sanity checks on a raw GAQL query before it is
// sent. It is not a full parser — the API is the source of truth — but it
// catches the obvious mistakes (empty query, missing SELECT, stacked
// statements) before a round-trip.
func validateGAQL(query string) error {
	q := strings.TrimSpace(query)
	if q == "" {
		return fmt.Errorf("empty GAQL query")
	}
	if !strings.HasPrefix(strings.ToUpper(q), "SELECT") {
		return fmt.Errorf("GAQL query must start with SELECT")
	}
	// GAQL is a single statement; a ';' separating statements is not allowed.
	if i := strings.Index(q, ";"); i >= 0 && strings.TrimSpace(q[i+1:]) != "" {
		return fmt.Errorf("GAQL accepts a single statement; remove text after ';'")
	}
	return nil
}

// quoteGAQLString escapes a value for use inside a single-quoted GAQL string
// literal, e.g. WHERE campaign.name = 'quoteGAQLString(name)'.
func quoteGAQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// buildSelect assembles a simple SELECT query. More elaborate builders (date
// ranges, ordering, segments) can layer on top; this covers the common
// "fields FROM resource [WHERE …] [LIMIT n]" shape used by most read tools.
func buildSelect(fields []string, resource, where string, limit int) (string, error) {
	if len(fields) == 0 {
		return "", fmt.Errorf("buildSelect: no fields")
	}
	if strings.TrimSpace(resource) == "" {
		return "", fmt.Errorf("buildSelect: no resource")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s", strings.Join(fields, ", "), resource)
	if w := strings.TrimSpace(where); w != "" {
		fmt.Fprintf(&b, " WHERE %s", w)
	}
	if limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", limit)
	}
	return b.String(), nil
}
