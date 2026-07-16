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
	// SELECT must be the leading keyword (word boundary — "SELECTfoo" is not).
	if upper := strings.ToUpper(q); !strings.HasPrefix(upper, "SELECT") ||
		len(q) < 7 || (q[6] != ' ' && q[6] != '\t' && q[6] != '\n' && q[6] != '\r') {
		return fmt.Errorf("GAQL query must start with SELECT")
	}
	// GAQL is a single statement; a ';' separating statements is not allowed.
	// Semicolons inside quoted string literals are fine (issue #13).
	var inQuote byte
	escaped := false
	for i := 0; i < len(q); i++ {
		ch := q[i]
		switch {
		case escaped:
			escaped = false
		case inQuote != 0 && ch == '\\':
			escaped = true
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			}
		case ch == '\'' || ch == '"':
			inQuote = ch
		case ch == ';':
			if strings.TrimSpace(q[i+1:]) != "" {
				return fmt.Errorf("GAQL accepts a single statement; remove text after ';'")
			}
		}
	}
	return nil
}

// numericID validates that a caller-supplied entity ID is purely numeric
// before it is interpolated into GAQL or a resource name. IDs are attacker/
// model-supplied input; "1 OR campaign.id > 0" must never reach a query
// (issues #8, #13).
func numericID(kind, id string) (string, error) {
	if id == "" {
		return "", fmt.Errorf("%s is required", kind)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("%s %q must be a plain numeric ID", kind, id)
		}
	}
	return id, nil
}

// numericIDs validates every entry of a caller-supplied ID list.
func numericIDs(kind string, ids []string) error {
	for _, id := range ids {
		if _, err := numericID(kind, id); err != nil {
			return err
		}
	}
	return nil
}

// escapeGAQLString escapes a value for interpolation inside a single-quoted
// GAQL string literal: backslashes first, then quotes — escaping only quotes
// lets a trailing backslash break out of the literal.
func escapeGAQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}

// quoteGAQLString escapes a value for use inside a single-quoted GAQL string
// literal, e.g. WHERE campaign.name = 'quoteGAQLString(name)'.
func quoteGAQLString(s string) string {
	return "'" + escapeGAQLString(s) + "'"
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
