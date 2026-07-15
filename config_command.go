package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect configuration resolution",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config path selected by goads",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		resolved, err := resolveConfigPath(configPath)
		if err != nil {
			return fmt.Errorf("resolve config path: %w", err)
		}
		if resolved == "" {
			fmt.Fprintln(cmd.OutOrStdout(), "environment only (no config file)")
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), resolved)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configPathCmd)
}
