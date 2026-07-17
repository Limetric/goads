// Command goads is a Google Ads MCP server and CLI.
//
// It exposes the same set of Google Ads tools two ways: as a conventional CLI
// (`goads search …`) and as an MCP server over stdio (`goads mcp`). Both share
// one handler per tool; see tool_*.go.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// exitErr lets a command request a specific process exit code. When a command
// returns one, it has already printed its own diagnostics, so main() exits with
// the requested code without printing the generic "error:" line.
type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string { return e.err.Error() }
func (e *exitErr) Unwrap() error { return e.err }

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
	doctorCmd.Flags().BoolVar(&doctorOffline, "offline", false, "skip the live API check; only verify that credentials resolve")

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(mcpCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(confirmCmd)
	rootCmd.AddCommand(auditCmd)

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
	rootCmd.AddCommand(conversionsCmd)
	rootCmd.AddCommand(policyCmd)
	rootCmd.AddCommand(extensionsCmd)
	rootCmd.AddCommand(keywordIdeasCmd)
	rootCmd.AddCommand(keywordForecastsCmd)
	rootCmd.AddCommand(recommendationsCmd)
	rootCmd.AddCommand(assetCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(enableCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(scheduleCmd)
	rootCmd.AddCommand(biddingCmd)
	rootCmd.AddCommand(audienceCmd)
	rootCmd.AddCommand(adGroupCmd)
	rootCmd.AddCommand(adCmd)
	rootCmd.AddCommand(extensionCmd)
	rootCmd.AddCommand(pmaxCmd)
	rootCmd.AddCommand(campaignCmd)
}

func main() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}
	// A command that carries its own exit code has already reported details;
	// just exit with that code (e.g. doctor's inconclusive vs failed).
	var ex *exitErr
	if errors.As(err, &ex) {
		os.Exit(ex.code)
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
