package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// AdsArgs is the input for the `ads` read tool, with an optional date window.
type AdsArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	DateStart  string `json:"date_start,omitempty" jsonschema:"start date YYYY-MM-DD; pair with date_end to scope metrics"`
	DateEnd    string `json:"date_end,omitempty" jsonschema:"end date YYYY-MM-DD; pair with date_start to scope metrics"`
}

// AdsResult is the structured output: enriched ad rows + a count.
type AdsResult struct {
	Ads        []json.RawMessage `json:"ads"`
	TotalCount int               `json:"total_count"`
}

// runAds returns ad-level performance for all non-removed ads, ordered by cost
// descending, with cost fields enriched.
func runAds(ctx context.Context, c *Client, args AdsArgs) (AdsResult, error) {
	if args.CustomerID == "" {
		return AdsResult{}, fmt.Errorf("customer_id is required")
	}
	query := "SELECT " +
		"campaign.name, campaign.id, ad_group.name, ad_group.id, " +
		"ad_group_ad.ad.id, ad_group_ad.ad.type, " +
		"ad_group_ad.ad.responsive_search_ad.headlines, " +
		"ad_group_ad.ad.responsive_search_ad.descriptions, " +
		"ad_group_ad.ad.final_urls, ad_group_ad.status, " +
		"metrics.impressions, metrics.clicks, metrics.ctr, " +
		"metrics.conversions, metrics.cost_micros " +
		"FROM ad_group_ad WHERE ad_group_ad.status != 'REMOVED'" +
		andDateClause(args.DateStart, args.DateEnd) +
		" ORDER BY metrics.cost_micros DESC"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return AdsResult{}, toolError("ads", err)
	}
	rows = enrichCostFields(rows)
	return AdsResult{Ads: rows, TotalCount: len(rows)}, nil
}

// --- CLI front-end ---

var adsArgs AdsArgs

var adsCmd = &cobra.Command{
	Use:   "ads",
	Short: "Show ad-level performance metrics",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runAds(cmd.Context(), client, adsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	adsCmd.Flags().StringVar(&adsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	adsCmd.Flags().StringVar(&adsArgs.DateStart, "date-start", "", "start date YYYY-MM-DD")
	adsCmd.Flags().StringVar(&adsArgs.DateEnd, "date-end", "", "end date YYYY-MM-DD")
	_ = adsCmd.MarkFlagRequired("customer-id")
}
