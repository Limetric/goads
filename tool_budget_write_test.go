package main

import "testing"

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
	if op["updateMask"] != "amount_micros" {
		t.Errorf("updateMask = %v, want amount_micros", op["updateMask"])
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
