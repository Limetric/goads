package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// SearchArgs is the input for the `search` tool. Struct tags drive both the CLI
// flags and (via reflection) the MCP JSON schema, so descriptions live here.
type SearchArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID to query (dashes optional)"`
	Query      string `json:"query" jsonschema:"the GAQL query to run, e.g. SELECT campaign.id, campaign.name FROM campaign"`
}

// SearchResult is the structured output for the `search` tool.
type SearchResult struct {
	CustomerID string            `json:"customer_id"`
	RowCount   int               `json:"row_count"`
	Rows       []json.RawMessage `json:"rows"`
}

// runSearch is the shared handler used by both the CLI and the MCP server.
func runSearch(ctx context.Context, c *Client, args SearchArgs) (SearchResult, error) {
	if args.CustomerID == "" {
		return SearchResult{}, fmt.Errorf("customer_id is required")
	}
	if err := validateGAQL(args.Query); err != nil {
		return SearchResult{}, err
	}
	rows, err := c.Search(ctx, args.CustomerID, args.Query)
	if err != nil {
		return SearchResult{}, toolError("search", err)
	}
	return SearchResult{
		CustomerID: normalizeCustomerID(args.CustomerID),
		RowCount:   len(rows),
		Rows:       rows,
	}, nil
}

// --- CLI front-end ---

var searchArgs SearchArgs

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Run a GAQL query and print the result rows as JSON",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runSearch(cmd.Context(), client, searchArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	searchCmd.Flags().StringVar(&searchArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	searchCmd.Flags().StringVar(&searchArgs.Query, "query", "", "GAQL query (required)")
	_ = searchCmd.MarkFlagRequired("customer-id")
	_ = searchCmd.MarkFlagRequired("query")
}
