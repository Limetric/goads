package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// PolicyArgs lists ads with policy issues (disapproved, limited, under review).
// Ports upstream `tools/policy.rs`.
type PolicyArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
}

type PolicyResult struct {
	PolicyIssues []json.RawMessage `json:"policy_issues"`
	TotalCount   int               `json:"total_count"`
}

func runPolicy(ctx context.Context, c *Client, args PolicyArgs) (PolicyResult, error) {
	if args.CustomerID == "" {
		return PolicyResult{}, fmt.Errorf("customer_id is required")
	}
	query := "SELECT " +
		"ad_group_ad.ad.id, ad_group_ad.ad.name, " +
		"ad_group_ad.policy_summary.approval_status, ad_group_ad.policy_summary.review_status, " +
		"ad_group_ad.policy_summary.policy_topic_entries, campaign.name, ad_group.name " +
		"FROM ad_group_ad WHERE ad_group_ad.policy_summary.approval_status != 'APPROVED' LIMIT 200"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return PolicyResult{}, toolError("policy", err)
	}
	return PolicyResult{PolicyIssues: rows, TotalCount: len(rows)}, nil
}

var policyArgs PolicyArgs

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "List ads with policy issues (disapproved, limited, under review)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runPolicy(cmd.Context(), client, policyArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	policyCmd.Flags().StringVar(&policyArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	_ = policyCmd.MarkFlagRequired("customer-id")
}
