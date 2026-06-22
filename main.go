// Command goads is a Google Ads MCP server and CLI.
//
// It exposes the same set of Google Ads tools two ways: as a conventional CLI
// (`goads search …`) and as an MCP server over stdio (`goads mcp`). Both share
// one handler per tool; see tool_*.go.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// configPath is the optional --config flag (a TOML credentials/settings file).
// When empty, configuration comes from the environment and the default path.
var configPath string

var rootCmd = &cobra.Command{
	Use:           "goads",
	Short:         "Google Ads campaign management — CLI and MCP server",
	Long:          "goads exposes Google Ads tools as both a CLI and an MCP server (`goads mcp`).\n\nCredentials are read from the environment (GOOGLE_ADS_*) or a TOML config file.\nRun `goads doctor` to check your setup.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

var versionVerbose bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print goads version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		out := cmd.OutOrStdout()
		if versionVerbose {
			fmt.Fprintln(out, versionVerboseString())
			return
		}
		fmt.Fprintln(out, versionString())
	},
}

func init() {
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to TOML credentials/settings file (env overrides)")

	versionCmd.Flags().BoolVarP(&versionVerbose, "verbose", "v", false, "print detailed build metadata")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(doctorCmd)

	// Tool subcommands. Each tool_*.go registers its CLI command here and its
	// MCP tool in registerTools (mcp.go) — keep the two in sync.
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(accountsCmd)
	rootCmd.AddCommand(budgetCmd)
	rootCmd.AddCommand(campaignsCmd)
	rootCmd.AddCommand(adsCmd)
	rootCmd.AddCommand(keywordsCmd)
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(geoCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
