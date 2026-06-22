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
