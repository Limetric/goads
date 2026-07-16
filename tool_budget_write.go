package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// BudgetSetArgs updates a campaign budget's daily amount. This is a *write*
// tool, so it follows the confirm-token flow: the first call (Confirm == "")
// stages a preview; the second call (Confirm == token) applies it.
type BudgetSetArgs struct {
	CustomerID   string `json:"customer_id" jsonschema:"the Google Ads customer ID that owns the budget"`
	BudgetID     string `json:"budget_id" jsonschema:"the campaign budget ID to update"`
	AmountMicros int64  `json:"amount_micros" jsonschema:"the new daily budget in micros (1 unit of currency = 1,000,000 micros)"`
	Confirm      string `json:"confirm,omitempty" jsonschema:"a confirm token returned by a previous preview call; omit to preview"`
}

func (a BudgetSetArgs) operations() []any {
	resource := fmt.Sprintf("customers/%s/campaignBudgets/%s", normalizeCustomerID(a.CustomerID), a.BudgetID)
	return []any{
		map[string]any{
			"campaignBudgetOperation": map[string]any{
				"update": map[string]any{
					"resourceName": resource,
					"amountMicros": a.AmountMicros,
				},
				"updateMask": "amountMicros",
			},
		},
	}
}

// runBudgetSet stages or applies a campaign-budget update via the shared
// preview/confirm helpers, so partial failures fail the apply (issue #7).
func runBudgetSet(ctx context.Context, c *Client, args BudgetSetArgs) (WriteResult, error) {
	const tool = "set_campaign_budget"
	if args.CustomerID == "" || args.BudgetID == "" {
		return WriteResult{}, fmt.Errorf("customer_id and budget_id are required")
	}
	// Blocked-op check runs before the confirm branch so an operation blocked
	// between preview and confirm cannot still be applied with its token.
	cfg := loadSafetyConfig()
	if err := checkBlockedOperation(tool, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	if args.AmountMicros <= 0 {
		return WriteResult{}, fmt.Errorf("amount_micros must be positive (1 unit of currency = 1,000,000 micros)")
	}
	// Guard: reject daily budgets above the configured cap (default $50/day).
	if err := checkBudgetCap(float64(args.AmountMicros)/1_000_000.0, cfg); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	summary := fmt.Sprintf("Set budget %s to %d micros", args.BudgetID, args.AmountMicros)
	return previewMutate(tool, normalizeCustomerID(args.CustomerID), summary, args.operations())
}

// --- CLI front-end ---

var budgetArgs BudgetSetArgs

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Manage campaign budgets",
}

var budgetSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a campaign budget's daily amount (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runBudgetSet(cmd.Context(), client, budgetArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	budgetSetCmd.Flags().StringVar(&budgetArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	budgetSetCmd.Flags().StringVar(&budgetArgs.BudgetID, "budget-id", "", "campaign budget ID (required)")
	budgetSetCmd.Flags().Int64Var(&budgetArgs.AmountMicros, "amount-micros", 0, "new daily budget in micros")
	budgetSetCmd.Flags().StringVar(&budgetArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = budgetSetCmd.MarkFlagRequired("customer-id")
	_ = budgetSetCmd.MarkFlagRequired("budget-id")
	budgetCmd.AddCommand(budgetSetCmd)
}
