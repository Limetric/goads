package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunBudgetSet_PreviewThenApply(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "") // use the default $50/day cap
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	args := BudgetSetArgs{CustomerID: "123-456-7890", BudgetID: "555", AmountMicros: 5_000_000} // $5/day
	prev, err := runBudgetSet(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.Applied || prev.Token == "" || prev.Preview == "" {
		t.Fatalf("bad preview: %+v", prev)
	}
	if cap.calls != 0 {
		t.Fatalf("preview must not call mutate (calls=%d)", cap.calls)
	}

	args.Confirm = prev.Token
	done, err := runBudgetSet(t.Context(), c, args)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !done.Applied || cap.calls != 1 {
		t.Fatalf("apply failed: result=%+v calls=%d", done, cap.calls)
	}
	op := cap.firstOp(t)["campaignBudgetOperation"].(map[string]any)
	if op["updateMask"] != "amountMicros" {
		t.Errorf("updateMask = %v, want amountMicros", op["updateMask"])
	}
	upd := opUpdate(t, cap.firstOp(t), "campaignBudgetOperation")
	if upd["resourceName"] != "customers/1234567890/campaignBudgets/555" {
		t.Errorf("resourceName = %v", upd["resourceName"])
	}
	if amt, _ := asFloat(upd["amountMicros"]); amt != 5_000_000 {
		t.Errorf("amountMicros = %v, want 5000000", upd["amountMicros"])
	}
}

func TestRunBudgetSet_RequiresIDs(t *testing.T) {
	if _, err := runBudgetSet(t.Context(), nil, BudgetSetArgs{BudgetID: "5", AmountMicros: 1_000_000}); err == nil {
		t.Error("missing customer_id should error")
	}
	if _, err := runBudgetSet(t.Context(), nil, BudgetSetArgs{CustomerID: "1", AmountMicros: 1_000_000}); err == nil {
		t.Error("missing budget_id should error")
	}
}

func TestRunBudgetSet_BudgetCapGuard(t *testing.T) {
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "") // default $50/day
	// $60/day exceeds the default cap and must be rejected before any API call.
	if _, err := runBudgetSet(t.Context(), nil, BudgetSetArgs{CustomerID: "1", BudgetID: "5", AmountMicros: 60_000_000}); err == nil {
		t.Fatal("expected the budget cap to reject $60/day")
	}
}

func TestBudgetSetArgs_Operations(t *testing.T) {
	ops := BudgetSetArgs{CustomerID: "123-456-7890", BudgetID: "5", AmountMicros: 2_000_000}.operations()
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	// The operation must use an allowed mutate key (would otherwise be rejected
	// client-side at apply time).
	if err := validateMutateOps(ops); err != nil {
		t.Errorf("operation rejected by allow-list: %v", err)
	}
}

func TestRunBudgetSet_PartialFailureFailsApply(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// partialFailure:true means a bad op returns HTTP 200 with the error in
		// the body; the tool used to report Applied=true for this (issue #7).
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{}],"partialFailureError":{"code":3,"message":"budget below account minimum"}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	args := BudgetSetArgs{CustomerID: "1", BudgetID: "555", AmountMicros: 5_000_000}
	prev, err := runBudgetSet(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	done, err := runBudgetSet(t.Context(), c, args)
	if err == nil || !strings.Contains(err.Error(), "budget below account minimum") {
		t.Fatalf("expected partial-failure error, got result=%+v err=%v", done, err)
	}
	if done.Applied {
		t.Fatal("Applied must be false on partial failure")
	}
}

func TestRunBudgetSet_RejectsNonPositiveAmount(t *testing.T) {
	useTempState(t)
	if _, err := runBudgetSet(t.Context(), nil, BudgetSetArgs{CustomerID: "1", BudgetID: "5"}); err == nil {
		t.Fatal("amount_micros = 0 should be rejected")
	}
}

func TestRunBudgetSet_HonorsBlockedOps(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "set_campaign_budget")
	if _, err := runBudgetSet(t.Context(), nil, BudgetSetArgs{CustomerID: "1", BudgetID: "5", AmountMicros: 1_000_000}); err == nil {
		t.Fatal("blocked operation should be rejected")
	}
}

func TestRunBudgetSet_LargeIncreaseNeedsDoubleConfirm(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "")
	// Budget increases over 50% take a second confirmation (issue #12): the
	// current amount ($1/day) is fetched from the API, and the jump to $5/day
	// re-stages on the first confirm instead of applying.
	var mutates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "googleAds:search") {
			_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"amountMicros":"1000000"}}]}`))
			return
		}
		mutates++
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	args := BudgetSetArgs{CustomerID: "1", BudgetID: "555", AmountMicros: 5_000_000}
	prev, err := runBudgetSet(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = prev.Token
	second, err := runBudgetSet(t.Context(), c, args)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	if second.Applied || second.Token == "" || mutates != 0 {
		t.Fatalf("first confirm of a >50%% increase must re-stage, got %+v mutates=%d", second, mutates)
	}
	args.Confirm = second.Token
	done, err := runBudgetSet(t.Context(), c, args)
	if err != nil || !done.Applied || mutates != 1 {
		t.Fatalf("second confirm should apply: %+v err=%v mutates=%d", done, err, mutates)
	}
}

func TestRunBudgetSet_SmallChangeSingleConfirm(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_MAX_DAILY_BUDGET", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "googleAds:search") {
			_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"amountMicros":"4000000"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	// $4 -> $5 is a 25% increase: one confirm applies.
	args := BudgetSetArgs{CustomerID: "1", BudgetID: "555", AmountMicros: 5_000_000}
	prev, _ := runBudgetSet(t.Context(), c, args)
	args.Confirm = prev.Token
	done, err := runBudgetSet(t.Context(), c, args)
	if err != nil || !done.Applied {
		t.Fatalf("small increase should apply on first confirm: %+v err=%v", done, err)
	}
}
