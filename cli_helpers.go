package main

import (
	"context"
	"encoding/json"
	"io"
)

// newClient builds an API client from the resolved configuration (the global
// --config flag plus the environment). Shared by every CLI subcommand and by
// the MCP server.
func newClient(ctx context.Context) (*Client, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}
	return NewClient(ctx, cfg)
}

// printJSON writes v as indented JSON followed by a newline. This is the
// default CLI output so results pipe cleanly into jq.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
