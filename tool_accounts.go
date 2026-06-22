package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"
)

// AccountsArgs has no inputs today, but the struct is the schema anchor and the
// place to add filters (e.g. by manager account) later.
type AccountsArgs struct{}

// AccountsResult lists customer accounts reachable under the login customer.
type AccountsResult struct {
	CustomerIDs []string `json:"customer_ids"`
}

// runAccounts lists accessible customers. It uses the login-customer-id as the
// query root and a GAQL query over customer_client (the REST API has no
// dedicated listAccessibleCustomers verb in this client; search covers it).
func runAccounts(ctx context.Context, c *Client, _ AccountsArgs) (AccountsResult, error) {
	root := c.cfg.LoginCustomerID
	q, err := buildSelect(
		[]string{"customer_client.id", "customer_client.descriptive_name"},
		"customer_client", "customer_client.status = 'ENABLED'", 0,
	)
	if err != nil {
		return AccountsResult{}, err
	}
	rows, err := c.Search(ctx, root, q)
	if err != nil {
		return AccountsResult{}, toolError("list_accounts", err)
	}
	var out AccountsResult
	for _, r := range rows {
		var row struct {
			CustomerClient struct {
				ID string `json:"id"`
			} `json:"customerClient"`
		}
		if json.Unmarshal(r, &row) == nil && row.CustomerClient.ID != "" {
			out.CustomerIDs = append(out.CustomerIDs, row.CustomerClient.ID)
		}
	}
	return out, nil
}

// --- CLI front-end ---

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "List accessible Google Ads accounts",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runAccounts(cmd.Context(), client, AccountsArgs{})
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}
