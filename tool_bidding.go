package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// This file creates portfolio bidding strategies and updates keyword CPC bids.
// Both writes preview first; the keyword-bid update also enforces the
// bid-increase guard.

var validStrategyTypes = map[string]bool{
	"TARGET_CPA": true, "TARGET_ROAS": true, "TARGET_IMPRESSION_SHARE": true,
}

// PortfolioBiddingArgs creates a portfolio (shared) bidding strategy.
type PortfolioBiddingArgs struct {
	CustomerID   string  `json:"customer_id" jsonschema:"the Google Ads customer ID that will own the strategy"`
	Name         string  `json:"name" jsonschema:"a name for the bidding strategy"`
	StrategyType string  `json:"strategy_type" jsonschema:"one of: TARGET_CPA, TARGET_ROAS, TARGET_IMPRESSION_SHARE"`
	TargetCPA    float64 `json:"target_cpa,omitempty" jsonschema:"target CPA in currency units (required for TARGET_CPA)"`
	TargetROAS   float64 `json:"target_roas,omitempty" jsonschema:"target ROAS as a ratio, e.g. 3.0 (required for TARGET_ROAS)"`
	Confirm      string  `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runCreatePortfolioBidding(ctx context.Context, c *Client, args PortfolioBiddingArgs) (WriteResult, error) {
	const tool = "create_portfolio_bidding_strategy"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if !validStrategyTypes[args.StrategyType] {
		return WriteResult{}, fmt.Errorf("invalid strategy type %q: must be TARGET_CPA, TARGET_ROAS, or TARGET_IMPRESSION_SHARE", args.StrategyType)
	}
	if args.Name == "" {
		return WriteResult{}, fmt.Errorf("strategy name cannot be empty")
	}

	strategy := map[string]any{"name": args.Name, "type": args.StrategyType}
	switch args.StrategyType {
	case "TARGET_CPA":
		if args.TargetCPA <= 0 {
			return WriteResult{}, fmt.Errorf("target_cpa is required for TARGET_CPA strategy")
		}
		strategy["targetCpa"] = map[string]any{"targetCpaMicros": microsString(dollarsToMicros(args.TargetCPA))}
	case "TARGET_ROAS":
		if args.TargetROAS <= 0 {
			return WriteResult{}, fmt.Errorf("target_roas is required for TARGET_ROAS strategy")
		}
		strategy["targetRoas"] = map[string]any{"targetRoas": args.TargetROAS}
	case "TARGET_IMPRESSION_SHARE":
		strategy["targetImpressionShare"] = map[string]any{
			"location":               "ANYWHERE_ON_PAGE",
			"locationFractionMicros": "500000",
		}
	}

	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	op := map[string]any{"biddingStrategyOperation": map[string]any{"create": strategy}}
	summary := fmt.Sprintf("Create %s portfolio bidding strategy %q", args.StrategyType, args.Name)
	return previewMutate(tool, normalizeCustomerID(args.CustomerID), summary, []any{op})
}

// UpdateKeywordBidArgs updates a keyword's CPC bid, enforcing the bid-increase
// guard against the supplied current bid.
type UpdateKeywordBidArgs struct {
	CustomerID  string  `json:"customer_id" jsonschema:"the Google Ads customer ID that owns the keyword"`
	AdGroupID   string  `json:"ad_group_id" jsonschema:"the ad group ID"`
	CriterionID string  `json:"criterion_id" jsonschema:"the keyword criterion ID"`
	CurrentBid  float64 `json:"current_bid" jsonschema:"the current bid in currency units (for the safety check)"`
	NewBid      float64 `json:"new_bid" jsonschema:"the desired new bid in currency units"`
	Confirm     string  `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runUpdateKeywordBid(ctx context.Context, c *Client, args UpdateKeywordBidArgs) (WriteResult, error) {
	const tool = "update_keyword_bid"
	cfg := loadSafetyConfig()
	if err := checkBlockedOperation(tool, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if err := checkBidIncrease(args.CurrentBid, args.NewBid, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.AdGroupID == "" || args.CriterionID == "" {
		return WriteResult{}, fmt.Errorf("ad_group_id and criterion_id are required")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	resource := fmt.Sprintf("customers/%s/adGroupCriteria/%s~%s", cid, args.AdGroupID, args.CriterionID)
	op := map[string]any{
		"adGroupCriterionOperation": map[string]any{
			"update":     map[string]any{"resourceName": resource, "cpcBidMicros": microsString(dollarsToMicros(args.NewBid))},
			"updateMask": "cpcBidMicros",
		},
	}
	summary := fmt.Sprintf("Update keyword %s~%s CPC bid to %.2f", args.AdGroupID, args.CriterionID, args.NewBid)
	return previewMutate(tool, cid, summary, []any{op})
}

// --- CLI front-end ---

var (
	portfolioArgs  PortfolioBiddingArgs
	keywordBidArgs UpdateKeywordBidArgs
)

var biddingCmd = &cobra.Command{
	Use:   "bidding",
	Short: "Manage bidding strategies and keyword bids",
}

var biddingPortfolioCmd = &cobra.Command{
	Use:   "create-strategy",
	Short: "Create a portfolio bidding strategy (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runCreatePortfolioBidding(cmd.Context(), client, portfolioArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var biddingKeywordBidCmd = &cobra.Command{
	Use:   "set-keyword-bid",
	Short: "Update a keyword's CPC bid (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runUpdateKeywordBid(cmd.Context(), client, keywordBidArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	biddingPortfolioCmd.Flags().StringVar(&portfolioArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	biddingPortfolioCmd.Flags().StringVar(&portfolioArgs.Name, "name", "", "strategy name (required)")
	biddingPortfolioCmd.Flags().StringVar(&portfolioArgs.StrategyType, "type", "", "TARGET_CPA, TARGET_ROAS, or TARGET_IMPRESSION_SHARE (required)")
	biddingPortfolioCmd.Flags().Float64Var(&portfolioArgs.TargetCPA, "target-cpa", 0, "target CPA in currency units (TARGET_CPA)")
	biddingPortfolioCmd.Flags().Float64Var(&portfolioArgs.TargetROAS, "target-roas", 0, "target ROAS ratio (TARGET_ROAS)")
	biddingPortfolioCmd.Flags().StringVar(&portfolioArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = biddingPortfolioCmd.MarkFlagRequired("customer-id")
	_ = biddingPortfolioCmd.MarkFlagRequired("name")
	_ = biddingPortfolioCmd.MarkFlagRequired("type")

	biddingKeywordBidCmd.Flags().StringVar(&keywordBidArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	biddingKeywordBidCmd.Flags().StringVar(&keywordBidArgs.AdGroupID, "ad-group-id", "", "ad group ID (required)")
	biddingKeywordBidCmd.Flags().StringVar(&keywordBidArgs.CriterionID, "criterion-id", "", "keyword criterion ID (required)")
	biddingKeywordBidCmd.Flags().Float64Var(&keywordBidArgs.CurrentBid, "current-bid", 0, "current bid in currency units (for the safety check)")
	biddingKeywordBidCmd.Flags().Float64Var(&keywordBidArgs.NewBid, "new-bid", 0, "new bid in currency units")
	biddingKeywordBidCmd.Flags().StringVar(&keywordBidArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = biddingKeywordBidCmd.MarkFlagRequired("customer-id")
	_ = biddingKeywordBidCmd.MarkFlagRequired("ad-group-id")
	_ = biddingKeywordBidCmd.MarkFlagRequired("criterion-id")

	biddingCmd.AddCommand(biddingPortfolioCmd, biddingKeywordBidCmd)
}
