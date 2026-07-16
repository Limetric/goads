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
// and records the touched fields in mask. Unknown strategies fall back to the
// output-only v23 biddingStrategyType field.
func applyBiddingStrategyUpdate(campaign map[string]any, mask *[]string, strategy string, cpa, roas float64) {
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
		if cpa != 0 {
			campaign["targetCpa"] = map[string]any{"targetCpaMicros": microsString(dollarsToMicros(cpa))}
			*mask = append(*mask, "targetCpa")
		}
	case "TARGET_ROAS":
		if roas != 0 {
			campaign["targetRoas"] = map[string]any{"targetRoas": roas}
			*mask = append(*mask, "targetRoas")
		}
	case "MANUAL_CPC":
		campaign["manualCpc"] = map[string]any{}
		*mask = append(*mask, "manualCpc")
	default:
		campaign["biddingStrategyType"] = strategy
		*mask = append(*mask, "biddingStrategyType")
	}
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
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	campaignResource := fmt.Sprintf("customers/%s/campaigns/%s", cid, args.CampaignID)
	var ops []any

	// Budget update — resolve the real budget resource first.
	if args.DailyBudget != 0 {
		if err := checkBudgetCap(args.DailyBudget, loadSafetyConfig()); err != nil {
			return WriteResult{}, toolError(tool, err)
		}
		budgetResource, err := resolveCampaignBudgetResource(ctx, c, cid, args.CampaignID)
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
		applyBiddingStrategyUpdate(update, &mask, args.BiddingStrategy, args.TargetCPA, args.TargetROAS)
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
