package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// draft_campaign builds budget + campaign + ad group (+ optional keywords) as
// one atomic batch using negative temp resource IDs (-1 budget, -2 campaign,
// -3 ad group). The update half lives in tool_campaigns_update.go; the shared
// bidding-strategy helpers live here.

// applyBiddingStrategyCreate sets the bidding sub-field on a campaign create
// map. In v23 bidding_strategy_type is OUTPUT_ONLY, so the strategy is selected
// by setting the matching sub-field. cpa/roas of 0 mean "unset".
func applyBiddingStrategyCreate(campaign map[string]any, strategy string, cpa, roas float64) {
	switch strategy {
	case "MAXIMIZE_CONVERSIONS":
		mc := map[string]any{}
		if cpa != 0 {
			mc["targetCpaMicros"] = microsString(dollarsToMicros(cpa))
		}
		campaign["maximizeConversions"] = mc
	case "MAXIMIZE_CONVERSION_VALUE":
		mcv := map[string]any{}
		if roas != 0 {
			mcv["targetRoas"] = roas
		}
		campaign["maximizeConversionValue"] = mcv
	case "TARGET_CPA":
		if cpa != 0 {
			campaign["targetCpa"] = map[string]any{"targetCpaMicros": microsString(dollarsToMicros(cpa))}
		}
	case "TARGET_ROAS":
		if roas != 0 {
			campaign["targetRoas"] = map[string]any{"targetRoas": roas}
		}
	case "MANUAL_CPC":
		campaign["manualCpc"] = map[string]any{}
	case "TARGET_SPEND", "MAXIMIZE_CLICKS":
		campaign["targetSpend"] = map[string]any{}
	case "TARGET_IMPRESSION_SHARE":
		campaign["targetImpressionShare"] = map[string]any{}
	case "PERCENT_CPC":
		campaign["percentCpc"] = map[string]any{}
	}
	// Unknown strategies leave bidding unset; the API rejects with a clear error.
}

// adGroupTypeForChannel maps a channel type to its standard ad group type.
func adGroupTypeForChannel(channelType string) string {
	if channelType == "DISPLAY" {
		return "DISPLAY_STANDARD"
	}
	return "SEARCH_STANDARD"
}

// DraftCampaignArgs drafts a new campaign with budget, ad group, and optional
// keywords. Defaults to PAUSED for safety.
type DraftCampaignArgs struct {
	CustomerID      string                 `json:"customer_id" jsonschema:"the Google Ads customer ID that will own the campaign"`
	CampaignName    string                 `json:"campaign_name" jsonschema:"the campaign name"`
	DailyBudget     float64                `json:"daily_budget" jsonschema:"daily budget in currency units (capped by the budget guard)"`
	BiddingStrategy string                 `json:"bidding_strategy" jsonschema:"e.g. MAXIMIZE_CONVERSIONS, TARGET_CPA, MANUAL_CPC"`
	TargetCPA       float64                `json:"target_cpa,omitempty" jsonschema:"target CPA in currency units (for TARGET_CPA / MAXIMIZE_CONVERSIONS)"`
	TargetROAS      float64                `json:"target_roas,omitempty" jsonschema:"target ROAS ratio (for TARGET_ROAS / MAXIMIZE_CONVERSION_VALUE)"`
	ChannelType     string                 `json:"channel_type" jsonschema:"advertising channel, e.g. SEARCH or DISPLAY"`
	AdGroupName     string                 `json:"ad_group_name" jsonschema:"the name of the ad group to create"`
	Keywords        []KeywordWithMatchType `json:"keywords,omitempty" jsonschema:"optional keywords to add to the ad group"`
	GeoTargetIDs    []string               `json:"geo_target_ids,omitempty" jsonschema:"optional geo target constant IDs"`
	LanguageIDs     []string               `json:"language_ids,omitempty" jsonschema:"optional language constant IDs"`
	Status          string                 `json:"status,omitempty" jsonschema:"ENABLED, PAUSED, or REMOVED; defaults to PAUSED"`
	Confirm         string                 `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runDraftCampaign(ctx context.Context, c *Client, args DraftCampaignArgs) (WriteResult, error) {
	const tool = "draft_campaign"
	cfg := loadSafetyConfig()
	if err := checkBudgetCap(args.DailyBudget, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if err := checkBlockedOperation(tool, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	for _, kw := range args.Keywords {
		if err := validateMatchType(kw.MatchType); err != nil {
			return WriteResult{}, err
		}
	}
	status, err := parseAdStatus(args.Status)
	if err != nil {
		return WriteResult{}, err
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}

	cid := normalizeCustomerID(args.CustomerID)
	channelType := args.ChannelType
	if channelType == "" {
		channelType = "SEARCH"
	}
	budgetResource := fmt.Sprintf("customers/%s/campaignBudgets/-1", cid)
	campaignResource := fmt.Sprintf("customers/%s/campaigns/-2", cid)
	adGroupResource := fmt.Sprintf("customers/%s/adGroups/-3", cid)

	var ops []any
	// 1. Budget.
	ops = append(ops, map[string]any{"campaignBudgetOperation": map[string]any{"create": map[string]any{
		"name":           args.CampaignName + " Budget",
		"amountMicros":   microsString(dollarsToMicros(args.DailyBudget)),
		"deliveryMethod": "STANDARD",
		"resourceName":   budgetResource,
	}}})

	// 2. Campaign.
	campaignCreate := map[string]any{
		"name":                   args.CampaignName,
		"status":                 string(status),
		"advertisingChannelType": channelType,
		"campaignBudget":         budgetResource,
		"resourceName":           campaignResource,
		"networkSettings": map[string]any{
			"targetGoogleSearch":         true,
			"targetSearchNetwork":        true,
			"targetContentNetwork":       false,
			"targetPartnerSearchNetwork": false,
		},
		// Required by EU TTPA regulation (Oct 2025+); defaults to "does not contain".
		"containsEuPoliticalAdvertising": "DOES_NOT_CONTAIN_EU_POLITICAL_ADVERTISING",
	}
	applyBiddingStrategyCreate(campaignCreate, args.BiddingStrategy, args.TargetCPA, args.TargetROAS)
	ops = append(ops, map[string]any{"campaignOperation": map[string]any{"create": campaignCreate}})

	// 3. Geo targets, 4. Language targets.
	for _, geoID := range args.GeoTargetIDs {
		ops = append(ops, campaignLocationCriterion(campaignResource, geoID))
	}
	for _, langID := range args.LanguageIDs {
		ops = append(ops, campaignLanguageCriterion(campaignResource, langID))
	}

	// 5. Ad group (inherits the campaign status).
	ops = append(ops, map[string]any{"adGroupOperation": map[string]any{"create": map[string]any{
		"name":         args.AdGroupName,
		"campaign":     campaignResource,
		"status":       string(status),
		"type":         adGroupTypeForChannel(channelType),
		"resourceName": adGroupResource,
	}}})

	// 6. Keywords (optional).
	for _, kw := range args.Keywords {
		ops = append(ops, map[string]any{"adGroupCriterionOperation": map[string]any{"create": map[string]any{
			"adGroup": adGroupResource,
			"keyword": map[string]any{"text": kw.Text, "matchType": kw.MatchType},
		}}})
	}

	summary := fmt.Sprintf("Draft %s campaign %q (budget %.2f/day, ad group %q, %d keyword(s), status %s)",
		channelType, args.CampaignName, args.DailyBudget, args.AdGroupName, len(args.Keywords), status)
	res, err := previewMutate(tool, cid, summary, ops)
	if err != nil {
		return WriteResult{}, err
	}
	return res.withCreateStatus(status, enableCampaignHint("<resolve campaign_id from apply response>")), nil
}

// campaignLocationCriterion builds a geo-target campaign criterion create op.
func campaignLocationCriterion(campaignResource, geoID string) map[string]any {
	return map[string]any{"campaignCriterionOperation": map[string]any{"create": map[string]any{
		"campaign": campaignResource,
		"location": map[string]any{"geoTargetConstant": fmt.Sprintf("geoTargetConstants/%s", geoID)},
	}}}
}

// campaignLanguageCriterion builds a language campaign criterion create op.
func campaignLanguageCriterion(campaignResource, langID string) map[string]any {
	return map[string]any{"campaignCriterionOperation": map[string]any{"create": map[string]any{
		"campaign": campaignResource,
		"language": map[string]any{"languageConstant": fmt.Sprintf("languageConstants/%s", langID)},
	}}}
}

// --- CLI front-end ---

var (
	draftCampaignArgs    DraftCampaignArgs
	draftCampaignKwFlags []string
)

var campaignCmd = &cobra.Command{
	Use:   "campaign",
	Short: "Create and update campaigns",
}

var campaignCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Draft a new campaign with budget and ad group (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		for _, s := range draftCampaignKwFlags {
			draftCampaignArgs.Keywords = append(draftCampaignArgs.Keywords, parseKeywordFlag(s))
		}
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runDraftCampaign(cmd.Context(), client, draftCampaignArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	f := campaignCreateCmd.Flags()
	f.StringVar(&draftCampaignArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	f.StringVar(&draftCampaignArgs.CampaignName, "name", "", "campaign name (required)")
	f.Float64Var(&draftCampaignArgs.DailyBudget, "daily-budget", 0, "daily budget in currency units (required)")
	f.StringVar(&draftCampaignArgs.BiddingStrategy, "bidding-strategy", "MAXIMIZE_CONVERSIONS", "bidding strategy")
	f.Float64Var(&draftCampaignArgs.TargetCPA, "target-cpa", 0, "target CPA in currency units")
	f.Float64Var(&draftCampaignArgs.TargetROAS, "target-roas", 0, "target ROAS ratio")
	f.StringVar(&draftCampaignArgs.ChannelType, "channel-type", "SEARCH", "advertising channel, e.g. SEARCH or DISPLAY")
	f.StringVar(&draftCampaignArgs.AdGroupName, "ad-group-name", "", "ad group name (required)")
	f.StringArrayVar(&draftCampaignKwFlags, "keyword", nil, "keyword as text|MATCHTYPE (repeatable)")
	f.StringArrayVar(&draftCampaignArgs.GeoTargetIDs, "geo-target-id", nil, "geo target constant ID (repeatable)")
	f.StringArrayVar(&draftCampaignArgs.LanguageIDs, "language-id", nil, "language constant ID (repeatable)")
	f.StringVar(&draftCampaignArgs.Status, "status", "", "ENABLED, PAUSED (default), or REMOVED")
	f.StringVar(&draftCampaignArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = campaignCreateCmd.MarkFlagRequired("customer-id")
	_ = campaignCreateCmd.MarkFlagRequired("name")
	_ = campaignCreateCmd.MarkFlagRequired("daily-budget")
	_ = campaignCreateCmd.MarkFlagRequired("ad-group-name")

	campaignCmd.AddCommand(campaignCreateCmd)
}
