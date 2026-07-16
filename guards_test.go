package main

import (
	"math"
	"strings"
	"testing"
)

func budgetCfg(max float64) SafetyConfig {
	return SafetyConfig{MaxDailyBudget: max, MaxBidIncreasePct: 100}
}
func bidCfg(pct float64) SafetyConfig {
	return SafetyConfig{MaxDailyBudget: 50, MaxBidIncreasePct: pct}
}
func blockedCfg(ops ...string) SafetyConfig {
	return SafetyConfig{MaxDailyBudget: 50, MaxBidIncreasePct: 100, BlockedOperations: ops}
}
func f64(v float64) *float64 { return &v }

func TestCheckBudgetCap(t *testing.T) {
	cfg := budgetCfg(50)
	for _, b := range []float64{49.99, 50.0, 0.0, -1.0} {
		if err := checkBudgetCap(b, cfg); err != nil {
			t.Errorf("checkBudgetCap(%.2f) unexpected error: %v", b, err)
		}
	}
	if err := checkBudgetCap(51.0, cfg); err == nil {
		t.Error("checkBudgetCap(51) should exceed cap of 50")
	}
}

func TestCheckBidIncrease(t *testing.T) {
	cfg := bidCfg(100)
	if err := checkBidIncrease(1.0, 2.0, cfg); err != nil {
		t.Errorf("100%% increase within cap should pass: %v", err)
	}
	if err := checkBidIncrease(1.0, 2.5, cfg); err == nil {
		t.Error("150%% increase should exceed 100%% cap")
	}
	// No baseline / decreases / negative current bid are always allowed.
	for _, tc := range [][2]float64{{0.0, 999.0}, {2.0, 1.0}, {-1.0, 1.0}} {
		if err := checkBidIncrease(tc[0], tc[1], cfg); err != nil {
			t.Errorf("checkBidIncrease(%.1f,%.1f) unexpected error: %v", tc[0], tc[1], err)
		}
	}
}

func TestCheckBlockedOperation(t *testing.T) {
	cfg := blockedCfg("delete_campaign")
	if err := checkBlockedOperation("create_campaign", cfg); err != nil {
		t.Errorf("unblocked op should pass: %v", err)
	}
	if err := checkBlockedOperation("delete_campaign", cfg); err == nil {
		t.Error("blocked op should be rejected")
	}
	if err := checkBlockedOperation("anything", blockedCfg()); err != nil {
		t.Errorf("empty block list should allow all: %v", err)
	}
}

func TestCheckBroadManualCPC(t *testing.T) {
	if err := checkBroadManualCPC("BROAD", "MANUAL_CPC"); err == nil {
		t.Error("BROAD + MANUAL_CPC must be blocked")
	}
	if err := checkBroadManualCPC("BROAD", "MAXIMIZE_CONVERSIONS"); err != nil {
		t.Errorf("BROAD + smart bidding should pass: %v", err)
	}
	if err := checkBroadManualCPC("EXACT", "MANUAL_CPC"); err != nil {
		t.Errorf("EXACT + MANUAL_CPC should pass: %v", err)
	}
}

func TestRequiresDoubleConfirmation(t *testing.T) {
	if !requiresDoubleConfirmation("delete_campaign", nil, nil) {
		t.Error("delete should require double confirm")
	}
	if !requiresDoubleConfirmation("remove_entity", nil, nil) {
		t.Error("remove should require double confirm")
	}
	if requiresDoubleConfirmation("pause_entity", nil, nil) {
		t.Error("pause should not require double confirm")
	}
	if !requiresDoubleConfirmation("update_budget", f64(10), f64(20)) {
		t.Error("100%% budget increase should require double confirm")
	}
	if requiresDoubleConfirmation("update_budget", f64(10), f64(14)) {
		t.Error("40%% budget increase should not require double confirm")
	}
	if requiresDoubleConfirmation("update_budget", f64(0), f64(100)) {
		t.Error("zero current budget should not require double confirm")
	}
}

func TestCharLimitValidators(t *testing.T) {
	tests := []struct {
		name  string
		fn    func(string) error
		limit int
	}{
		{"headline", validateHeadline, 30},
		{"description", validateDescription, 90},
		{"sitelink text", validateSitelinkText, 25},
		{"sitelink description", validateSitelinkDescription, 35},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(strings.Repeat("A", tt.limit)); err != nil {
				t.Errorf("exactly %d chars should pass: %v", tt.limit, err)
			}
			if err := tt.fn(strings.Repeat("A", tt.limit+1)); err == nil {
				t.Errorf("%d chars should fail", tt.limit+1)
			}
			// Unicode counts by rune, not byte.
			if err := tt.fn(strings.Repeat("日", tt.limit)); err != nil {
				t.Errorf("%d CJK chars should pass (rune-counted): %v", tt.limit, err)
			}
			if err := tt.fn(strings.Repeat("日", tt.limit+1)); err == nil {
				t.Errorf("%d CJK chars should fail", tt.limit+1)
			}
		})
	}
}

func TestLoadSafetyConfigDefaults(t *testing.T) {
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "")
	t.Setenv("GOOGLE_ADS_MAX_BID_INCREASE_PCT", "")
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "")
	cfg := loadSafetyConfig()
	if cfg.MaxDailyBudget != 50.0 || cfg.MaxBidIncreasePct != 100 || len(cfg.BlockedOperations) != 0 {
		t.Errorf("defaults wrong: %+v", cfg)
	}
}

func TestLoadSafetyConfigFromEnv(t *testing.T) {
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "200")
	t.Setenv("GOOGLE_ADS_MAX_BID_INCREASE_PCT", "50")
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "delete_campaign, remove_ad")
	cfg := loadSafetyConfig()
	if cfg.MaxDailyBudget != 200 || cfg.MaxBidIncreasePct != 50 {
		t.Errorf("env override failed: %+v", cfg)
	}
	if len(cfg.BlockedOperations) != 2 || cfg.BlockedOperations[0] != "delete_campaign" || cfg.BlockedOperations[1] != "remove_ad" {
		t.Errorf("blocked ops parse failed: %v", cfg.BlockedOperations)
	}
}

func TestLoadSafetyConfig_RejectsBogusOverrides(t *testing.T) {
	// NaN would disable the cap entirely (x > NaN is always false) and
	// negative caps are meaningless — both must keep the default (issue #12).
	for _, v := range []string{"NaN", "-5", "0", "Inf", "banana"} {
		t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", v)
		t.Setenv("GOOGLE_ADS_MAX_BID_INCREASE_PCT", v)
		cfg := loadSafetyConfig()
		if cfg.MaxDailyBudget != 50.0 || cfg.MaxBidIncreasePct != 100 {
			t.Errorf("override %q should keep defaults, got %+v", v, cfg)
		}
	}
}

func TestCheckBudgetCap_RejectsNaN(t *testing.T) {
	if err := checkBudgetCap(math.NaN(), defaultSafetyConfig()); err == nil {
		t.Fatal("NaN budget should be rejected")
	}
}
