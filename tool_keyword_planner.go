package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// This file discovers keyword ideas from seed terms via the Keyword Planner
// generateKeywordIdeas endpoint and pulls historical performance for specific
// keywords as a rough forecast.

// DiscoverKeywordsArgs seeds keyword-idea discovery.
type DiscoverKeywordsArgs struct {
	CustomerID   string   `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	SeedKeywords []string `json:"seed_keywords" jsonschema:"seed keywords to expand into ideas, e.g. ['running shoes','trainers']"`
}

type DiscoverKeywordsResult struct {
	KeywordIdeas []json.RawMessage `json:"keyword_ideas"`
	TotalCount   int               `json:"total_count"`
}

func runDiscoverKeywords(ctx context.Context, c *Client, args DiscoverKeywordsArgs) (DiscoverKeywordsResult, error) {
	if args.CustomerID == "" {
		return DiscoverKeywordsResult{}, fmt.Errorf("customer_id is required")
	}
	if len(args.SeedKeywords) == 0 {
		return DiscoverKeywordsResult{}, fmt.Errorf("at least one seed keyword is required")
	}
	rows, err := c.GenerateKeywordIdeas(ctx, args.CustomerID, args.SeedKeywords, 50)
	if err != nil {
		return DiscoverKeywordsResult{}, toolError("keyword_ideas", err)
	}
	return DiscoverKeywordsResult{KeywordIdeas: rows, TotalCount: len(rows)}, nil
}

// KeywordForecastsArgs pulls recent historical metrics for specific keywords.
type KeywordForecastsArgs struct {
	CustomerID   string   `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	KeywordTexts []string `json:"keyword_texts" jsonschema:"the keyword texts to look up, e.g. ['running shoes']"`
}

type KeywordForecastsResult struct {
	KeywordForecasts []json.RawMessage `json:"keyword_forecasts"`
	TotalCount       int               `json:"total_count"`
	Message          string            `json:"message,omitempty"`
}

func runKeywordForecasts(ctx context.Context, c *Client, args KeywordForecastsArgs) (KeywordForecastsResult, error) {
	if args.CustomerID == "" {
		return KeywordForecastsResult{}, fmt.Errorf("customer_id is required")
	}
	if len(args.KeywordTexts) == 0 {
		return KeywordForecastsResult{KeywordForecasts: []json.RawMessage{}, TotalCount: 0, Message: "No keywords provided."}, nil
	}

	quoted := make([]string, len(args.KeywordTexts))
	for i, kw := range args.KeywordTexts {
		quoted[i] = "'" + strings.ReplaceAll(kw, "'", `\'`) + "'"
	}
	query := "SELECT " +
		"ad_group_criterion.keyword.text, metrics.average_cpc, metrics.impressions, " +
		"metrics.clicks, metrics.cost_micros, metrics.average_cpm " +
		"FROM keyword_view WHERE ad_group_criterion.keyword.text IN (" + strings.Join(quoted, ", ") + ") " +
		"AND segments.date DURING LAST_30_DAYS"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return KeywordForecastsResult{}, toolError("keyword_forecasts", err)
	}
	if len(rows) == 0 {
		return KeywordForecastsResult{
			KeywordForecasts: []json.RawMessage{},
			TotalCount:       0,
			Message:          "No matching keywords found in the account. These keywords may not exist in any active ad group.",
		}, nil
	}
	rows = enrichCostFields(rows)
	return KeywordForecastsResult{KeywordForecasts: rows, TotalCount: len(rows)}, nil
}

// --- CLI front-end ---

var (
	discoverArgs  DiscoverKeywordsArgs
	forecastsArgs KeywordForecastsArgs
)

var keywordIdeasCmd = &cobra.Command{
	Use:   "keyword-ideas",
	Short: "Discover keyword ideas from seed keywords (Keyword Planner)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runDiscoverKeywords(cmd.Context(), client, discoverArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var keywordForecastsCmd = &cobra.Command{
	Use:   "keyword-forecasts",
	Short: "Get recent historical metrics for specific keywords",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runKeywordForecasts(cmd.Context(), client, forecastsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	keywordIdeasCmd.Flags().StringVar(&discoverArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	keywordIdeasCmd.Flags().StringSliceVar(&discoverArgs.SeedKeywords, "seed", nil, "seed keyword (repeatable)")
	_ = keywordIdeasCmd.MarkFlagRequired("customer-id")
	_ = keywordIdeasCmd.MarkFlagRequired("seed")

	keywordForecastsCmd.Flags().StringVar(&forecastsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	keywordForecastsCmd.Flags().StringSliceVar(&forecastsArgs.KeywordTexts, "keyword", nil, "keyword text (repeatable)")
	_ = keywordForecastsCmd.MarkFlagRequired("customer-id")
}
