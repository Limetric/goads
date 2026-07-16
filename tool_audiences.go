package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// This file creates custom audiences and attaches audience targeting to
// campaigns. Both operations preview first.

var (
	validAudienceTypes  = map[string]bool{"WEBSITE_VISITORS": true, "CUSTOMER_MATCH": true}
	validTargetingModes = map[string]bool{"TARGETING": true, "OBSERVATION": true}
)

// CreateAudienceArgs creates a custom audience from URL patterns or rules.
//
// Note: customAudienceOperation is not a googleAds:mutate key in v23 (custom
// audiences have a dedicated service), so applying this is intentionally gated
// by the mutate allow-list. The preview is still useful for review.
type CreateAudienceArgs struct {
	CustomerID   string   `json:"customer_id" jsonschema:"the Google Ads customer ID that will own the audience"`
	AudienceName string   `json:"audience_name" jsonschema:"a name for the custom audience"`
	AudienceType string   `json:"audience_type" jsonschema:"one of: WEBSITE_VISITORS, CUSTOMER_MATCH"`
	URLsOrRules  []string `json:"urls_or_rules" jsonschema:"URL-contains patterns or matching rules for the audience"`
	Confirm      string   `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runCreateCustomAudience(ctx context.Context, c *Client, args CreateAudienceArgs) (WriteResult, error) {
	const tool = "create_custom_audience"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if !validAudienceTypes[args.AudienceType] {
		return WriteResult{}, fmt.Errorf("invalid audience type %q: must be WEBSITE_VISITORS or CUSTOMER_MATCH", args.AudienceType)
	}
	if args.AudienceName == "" {
		return WriteResult{}, fmt.Errorf("audience name cannot be empty")
	}
	if len(args.URLsOrRules) == 0 {
		return WriteResult{}, fmt.Errorf("at least one URL pattern or rule is required")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	members := make([]any, len(args.URLsOrRules))
	for i, rule := range args.URLsOrRules {
		members[i] = map[string]any{"keyword": map[string]any{"value": rule}}
	}
	op := map[string]any{
		"customAudienceOperation": map[string]any{
			"create": map[string]any{
				"name":    args.AudienceName,
				"type":    args.AudienceType,
				"members": members,
			},
		},
	}
	summary := fmt.Sprintf("Create %s custom audience %q with %d rule(s)", args.AudienceType, args.AudienceName, len(args.URLsOrRules))
	return previewMutate(tool, normalizeCustomerID(args.CustomerID), summary, []any{op})
}

// AddAudienceTargetingArgs attaches an audience to a campaign in TARGETING or
// OBSERVATION mode.
type AddAudienceTargetingArgs struct {
	CustomerID    string `json:"customer_id" jsonschema:"the Google Ads customer ID that owns the campaign"`
	CampaignID    string `json:"campaign_id" jsonschema:"the campaign ID to target"`
	AudienceID    string `json:"audience_id" jsonschema:"the user list / audience ID to attach"`
	TargetingMode string `json:"targeting_mode" jsonschema:"TARGETING (limit to audience) or OBSERVATION (monitor only)"`
	Confirm       string `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runAddAudienceTargeting(ctx context.Context, c *Client, args AddAudienceTargetingArgs) (WriteResult, error) {
	const tool = "add_audience_targeting"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if !validTargetingModes[args.TargetingMode] {
		return WriteResult{}, fmt.Errorf("invalid targeting mode %q: must be TARGETING or OBSERVATION", args.TargetingMode)
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	criterion := map[string]any{
		"campaign": fmt.Sprintf("customers/%s/campaigns/%s", cid, args.CampaignID),
		"userList": map[string]any{"userList": fmt.Sprintf("customers/%s/userLists/%s", cid, args.AudienceID)},
	}
	if args.TargetingMode == "OBSERVATION" {
		criterion["bidModifier"] = 1.0
	}
	op := map[string]any{"campaignCriterionOperation": map[string]any{"create": criterion}}
	summary := fmt.Sprintf("Attach audience %s to campaign %s (%s)", args.AudienceID, args.CampaignID, args.TargetingMode)
	return previewMutate(tool, cid, summary, []any{op})
}

// --- CLI front-end ---

var (
	createAudienceArgs CreateAudienceArgs
	addTargetingArgs   AddAudienceTargetingArgs
)

var audienceCmd = &cobra.Command{
	Use:   "audience",
	Short: "Create custom audiences and attach audience targeting",
}

var audienceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a custom audience (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runCreateCustomAudience(cmd.Context(), client, createAudienceArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var audienceTargetCmd = &cobra.Command{
	Use:   "target",
	Short: "Attach audience targeting to a campaign (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runAddAudienceTargeting(cmd.Context(), client, addTargetingArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	audienceCreateCmd.Flags().StringVar(&createAudienceArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	audienceCreateCmd.Flags().StringVar(&createAudienceArgs.AudienceName, "name", "", "audience name (required)")
	audienceCreateCmd.Flags().StringVar(&createAudienceArgs.AudienceType, "type", "", "WEBSITE_VISITORS or CUSTOMER_MATCH (required)")
	audienceCreateCmd.Flags().StringArrayVar(&createAudienceArgs.URLsOrRules, "rule", nil, "URL pattern or rule (repeatable, required)")
	audienceCreateCmd.Flags().StringVar(&createAudienceArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = audienceCreateCmd.MarkFlagRequired("customer-id")
	_ = audienceCreateCmd.MarkFlagRequired("name")
	_ = audienceCreateCmd.MarkFlagRequired("type")
	_ = audienceCreateCmd.MarkFlagRequired("rule")

	audienceTargetCmd.Flags().StringVar(&addTargetingArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	audienceTargetCmd.Flags().StringVar(&addTargetingArgs.CampaignID, "campaign-id", "", "campaign ID (required)")
	audienceTargetCmd.Flags().StringVar(&addTargetingArgs.AudienceID, "audience-id", "", "audience/user list ID (required)")
	audienceTargetCmd.Flags().StringVar(&addTargetingArgs.TargetingMode, "mode", "", "TARGETING or OBSERVATION (required)")
	audienceTargetCmd.Flags().StringVar(&addTargetingArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = audienceTargetCmd.MarkFlagRequired("customer-id")
	_ = audienceTargetCmd.MarkFlagRequired("campaign-id")
	_ = audienceTargetCmd.MarkFlagRequired("audience-id")
	_ = audienceTargetCmd.MarkFlagRequired("mode")

	audienceCmd.AddCommand(audienceCreateCmd, audienceTargetCmd)
}
