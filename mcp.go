package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// mcpCmd starts the MCP server over stdio. It exposes the same tools as the CLI
// subcommands, backed by the same handlers.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run the Google Ads MCP server over stdio",
	Long:  "Serve Google Ads tools to an MCP host (Claude Desktop, Cursor, …) over stdio.\n\nConfigure your host to run `goads mcp` and pass credentials via the environment.",
	Args:  cobra.NoArgs,
	RunE:  runMCP,
}

func runMCP(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	client, err := newClient(ctx)
	if err != nil {
		return err
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "goads", Version: versionString()}, nil)
	registerTools(server, client)

	// Run blocks until stdin closes or the context is cancelled.
	return server.Run(ctx, mcp.NewStdioTransport())
}

// registerTools wires every ported tool into the MCP server. Each tool's input
// schema is derived by reflection from its Args struct, so the struct tags in
// tool_*.go are the single source of truth for the schema.
//
// Keep this list in sync with the CLI subcommands registered in main.go's init.
func registerTools(server *mcp.Server, client *Client) {
	addTool(server, client, "search",
		"Run a GAQL query against a Google Ads account and return the rows.",
		runSearch)

	addTool(server, client, "list_accounts",
		"List the Google Ads accounts accessible to the authenticated user.",
		runAccounts)

	addTool(server, client, "set_campaign_budget",
		"Update a campaign's daily budget. Returns a preview + confirm token; pass Confirm to apply.",
		runBudgetSet)

	addTool(server, client, "campaigns",
		"Show campaign-level performance metrics (cost, clicks, conversions, CTR, CPA) for non-removed campaigns.",
		runCampaigns)

	addTool(server, client, "ads",
		"Show ad-level performance metrics for non-removed ads, ordered by cost.",
		runAds)

	addTool(server, client, "keyword_performance",
		"Show keyword-level performance metrics (impressions, clicks, CTR, CPC, cost, conversions, quality score).",
		runKeywordPerformance)

	addTool(server, client, "search_terms",
		"Show the actual search terms that triggered ads (defaults to the last 30 days).",
		runSearchTerms)

	addTool(server, client, "negative_keywords",
		"List campaign-level negative keywords.",
		runNegativeKeywords)

	addTool(server, client, "report",
		"Run an arbitrary GAQL query and return results as json (default), table, or csv.",
		runReport)

	addTool(server, client, "geo_targets",
		"Search geo target constants by name to find location IDs for geo-targeting.",
		runGeoTargets)

	addTool(server, client, "geo_performance",
		"Show geographic performance for campaigns (defaults to the last 30 days).",
		runGeoPerformance)

	addTool(server, client, "conversions",
		"List all conversion actions configured in the account.",
		runConversions)

	addTool(server, client, "policy",
		"List ads with policy issues (disapproved, limited, under review).",
		runPolicy)

	addTool(server, client, "extensions",
		"List campaign-level extensions (sitelinks, callouts, structured snippets).",
		runExtensions)

	addTool(server, client, "keyword_ideas",
		"Discover keyword ideas from seed keywords using the Keyword Planner.",
		runDiscoverKeywords)

	addTool(server, client, "keyword_forecasts",
		"Get recent historical performance metrics for specific keywords.",
		runKeywordForecasts)

	addTool(server, client, "list_recommendations",
		"List active (non-dismissed) recommendations for the account.",
		runListRecommendations)

	addTool(server, client, "apply_recommendation",
		"Apply a recommendation. Returns a preview + confirm token; pass Confirm to apply.",
		runApplyRecommendation)

	addTool(server, client, "dismiss_recommendation",
		"Dismiss a recommendation. Returns a preview + confirm token; pass Confirm to apply.",
		runDismissRecommendation)

	addTool(server, client, "upload_image_asset",
		"Upload a base64-encoded image asset. Returns a preview + confirm token; pass Confirm to apply.",
		runUploadImageAsset)

	addTool(server, client, "upload_text_asset",
		"Upload a reusable text asset. Returns a preview + confirm token; pass Confirm to apply.",
		runUploadTextAsset)

	addTool(server, client, "pause_entity",
		"Pause a campaign, ad group, ad, or keyword. Returns a preview + confirm token; pass Confirm to apply.",
		runPauseEntity)

	addTool(server, client, "enable_entity",
		"Enable a campaign, ad group, ad, or keyword. Returns a preview + confirm token; pass Confirm to apply.",
		runEnableEntity)

	addTool(server, client, "remove_entity",
		"Remove a campaign, ad group, ad, or keyword (destructive). Returns a preview + confirm token; pass Confirm to apply.",
		runRemoveEntity)

	addTool(server, client, "set_campaign_schedule",
		"Set campaign ad schedules (day-of-week time windows). Returns a preview + confirm token; pass Confirm to apply.",
		runSetCampaignSchedule)

	addTool(server, client, "create_portfolio_bidding_strategy",
		"Create a portfolio bidding strategy (TARGET_CPA/ROAS/IMPRESSION_SHARE). Returns a preview + confirm token; pass Confirm to apply.",
		runCreatePortfolioBidding)

	addTool(server, client, "update_keyword_bid",
		"Update a keyword's CPC bid (enforces the bid-increase guard). Returns a preview + confirm token; pass Confirm to apply.",
		runUpdateKeywordBid)

	addTool(server, client, "create_custom_audience",
		"Create a custom audience from URL patterns or rules. Returns a preview + confirm token; pass Confirm to apply.",
		runCreateCustomAudience)

	addTool(server, client, "add_audience_targeting",
		"Attach audience targeting to a campaign (TARGETING/OBSERVATION). Returns a preview + confirm token; pass Confirm to apply.",
		runAddAudienceTargeting)

	addTool(server, client, "create_ad_group",
		"Create an ad group in a campaign (defaults to PAUSED). Returns a preview + confirm token; pass Confirm to apply.",
		runCreateAdGroup)

	addTool(server, client, "update_ad_group",
		"Update an ad group's name, CPC bid, and/or ad rotation mode. Returns a preview + confirm token; pass Confirm to apply.",
		runUpdateAdGroup)
}

// addTool adapts a shared handler func(ctx, *Client, A) (R, error) into an MCP
// tool, marshaling the result into both structured output and a text block. The
// input schema for A is derived by the SDK via reflection over its struct tags.
func addTool[A any, R any](server *mcp.Server, client *Client, name, desc string, handler func(context.Context, *Client, A) (R, error)) {
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: desc},
		func(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[A]) (*mcp.CallToolResultFor[R], error) {
			result, err := handler(ctx, client, params.Arguments)
			if err != nil {
				return nil, err
			}
			text, _ := json.MarshalIndent(result, "", "  ")
			return &mcp.CallToolResultFor[R]{
				Content:           []mcp.Content{&mcp.TextContent{Text: string(text)}},
				StructuredContent: result,
			}, nil
		})
}

// toolError is a small helper for handlers to produce consistent messages.
func toolError(tool string, err error) error {
	return fmt.Errorf("%s: %w", tool, err)
}
