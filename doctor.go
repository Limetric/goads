package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// doctorCmd reports whether credentials resolve, without making an API call.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that credentials and configuration resolve",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := loadConfig(configPath)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "base URL:           %s\n", cfg.BaseURL)
		fmt.Fprintf(out, "developer token:    %s\n", present(cfg.DeveloperToken))
		fmt.Fprintf(out, "client id:          %s\n", present(cfg.ClientID))
		fmt.Fprintf(out, "client secret:      %s\n", present(cfg.ClientSecret))
		fmt.Fprintf(out, "refresh token:      %s\n", present(cfg.RefreshToken))
		fmt.Fprintf(out, "login customer id:  %s\n", orNone(cfg.LoginCustomerID))
		if err := cfg.validate(); err != nil {
			fmt.Fprintf(out, "\nstatus: NOT READY — %v\n", err)
			return err
		}
		fmt.Fprintf(out, "\nstatus: ready\n")
		return nil
	},
}

func present(s string) string {
	if s == "" {
		return "MISSING"
	}
	return "set"
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
