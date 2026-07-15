package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpSession wires an in-memory MCP client to a server with all tools
// registered, exercising the real registration + schema + addTool adapter path
// without stdio. The client is used by the tool handlers, so point it at a fake
// API when a CallTool will actually run a handler.
func mcpSession(t *testing.T, client *Client) *mcp.ClientSession {
	t.Helper()
	serverT, clientT := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{Name: "goads", Version: "test"}, nil)
	registerTools(server, client)

	ctx := context.Background()
	ss, err := server.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	mc := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := mc.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Close()
	})
	return cs
}

func TestMCP_RegistrationIntegrity(t *testing.T) {
	// ListTools never invokes a handler, so a stub client is fine.
	cs := mcpSession(t, &Client{cfg: &Config{}})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := map[string]bool{}
	for _, tool := range res.Tools {
		if names[tool.Name] {
			t.Errorf("duplicate MCP tool registered: %q", tool.Name)
		}
		names[tool.Name] = true
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
		}
	}
	// A representative slice across reads, writes, and the dedicated RPCs must
	// all be reachable over MCP (catches a handler dropped from registerTools).
	for _, want := range []string{
		"search", "report", "list_accounts", "campaigns", "ads", "keyword_performance",
		"search_terms", "negative_keywords", "geo_targets", "geo_performance",
		"conversions", "policy", "extensions", "keyword_ideas", "keyword_forecasts",
		"list_recommendations", "set_campaign_budget", "draft_campaign", "update_campaign",
		"create_ad_group", "draft_responsive_search_ad", "draft_keywords",
		"create_portfolio_bidding_strategy", "create_custom_audience", "upload_image_asset",
		"set_campaign_schedule", "create_pmax_campaign", "create_app_campaign", "pause_entity", "enable_entity",
		"remove_entity", "apply_recommendation", "dismiss_recommendation",
	} {
		if !names[want] {
			t.Errorf("MCP tool %q is not registered", want)
		}
	}
	if len(names) != 47 {
		t.Errorf("expected 47 registered MCP tools, got %d (update this count when adding/removing a tool)", len(names))
	}
}

func TestMCP_CallSearchTool(t *testing.T) {
	srv := gaqlServer(t, `[{"campaign":{"id":"1"}}]`, "FROM campaign")
	defer srv.Close()
	cs := mcpSession(t, newTestClient(t, srv))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"customer_id": "123-456-7890", "query": "SELECT campaign.id FROM campaign"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("search tool returned an error result: %+v", res.Content)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	out := strings.ReplaceAll(text.String(), " ", "")
	if !strings.Contains(out, `"row_count":1`) {
		t.Errorf("tool output missing row_count: %s", text.String())
	}
	// Structured content must also be carried (it is populated manually because
	// the output type is `any` to bypass schema validation of RawMessage fields).
	if res.StructuredContent == nil {
		t.Error("expected structured content to be populated")
	}
}

// TestMCP_CallReadToolWithRawRows guards the fix for output-schema validation:
// read tools whose Result has []json.RawMessage fields (rows/objects) must call
// cleanly over MCP. campaigns also exercises cost enrichment through the SDK.
func TestMCP_CallReadToolWithRawRows(t *testing.T) {
	srv := gaqlServer(t, `[{"campaign":{"id":"1"},"metrics":{"cost_micros":"5000000","conversions":2}}]`, "FROM campaign")
	defer srv.Close()
	cs := mcpSession(t, newTestClient(t, srv))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "campaigns",
		Arguments: map[string]any{"customer_id": "123-456-7890"},
	})
	if err != nil {
		t.Fatalf("CallTool campaigns: %v", err)
	}
	if res.IsError {
		t.Fatalf("campaigns tool returned an error result: %+v", res.Content)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if !strings.Contains(text.String(), "cost_readable") || !strings.Contains(text.String(), "cpa") {
		t.Errorf("campaigns output missing enriched fields:\n%s", text.String())
	}
}

func TestMCP_CallToolSurfacesHandlerError(t *testing.T) {
	cs := mcpSession(t, &Client{cfg: &Config{}})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"customer_id": "", "query": "SELECT campaign.id FROM campaign"},
	})
	// A handler error is reported as an error result (IsError), not a transport error.
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("expected an error result for empty customer_id; err=%v res=%+v", err, res)
	}
}

func TestToolError(t *testing.T) {
	inner := fmt.Errorf("boom")
	err := toolError("mytool", inner)
	if err.Error() != "mytool: boom" {
		t.Errorf("toolError = %q, want %q", err.Error(), "mytool: boom")
	}
	if !errors.Is(err, inner) {
		t.Error("toolError should wrap the underlying error with %w")
	}
}
