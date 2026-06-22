package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// This file ports upstream `tools/entity_lifecycle.rs`: pause, enable, and
// remove a campaign, ad group, ad, or keyword. All are writes that preview
// first; remove is destructive and is flagged as such in the preview.

var validEntityTypes = []string{"campaign", "ad_group", "ad", "keyword"}

// entityResourceAndOp maps an entity type to its REST resource path and the
// mutate operation key that targets it.
func entityResourceAndOp(cid, entityType, entityID string) (resource, opKey string, err error) {
	switch entityType {
	case "campaign":
		return fmt.Sprintf("customers/%s/campaigns/%s", cid, entityID), "campaignOperation", nil
	case "ad_group":
		return fmt.Sprintf("customers/%s/adGroups/%s", cid, entityID), "adGroupOperation", nil
	case "ad":
		return fmt.Sprintf("customers/%s/adGroupAds/%s", cid, entityID), "adGroupAdOperation", nil
	case "keyword":
		return fmt.Sprintf("customers/%s/adGroupCriteria/%s", cid, entityID), "adGroupCriterionOperation", nil
	default:
		return "", "", fmt.Errorf("invalid entity type %q: must be one of: %s", entityType, strings.Join(validEntityTypes, ", "))
	}
}

// EntityActionArgs pauses, enables, or removes a single entity.
type EntityActionArgs struct {
	CustomerID string `json:"customer_id" jsonschema:"the Google Ads customer ID that owns the entity"`
	EntityType string `json:"entity_type" jsonschema:"one of: campaign, ad_group, ad, keyword"`
	EntityID   string `json:"entity_id" jsonschema:"the entity ID (for an ad, the composite adGroupId~adId)"`
	Confirm    string `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runPauseEntity(ctx context.Context, c *Client, args EntityActionArgs) (WriteResult, error) {
	return entityStatusChange(ctx, c, args, "pause_entity", string(AdStatusPaused))
}

func runEnableEntity(ctx context.Context, c *Client, args EntityActionArgs) (WriteResult, error) {
	return entityStatusChange(ctx, c, args, "enable_entity", string(AdStatusEnabled))
}

// entityStatusChange stages or applies a status update on an entity.
func entityStatusChange(ctx context.Context, c *Client, args EntityActionArgs, tool, status string) (WriteResult, error) {
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.EntityID == "" {
		return WriteResult{}, fmt.Errorf("entity_id is required")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	resource, opKey, err := entityResourceAndOp(cid, args.EntityType, args.EntityID)
	if err != nil {
		return WriteResult{}, err
	}
	op := map[string]any{
		opKey: map[string]any{
			"update":     map[string]any{"resourceName": resource, "status": status},
			"updateMask": "status",
		},
	}
	summary := fmt.Sprintf("Set %s %s status to %s", args.EntityType, args.EntityID, status)
	return previewMutate(tool, cid, summary, []any{op})
}

func runRemoveEntity(ctx context.Context, c *Client, args EntityActionArgs) (WriteResult, error) {
	const tool = "remove_entity"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.EntityID == "" {
		return WriteResult{}, fmt.Errorf("entity_id is required")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	cid := normalizeCustomerID(args.CustomerID)
	resource, opKey, err := entityResourceAndOp(cid, args.EntityType, args.EntityID)
	if err != nil {
		return WriteResult{}, err
	}
	op := map[string]any{opKey: map[string]any{"remove": resource}}
	summary := fmt.Sprintf("REMOVE %s %s — destructive and cannot be undone", args.EntityType, args.EntityID)
	return previewMutate(tool, cid, summary, []any{op})
}

// --- CLI front-end ---

var (
	pauseArgs  EntityActionArgs
	enableArgs EntityActionArgs
	removeArgs EntityActionArgs
)

func entityCmd(use, short string, args *EntityActionArgs, run func(context.Context, *Client, EntityActionArgs) (WriteResult, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(cmd.Context())
			if err != nil {
				return err
			}
			res, err := run(cmd.Context(), client, *args)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().StringVar(&args.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	cmd.Flags().StringVar(&args.EntityType, "type", "", "entity type: campaign, ad_group, ad, or keyword (required)")
	cmd.Flags().StringVar(&args.EntityID, "id", "", "entity ID (required)")
	cmd.Flags().StringVar(&args.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = cmd.MarkFlagRequired("customer-id")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

var (
	pauseCmd  = entityCmd("pause", "Pause a campaign, ad group, ad, or keyword", &pauseArgs, runPauseEntity)
	enableCmd = entityCmd("enable", "Enable a campaign, ad group, ad, or keyword", &enableArgs, runEnableEntity)
	removeCmd = entityCmd("remove", "Remove a campaign, ad group, ad, or keyword (destructive)", &removeArgs, runRemoveEntity)
)
