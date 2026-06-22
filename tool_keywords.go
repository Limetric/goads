package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// This file ports upstream `tools/keywords.rs`: three read tools over keyword
// data — performance (keyword_view), search terms (search_term_view), and
// campaign-level negative keywords (campaign_criterion).

// KeywordPerformanceArgs scopes keyword performance to an optional date window.
type KeywordPerformanceArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	DateStart  string `json:"date_start,omitempty" jsonschema:"start date YYYY-MM-DD; pair with date_end to scope metrics"`
	DateEnd    string `json:"date_end,omitempty" jsonschema:"end date YYYY-MM-DD; pair with date_start to scope metrics"`
}

type KeywordPerformanceResult struct {
	Keywords   []json.RawMessage `json:"keywords"`
	TotalCount int               `json:"total_count"`
}

func runKeywordPerformance(ctx context.Context, c *Client, args KeywordPerformanceArgs) (KeywordPerformanceResult, error) {
	if args.CustomerID == "" {
		return KeywordPerformanceResult{}, fmt.Errorf("customer_id is required")
	}
	query := "SELECT " +
		"campaign.name, ad_group.name, ad_group_criterion.keyword.text, " +
		"ad_group_criterion.keyword.match_type, ad_group_criterion.quality_info.quality_score, " +
		"metrics.impressions, metrics.clicks, metrics.ctr, metrics.average_cpc, " +
		"metrics.cost_micros, metrics.conversions " +
		"FROM keyword_view WHERE ad_group_criterion.status != 'REMOVED'" +
		andDateClause(args.DateStart, args.DateEnd) +
		" ORDER BY metrics.cost_micros DESC"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return KeywordPerformanceResult{}, toolError("keywords", err)
	}
	rows = enrichCostFields(rows)
	return KeywordPerformanceResult{Keywords: rows, TotalCount: len(rows)}, nil
}

// SearchTermsArgs scopes the search-terms report to a date window (defaults to
// the last 30 days when no explicit range is given).
type SearchTermsArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	DateStart  string `json:"date_start,omitempty" jsonschema:"start date YYYY-MM-DD; defaults to last 30 days if omitted"`
	DateEnd    string `json:"date_end,omitempty" jsonschema:"end date YYYY-MM-DD; defaults to last 30 days if omitted"`
}

type SearchTermsResult struct {
	SearchTerms []json.RawMessage `json:"search_terms"`
	TotalCount  int               `json:"total_count"`
}

func runSearchTerms(ctx context.Context, c *Client, args SearchTermsArgs) (SearchTermsResult, error) {
	if args.CustomerID == "" {
		return SearchTermsResult{}, fmt.Errorf("customer_id is required")
	}
	where := "segments.date DURING LAST_30_DAYS"
	if args.DateStart != "" && args.DateEnd != "" {
		where = dateClause(args.DateStart, args.DateEnd)
	}
	query := "SELECT " +
		"search_term_view.search_term, campaign.name, ad_group.name, " +
		"metrics.impressions, metrics.clicks, metrics.cost_micros, metrics.conversions " +
		"FROM search_term_view WHERE " + where +
		" ORDER BY metrics.clicks DESC LIMIT 200"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return SearchTermsResult{}, toolError("search_terms", err)
	}
	rows = enrichCostFields(rows)
	return SearchTermsResult{SearchTerms: rows, TotalCount: len(rows)}, nil
}

// NegativeKeywordsArgs lists campaign-level negative keywords.
type NegativeKeywordsArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
}

type NegativeKeywordsResult struct {
	NegativeKeywords []json.RawMessage `json:"negative_keywords"`
	TotalCount       int               `json:"total_count"`
}

func runNegativeKeywords(ctx context.Context, c *Client, args NegativeKeywordsArgs) (NegativeKeywordsResult, error) {
	if args.CustomerID == "" {
		return NegativeKeywordsResult{}, fmt.Errorf("customer_id is required")
	}
	query := "SELECT " +
		"campaign.id, campaign.name, campaign_criterion.keyword.text, " +
		"campaign_criterion.keyword.match_type, campaign_criterion.negative, " +
		"campaign_criterion.criterion_id " +
		"FROM campaign_criterion WHERE campaign_criterion.negative = TRUE " +
		"AND campaign_criterion.status != 'REMOVED'"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return NegativeKeywordsResult{}, toolError("negative_keywords", err)
	}
	return NegativeKeywordsResult{NegativeKeywords: rows, TotalCount: len(rows)}, nil
}

// --- CLI front-end ---

var (
	keywordPerfArgs KeywordPerformanceArgs
	searchTermsArgs SearchTermsArgs
	negKeywordsArgs NegativeKeywordsArgs
)

var keywordsCmd = &cobra.Command{
	Use:   "keywords",
	Short: "Inspect keyword performance, search terms, and negatives",
}

var keywordsPerfCmd = &cobra.Command{
	Use:   "performance",
	Short: "Show keyword-level performance metrics",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runKeywordPerformance(cmd.Context(), client, keywordPerfArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var searchTermsCmd = &cobra.Command{
	Use:   "search-terms",
	Short: "Show the search terms that triggered ads",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runSearchTerms(cmd.Context(), client, searchTermsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var negativeKeywordsCmd = &cobra.Command{
	Use:   "negative",
	Short: "List campaign-level negative keywords",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runNegativeKeywords(cmd.Context(), client, negKeywordsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	keywordsPerfCmd.Flags().StringVar(&keywordPerfArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	keywordsPerfCmd.Flags().StringVar(&keywordPerfArgs.DateStart, "date-start", "", "start date YYYY-MM-DD")
	keywordsPerfCmd.Flags().StringVar(&keywordPerfArgs.DateEnd, "date-end", "", "end date YYYY-MM-DD")
	_ = keywordsPerfCmd.MarkFlagRequired("customer-id")

	searchTermsCmd.Flags().StringVar(&searchTermsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	searchTermsCmd.Flags().StringVar(&searchTermsArgs.DateStart, "date-start", "", "start date YYYY-MM-DD")
	searchTermsCmd.Flags().StringVar(&searchTermsArgs.DateEnd, "date-end", "", "end date YYYY-MM-DD")
	_ = searchTermsCmd.MarkFlagRequired("customer-id")

	negativeKeywordsCmd.Flags().StringVar(&negKeywordsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	_ = negativeKeywordsCmd.MarkFlagRequired("customer-id")

	keywordsCmd.AddCommand(keywordsPerfCmd, searchTermsCmd, negativeKeywordsCmd)
}
