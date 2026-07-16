package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

// doctorOffline backs the `--offline` flag: by default doctor makes real API
// calls to verify the setup can query; --offline skips them and only checks
// that credentials resolve (fast, deterministic, no network — for CI/offline).
var doctorOffline bool

// doctorCmd reports whether the setup works. By default it probes the API so
// "ready" means real queries succeed, not just that the credential strings are
// present. --offline reduces it to the cheap config-only check.
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that the Google Ads setup works (config + live API check)",
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

		if doctorOffline {
			fmt.Fprintf(out, "\nstatus: ready — credentials resolve (offline check). Run `goads doctor` to verify against the API.\n")
			return nil
		}

		fmt.Fprintln(out)
		res, liveErr := runDoctorLive(cmd.Context(), out, cfg)
		switch res {
		case liveOK:
			fmt.Fprintf(out, "\nstatus: ready (live check passed)\n")
			return nil
		case liveInconclusive:
			fmt.Fprintf(out, "\nstatus: INCONCLUSIVE — credentials resolve, but the API couldn't be reached (network/transient). Setup unconfirmed, not necessarily broken.\n")
			return &exitErr{code: 2, err: liveErr}
		default: // liveFailed
			fmt.Fprintf(out, "\nstatus: NOT READY — the API rejected the request (see above)\n")
			return &exitErr{code: 1, err: liveErr}
		}
	},
}

// liveResult is the outcome of doctor's live check.
type liveResult int

const (
	liveOK           liveResult = iota // the API answered and real queries work
	liveInconclusive                   // couldn't reach the API (transport/5xx) — setup unconfirmed, not broken
	liveFailed                         // the API definitively rejected us (4xx) — setup is broken
)

// liveVerdictFor classifies a live-probe error. A 4xx from the Ads API is
// definitive — the request or credentials are wrong (liveFailed). So is a 4xx
// from the OAuth token endpoint (oauth2.RetrieveError): invalid_grant means
// the refresh token is revoked or mistyped, which used to be misreported as
// "inconclusive — not necessarily broken" (issue #11). Anything else — a 5xx,
// a connection failure — means we simply couldn't get a verdict
// (liveInconclusive), which must not be reported as a broken setup.
func liveVerdictFor(err error) liveResult {
	if err == nil {
		return liveOK
	}
	var apiErr *apiStatusError
	if errors.As(err, &apiErr) && apiErr.isClientError() {
		return liveFailed
	}
	var oauthErr *oauth2.RetrieveError
	if errors.As(err, &oauthErr) && oauthErr.Response != nil &&
		oauthErr.Response.StatusCode >= 400 && oauthErr.Response.StatusCode < 500 {
		return liveFailed
	}
	return liveInconclusive
}

// runDoctorLive makes real API calls to prove the setup works, printing a line
// per probe (✓ ok, ✗ definitive failure, ? inconclusive). It runs two probes
// because they fail independently:
//
//  1. listAccessibleCustomers — needs only OAuth + developer token, so it
//     confirms credentials are valid and lists reachable accounts. A test-level
//     developer token still passes this.
//  2. a real customer_client search on the login customer — what every read
//     command does. Unlike probe 1 it fails when the developer token is only
//     approved for test accounts (DEVELOPER_TOKEN_NOT_APPROVED), the exact gap
//     that made plain `doctor` say "ready" for a setup that can't query.
//
// It returns the verdict of the first probe that doesn't pass, so the caller can
// set the status line and exit code.
func runDoctorLive(ctx context.Context, out io.Writer, cfg *Config) (liveResult, error) {
	client, err := NewClient(ctx, cfg)
	if err != nil {
		return reportProbe(out, "client:              ", err), err
	}

	ids, err := client.ListAccessibleCustomers(ctx)
	if err != nil {
		return reportProbe(out, "accessible accounts: ", err), err
	}
	dashed := make([]string, len(ids))
	for i, id := range ids {
		dashed[i] = dashCustomerID(id)
	}
	fmt.Fprintf(out, "accessible accounts:  ✓ %d (%s)\n", len(ids), strings.Join(dashed, ", "))

	if cfg.LoginCustomerID == "" {
		fmt.Fprintf(out, "live query:           skipped (no login_customer_id set)\n")
		return liveOK, nil
	}
	res, err := runAccounts(ctx, client, AccountsArgs{})
	if err != nil {
		return reportProbe(out, "live query:          ", err), err
	}
	fmt.Fprintf(out, "live query:           ✓ %d account(s) reachable under %s\n", len(res.CustomerIDs), dashCustomerID(cfg.LoginCustomerID))
	return liveOK, nil
}

// reportProbe prints a failed probe line — ✗ for a definitive failure, ? for an
// inconclusive one — and returns the classification. label should be padded to
// align with the ✓ lines (a trailing space follows the marker).
func reportProbe(out io.Writer, label string, err error) liveResult {
	verdict := liveVerdictFor(err)
	marker := "?"
	prefix := "could not reach the API: "
	if verdict == liveFailed {
		marker = "✗"
		prefix = ""
	}
	fmt.Fprintf(out, "%s %s %s%v\n", label, marker, prefix, err)
	return verdict
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
