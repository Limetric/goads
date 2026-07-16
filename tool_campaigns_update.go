package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// This file updates a campaign's budget and bidding strategy, and can add
// geo/language targeting. A budget change targets the campaign's budget
// resource (a distinct ID), which is resolved from the API first.

// applyBiddingStrategyUpdate sets the bidding sub-field on a campaign update map
// and records the touched fields in mask. In v23 bidding_strategy_type is
// OUTPUT_ONLY, so the strategy is selected by setting the matching sub-field;
// unknown strategies and missing targets error at preview time rather than
// staging an op Google will reject at confirm (issue #8).
func applyBiddingStrategyUpdate(campaign map[string]any, mask *[]string, strategy string, cpa, roas float64) error {
	switch strategy {
	case "MAXIMIZE_CONVERSIONS":
		mc := map[string]any{}
		if cpa != 0 {
			mc["targetCpaMicros"] = microsString(dollarsToMicros(cpa))
		}
		campaign["maximizeConversions"] = mc
		*mask = append(*mask, "maximizeConversions")
	case "MAXIMIZE_CONVERSION_VALUE":
		mcv := map[string]any{}
		if roas != 0 {
			mcv["targetRoas"] = roas
		}
		campaign["maximizeConversionValue"] = mcv
		*mask = append(*mask, "maximizeConversionValue")
	case "TARGET_CPA":
		if cpa == 0 {
			return fmt.Errorf("TARGET_CPA requires target_cpa (currency units)")
		}
		campaign["targetCpa"] = map[string]any{"targetCpaMicros": microsString(dollarsToMicros(cpa))}
		*mask = append(*mask, "targetCpa")
	case "TARGET_ROAS":
		if roas == 0 {
			return fmt.Errorf("TARGET_ROAS requires target_roas (a ratio, e.g. 3.5)")
		}
		campaign["targetRoas"] = map[string]any{"targetRoas": roas}
		*mask = append(*mask, "targetRoas")
	case "MANUAL_CPC":
		campaign["manualCpc"] = map[string]any{}
		*mask = append(*mask, "manualCpc")
	case "TARGET_SPEND", "MAXIMIZE_CLICKS":
		campaign["targetSpend"] = map[string]any{}
		*mask = append(*mask, "targetSpend")
	case "TARGET_IMPRESSION_SHARE":
		// v23 requires location + fraction (and optionally a CPC ceiling) —
		// an empty object previews fine and is rejected at confirm. Use
		// create_portfolio_bidding_strategy, which stages those fields.
		return fmt.Errorf("TARGET_IMPRESSION_SHARE cannot be set via update_campaign (it requires location/fraction/ceiling parameters) — create it with create_portfolio_bidding_strategy instead")
	case "PERCENT_CPC":
		campaign["percentCpc"] = map[string]any{}
		*mask = append(*mask, "percentCpc")
	default:
		return fmt.Errorf("unsupported bidding strategy %q — use one of MAXIMIZE_CONVERSIONS, MAXIMIZE_CONVERSION_VALUE, TARGET_CPA, TARGET_ROAS, MANUAL_CPC, TARGET_SPEND/MAXIMIZE_CLICKS, PERCENT_CPC", strategy)
	}
	return nil
}

// resolveCampaignBudgetResource looks up a campaign's budget resource name. A
// campaign budget has its own ID distinct from the campaign ID, so a budget
// update must target the real budget resource.
func resolveCampaignBudgetResource(ctx context.Context, c *Client, customerID, campaignID string) (string, error) {
	q := fmt.Sprintf("SELECT campaign.campaign_budget FROM campaign WHERE campaign.id = %s", campaignID)
	rows, err := c.Search(ctx, customerID, q)
	if err != nil {
		return "", err
	}
	if len(rows) > 0 {
		var row struct {
			Campaign struct {
				CampaignBudget string `json:"campaignBudget"`
			} `json:"campaign"`
		}
		if json.Unmarshal(rows[0], &row) == nil && row.Campaign.CampaignBudget != "" {
			return row.Campaign.CampaignBudget, nil
		}
	}
	return "", fmt.Errorf("could not resolve a campaign budget for campaign %s — the campaign may not exist or has no associated budget", campaignID)
}

// UpdateCampaignArgs updates an existing campaign's settings. Only the provided
// fields change; at least one change must be specified.
type UpdateCampaignArgs struct {
	CustomerID      string   `json:"customer_id" jsonschema:"the Google Ads customer ID that owns the campaign"`
	CampaignID      string   `json:"campaign_id" jsonschema:"the campaign ID to update"`
	BiddingStrategy string   `json:"bidding_strategy,omitempty" jsonschema:"new bidding strategy, e.g. MAXIMIZE_CONVERSIONS"`
	TargetCPA       float64  `json:"target_cpa,omitempty" jsonschema:"target CPA in currency units"`
	TargetROAS      float64  `json:"target_roas,omitempty" jsonschema:"target ROAS ratio"`
	DailyBudget     float64  `json:"daily_budget,omitempty" jsonschema:"new daily budget in currency units (capped by the budget guard)"`
	GeoTargetIDs    []string `json:"geo_target_ids,omitempty" jsonschema:"geo target constant IDs to add"`
	LanguageIDs     []string `json:"language_ids,omitempty" jsonschema:"language constant IDs to add"`
	Confirm         string   `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runUpdateCampaign(ctx context.Context, c *Client, args UpdateCampaignArgs) (WriteResult, error) {
	const tool = "update_campaign"
	// Blocked-op check runs before the confirm branch so an operation blocked
	// between preview and confirm cannot still be applied with its token.
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	if cid == "" {
		return WriteResult{}, fmt.Errorf("customer_id is required")
	}
	campaignID, err := numericID("campaign_id", args.CampaignID)
	if err != nil {
		return WriteResult{}, err
	}
	campaignResource := fmt.Sprintf("customers/%s/campaigns/%s", cid, campaignID)
	var ops []any

	// Budget update — resolve the real budget resource first.
	if args.DailyBudget != 0 {
		if err := checkBudgetCap(args.DailyBudget, loadSafetyConfig()); err != nil {
			return WriteResult{}, toolError(tool, err)
		}
		budgetResource, err := resolveCampaignBudgetResource(ctx, c, cid, campaignID)
		if err != nil {
			return WriteResult{}, toolError(tool, err)
		}
		ops = append(ops, map[string]any{"campaignBudgetOperation": map[string]any{
			"update":     map[string]any{"resourceName": budgetResource, "amountMicros": microsString(dollarsToMicros(args.DailyBudget))},
			"updateMask": "amountMicros",
		}})
	}

	// Bidding strategy update.
	if args.BiddingStrategy != "" {
		update := map[string]any{"resourceName": campaignResource}
		var mask []string
		if err := applyBiddingStrategyUpdate(update, &mask, args.BiddingStrategy, args.TargetCPA, args.TargetROAS); err != nil {
			return WriteResult{}, err
		}
		ops = append(ops, map[string]any{"campaignOperation": map[string]any{"update": update, "updateMask": strings.Join(mask, ",")}})
	}

	// Geo and language additions.
	for _, geoID := range args.GeoTargetIDs {
		ops = append(ops, campaignLocationCriterion(campaignResource, geoID))
	}
	for _, langID := range args.LanguageIDs {
		ops = append(ops, campaignLanguageCriterion(campaignResource, langID))
	}

	if len(ops) == 0 {
		return WriteResult{}, fmt.Errorf("no changes specified for campaign update")
	}
	summary := fmt.Sprintf("Update campaign %s (%d operation(s))", args.CampaignID, len(ops))
	return previewMutate(tool, cid, summary, ops)
}

// --- CLI front-end ---

var updateCampaignArgs UpdateCampaignArgs

var campaignUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update a campaign's budget, bidding, or targeting (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runUpdateCampaign(cmd.Context(), client, updateCampaignArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	f := campaignUpdateCmd.Flags()
	f.StringVar(&updateCampaignArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	f.StringVar(&updateCampaignArgs.CampaignID, "campaign-id", "", "campaign ID (required)")
	f.StringVar(&updateCampaignArgs.BiddingStrategy, "bidding-strategy", "", "new bidding strategy")
	f.Float64Var(&updateCampaignArgs.TargetCPA, "target-cpa", 0, "target CPA in currency units")
	f.Float64Var(&updateCampaignArgs.TargetROAS, "target-roas", 0, "target ROAS ratio")
	f.Float64Var(&updateCampaignArgs.DailyBudget, "daily-budget", 0, "new daily budget in currency units")
	f.StringArrayVar(&updateCampaignArgs.GeoTargetIDs, "geo-target-id", nil, "geo target constant ID to add (repeatable)")
	f.StringArrayVar(&updateCampaignArgs.LanguageIDs, "language-id", nil, "language constant ID to add (repeatable)")
	f.StringVar(&updateCampaignArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = campaignUpdateCmd.MarkFlagRequired("customer-id")
	_ = campaignUpdateCmd.MarkFlagRequired("campaign-id")

	campaignCmd.AddCommand(campaignUpdateCmd)
}
