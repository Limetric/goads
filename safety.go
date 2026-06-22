package main

import (
	"context"
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

// Dispatch routes a confirmed write to the correct REST endpoint. The empty
// value means the default googleAds:mutate path; the recommendation variants
// route to dedicated RPCs because their operations are not valid mutate keys.
const (
	dispatchMutate                = ""
	dispatchApplyRecommendation   = "apply_recommendation"
	dispatchDismissRecommendation = "dismiss_recommendation"
)

// PendingMutation is what a write tool stages for confirmation.
type PendingMutation struct {
	Token      string `json:"token"`
	Tool       string `json:"tool"`
	CustomerID string `json:"customer_id"`
	Summary    string `json:"summary"`
	Operations []any  `json:"operations"`
	// Dispatch selects the apply endpoint; "" = googleAds:mutate.
	Dispatch string `json:"dispatch,omitempty"`
	// ResourceNames carries full recommendation resource paths for the
	// recommendation dispatches (unused for the default mutate path).
	ResourceNames []string  `json:"resource_names,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// stageMutation persists a pending googleAds:mutate and returns its confirm token.
func stageMutation(tool, customerID, summary string, ops []any) (*PendingMutation, error) {
	return stageDispatch(tool, customerID, summary, dispatchMutate, ops, nil)
}

// stageDispatch persists a pending write with an explicit dispatch route. Used
// by recommendation tools that must route to dedicated RPCs on apply.
func stageDispatch(tool, customerID, summary, dispatch string, ops []any, resourceNames []string) (*PendingMutation, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	p := &PendingMutation{
		Token:         tok,
		Tool:          tool,
		CustomerID:    customerID,
		Summary:       summary,
		Operations:    ops,
		Dispatch:      dispatch,
		ResourceNames: resourceNames,
		CreatedAt:     time.Now().UTC(),
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

// applyPending executes a consumed pending write via the endpoint selected by
// its Dispatch: the dedicated recommendation RPCs, or the default mutate path.
func applyPending(ctx context.Context, c *Client, p *PendingMutation) error {
	switch p.Dispatch {
	case dispatchApplyRecommendation:
		_, err := c.ApplyRecommendations(ctx, p.CustomerID, p.ResourceNames)
		return err
	case dispatchDismissRecommendation:
		_, err := c.DismissRecommendations(ctx, p.CustomerID, p.ResourceNames)
		return err
	default:
		_, err := c.Mutate(ctx, p.CustomerID, p.Operations)
		return err
	}
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

// allowedMutateOps is the whitelist of top-level MutateOperation keys accepted
// by Google Ads v23's googleAds:mutate endpoint. Payloads with any key not in
// this set are rejected client-side before any HTTP traffic — this catches
// applyRecommendationOperation / dismissRecommendationOperation mistakes (those
// live on dedicated RPCs, see Client.ApplyRecommendations / DismissRecommendations).
//
// Source: Google Ads API v23 MutateOperation.operation oneof definition.
var allowedMutateOps = map[string]bool{
	"adGroupAdLabelOperation":               true,
	"adGroupAdOperation":                    true,
	"adGroupAssetOperation":                 true,
	"adGroupBidModifierOperation":           true,
	"adGroupCriterionCustomizerOperation":   true,
	"adGroupCriterionLabelOperation":        true,
	"adGroupCriterionOperation":             true,
	"adGroupCustomizerOperation":            true,
	"adGroupExtensionSettingOperation":      true,
	"adGroupFeedOperation":                  true,
	"adGroupLabelOperation":                 true,
	"adGroupOperation":                      true,
	"adOperation":                           true,
	"adParameterOperation":                  true,
	"assetGroupAssetOperation":              true,
	"assetGroupListingGroupFilterOperation": true,
	"assetGroupOperation":                   true,
	"assetGroupSignalOperation":             true,
	"assetOperation":                        true,
	"assetSetAssetOperation":                true,
	"assetSetOperation":                     true,
	"audienceOperation":                     true,
	"biddingDataExclusionOperation":         true,
	"biddingSeasonalityAdjustmentOperation": true,
	"biddingStrategyOperation":              true,
	"campaignAssetOperation":                true,
	"campaignAssetSetOperation":             true,
	"campaignBidModifierOperation":          true,
	"campaignBudgetOperation":               true,
	"campaignConversionGoalOperation":       true,
	"campaignCriterionOperation":            true,
	"campaignCustomizerOperation":           true,
	"campaignDraftOperation":                true,
	"campaignExtensionSettingOperation":     true,
	"campaignFeedOperation":                 true,
	"campaignGroupOperation":                true,
	"campaignLabelOperation":                true,
	"campaignOperation":                     true,
	"campaignSharedSetOperation":            true,
	"conversionActionOperation":             true,
	"conversionCustomVariableOperation":     true,
	"conversionGoalCampaignConfigOperation": true,
	"conversionValueRuleOperation":          true,
	"conversionValueRuleSetOperation":       true,
	"customConversionGoalOperation":         true,
	"customerAssetOperation":                true,
	"customerConversionGoalOperation":       true,
	"customerCustomizerOperation":           true,
	"customerExtensionSettingOperation":     true,
	"customerFeedOperation":                 true,
	"customerLabelOperation":                true,
	"customerNegativeCriterionOperation":    true,
	"customerOperation":                     true,
	"customizerAttributeOperation":          true,
	"experimentArmOperation":                true,
	"experimentOperation":                   true,
	"extensionFeedItemOperation":            true,
	"feedItemOperation":                     true,
	"feedItemSetLinkOperation":              true,
	"feedItemSetOperation":                  true,
	"feedItemTargetOperation":               true,
	"feedMappingOperation":                  true,
	"feedOperation":                         true,
	"keywordPlanAdGroupKeywordOperation":    true,
	"keywordPlanAdGroupOperation":           true,
	"keywordPlanCampaignKeywordOperation":   true,
	"keywordPlanCampaignOperation":          true,
	"keywordPlanOperation":                  true,
	"labelOperation":                        true,
	"remarketingActionOperation":            true,
	"sharedCriterionOperation":              true,
	"sharedSetOperation":                    true,
	"smartCampaignSettingOperation":         true,
	"userListOperation":                     true,
}

// validateMutateOps verifies every operation uses a top-level key from
// allowedMutateOps, returning an actionable error on the first offender. This
// runs before any HTTP traffic (see Client.Mutate).
func validateMutateOps(ops []any) error {
	for i, op := range ops {
		m, ok := op.(map[string]any)
		if !ok {
			return fmt.Errorf("mutate operation at index %d is not a JSON object", i)
		}
		for key := range m {
			if !allowedMutateOps[key] {
				return fmt.Errorf("unknown MutateOperation key %q at index %d: recommendation operations must use apply_recommendations / dismiss_recommendations — they are NOT valid keys on googleAds:mutate in v23", key, i)
			}
		}
	}
	return nil
}
