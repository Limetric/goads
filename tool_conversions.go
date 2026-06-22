package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// ConversionsArgs lists the conversion actions configured in an account. Ports
// upstream `tools/conversions.rs`.
type ConversionsArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
}

type ConversionsResult struct {
	ConversionActions []json.RawMessage `json:"conversion_actions"`
	TotalCount        int               `json:"total_count"`
}

func runConversions(ctx context.Context, c *Client, args ConversionsArgs) (ConversionsResult, error) {
	if args.CustomerID == "" {
		return ConversionsResult{}, fmt.Errorf("customer_id is required")
	}
	query := "SELECT " +
		"conversion_action.id, conversion_action.name, conversion_action.type, " +
		"conversion_action.status, conversion_action.category, " +
		"conversion_action.value_settings.default_value, conversion_action.counting_type " +
		"FROM conversion_action WHERE conversion_action.status != 'REMOVED' " +
		"ORDER BY conversion_action.name LIMIT 200"

	rows, err := c.Search(ctx, args.CustomerID, query)
	if err != nil {
		return ConversionsResult{}, toolError("conversions", err)
	}
	return ConversionsResult{ConversionActions: rows, TotalCount: len(rows)}, nil
}

var conversionsArgs ConversionsArgs

var conversionsCmd = &cobra.Command{
	Use:   "conversions",
	Short: "List conversion actions configured in an account",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runConversions(cmd.Context(), client, conversionsArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	conversionsCmd.Flags().StringVar(&conversionsArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	_ = conversionsCmd.MarkFlagRequired("customer-id")
}
