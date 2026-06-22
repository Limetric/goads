package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// This file ports upstream `tools/assets.rs`: uploading reusable image and text
// assets. Both are writes that preview first and apply on confirm.

// AssetImageArgs uploads a base64-encoded image asset.
type AssetImageArgs struct {
	CustomerID      string `json:"customer_id" jsonschema:"the Google Ads customer ID that will own the asset"`
	AssetName       string `json:"asset_name" jsonschema:"a name for the asset"`
	ImageDataBase64 string `json:"image_data_base64" jsonschema:"the base64-encoded image bytes"`
	Confirm         string `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runUploadImageAsset(ctx context.Context, c *Client, args AssetImageArgs) (WriteResult, error) {
	const tool = "upload_image_asset"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.AssetName == "" {
		return WriteResult{}, fmt.Errorf("asset_name cannot be empty")
	}
	if args.ImageDataBase64 == "" {
		return WriteResult{}, fmt.Errorf("image_data_base64 cannot be empty")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	op := map[string]any{
		"assetOperation": map[string]any{
			"create": map[string]any{
				"name":       args.AssetName,
				"type":       "IMAGE",
				"imageAsset": map[string]any{"data": args.ImageDataBase64},
			},
		},
	}
	summary := fmt.Sprintf("Upload image asset %q (%d base64 bytes)", args.AssetName, len(args.ImageDataBase64))
	return previewMutate(tool, normalizeCustomerID(args.CustomerID), summary, []any{op})
}

// AssetTextArgs uploads a reusable text asset.
type AssetTextArgs struct {
	CustomerID  string `json:"customer_id" jsonschema:"the Google Ads customer ID that will own the asset"`
	AssetName   string `json:"asset_name" jsonschema:"a name for the asset"`
	TextContent string `json:"text_content" jsonschema:"the text content of the asset"`
	Confirm     string `json:"confirm,omitempty" jsonschema:"a confirm token from a previous preview; omit to preview"`
}

func runUploadTextAsset(ctx context.Context, c *Client, args AssetTextArgs) (WriteResult, error) {
	const tool = "upload_text_asset"
	if err := checkBlockedOperation(tool, loadSafetyConfig()); err != nil {
		return WriteResult{}, toolError(tool, err)
	}
	if args.AssetName == "" {
		return WriteResult{}, fmt.Errorf("asset_name cannot be empty")
	}
	if args.TextContent == "" {
		return WriteResult{}, fmt.Errorf("text_content cannot be empty")
	}
	if args.Confirm != "" {
		return applyConfirmed(ctx, c, tool, args.Confirm)
	}
	op := map[string]any{
		"assetOperation": map[string]any{
			"create": map[string]any{
				"name":      args.AssetName,
				"textAsset": map[string]any{"text": args.TextContent},
			},
		},
	}
	summary := fmt.Sprintf("Upload text asset %q", args.AssetName)
	return previewMutate(tool, normalizeCustomerID(args.CustomerID), summary, []any{op})
}

// --- CLI front-end ---

var (
	assetImageArgs AssetImageArgs
	assetTextArgs  AssetTextArgs
)

var assetCmd = &cobra.Command{
	Use:   "asset",
	Short: "Upload reusable image and text assets",
}

var assetImageCmd = &cobra.Command{
	Use:   "image",
	Short: "Upload a base64-encoded image asset (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runUploadImageAsset(cmd.Context(), client, assetImageArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

var assetTextCmd = &cobra.Command{
	Use:   "text",
	Short: "Upload a reusable text asset (previews first; --confirm to apply)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newClient(cmd.Context())
		if err != nil {
			return err
		}
		res, err := runUploadTextAsset(cmd.Context(), client, assetTextArgs)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), res)
	},
}

func init() {
	assetImageCmd.Flags().StringVar(&assetImageArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	assetImageCmd.Flags().StringVar(&assetImageArgs.AssetName, "name", "", "asset name (required)")
	assetImageCmd.Flags().StringVar(&assetImageArgs.ImageDataBase64, "image-base64", "", "base64-encoded image data (required)")
	assetImageCmd.Flags().StringVar(&assetImageArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = assetImageCmd.MarkFlagRequired("customer-id")
	_ = assetImageCmd.MarkFlagRequired("name")
	_ = assetImageCmd.MarkFlagRequired("image-base64")

	assetTextCmd.Flags().StringVar(&assetTextArgs.CustomerID, "customer-id", "", "Google Ads customer ID (required)")
	assetTextCmd.Flags().StringVar(&assetTextArgs.AssetName, "name", "", "asset name (required)")
	assetTextCmd.Flags().StringVar(&assetTextArgs.TextContent, "text", "", "text content (required)")
	assetTextCmd.Flags().StringVar(&assetTextArgs.Confirm, "confirm", "", "confirm token from a previous preview")
	_ = assetTextCmd.MarkFlagRequired("customer-id")
	_ = assetTextCmd.MarkFlagRequired("name")
	_ = assetTextCmd.MarkFlagRequired("text")

	assetCmd.AddCommand(assetImageCmd, assetTextCmd)
}
