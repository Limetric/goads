package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This file ports the upstream `safety/` module: write guards, a human-readable
// mutation preview, and a confirm-token flow. The rule: no mutating call
// executes on first request. A write tool returns a preview plus a short-lived
// token; the caller re-invokes with that token to actually apply the change.
//
// The token store is file-backed (under stateDir) so it survives across the
// stateless CLI invocations a skill makes, and works the same inside a
// long-lived `goads mcp` session.

// confirmTTL bounds how long a pending confirmation is valid.
const confirmTTL = 10 * time.Minute

// PendingMutation is what a write tool stages for confirmation.
type PendingMutation struct {
	Token      string    `json:"token"`
	Tool       string    `json:"tool"`
	CustomerID string    `json:"customer_id"`
	Summary    string    `json:"summary"`
	Operations []any     `json:"operations"`
	CreatedAt  time.Time `json:"created_at"`
}

// stageMutation persists a pending mutation and returns its confirm token.
func stageMutation(tool, customerID, summary string, ops []any) (*PendingMutation, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	p := &PendingMutation{
		Token:      tok,
		Tool:       tool,
		CustomerID: customerID,
		Summary:    summary,
		Operations: ops,
		CreatedAt:  time.Now().UTC(),
	}
	dir, err := stateDir()
	if err != nil {
		// No persistent state: still return the staged mutation so the MCP
		// in-process path (which keeps it in memory) can use it; the CLI path
		// will report that confirmation persistence is unavailable.
		return p, nil
	}
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "pending-"+tok+".json"), data, 0o600); err != nil {
		return nil, fmt.Errorf("stage confirmation: %w", err)
	}
	return p, nil
}

// consumeMutation loads and deletes a pending mutation by token, rejecting
// unknown or expired tokens.
func consumeMutation(token string) (*PendingMutation, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("no confirmation token provided")
	}
	dir, err := stateDir()
	if err != nil {
		return nil, fmt.Errorf("confirmation store unavailable: %w", err)
	}
	path := filepath.Join(dir, "pending-"+token+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unknown or already-used confirmation token %q", token)
	}
	var p PendingMutation
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("corrupt confirmation %q: %w", token, err)
	}
	_ = os.Remove(path) // single-use, even on later failure
	if time.Since(p.CreatedAt) > confirmTTL {
		return nil, fmt.Errorf("confirmation token %q expired (valid for %s); re-run the command to get a fresh one", token, confirmTTL)
	}
	return &p, nil
}

// previewText renders a staged mutation for a human/agent to review.
func (p *PendingMutation) previewText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "PREVIEW — %s on customer %s\n", p.Tool, p.CustomerID)
	fmt.Fprintf(&b, "%s\n", p.Summary)
	fmt.Fprintf(&b, "%d operation(s) staged. Nothing has been changed yet.\n", len(p.Operations))
	fmt.Fprintf(&b, "\nTo apply, re-run with: --confirm %s\n", p.Token)
	return b.String()
}

// auditLog appends a single line describing an applied mutation. Best-effort:
// audit failures never block or fail the operation.
func auditLog(p *PendingMutation, applied bool) {
	dir, err := stateDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s tool=%s customer=%s ops=%d applied=%t token=%s\n",
		time.Now().UTC().Format(time.RFC3339), p.Tool, p.CustomerID, len(p.Operations), applied, p.Token)
}

func newToken() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
