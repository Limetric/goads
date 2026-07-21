package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunDeleteBudget_PreviewThenDoubleConfirm(t *testing.T) {
	useTempState(t)
	var mutates, searches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "googleAds:search") {
			searches++
			_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"referenceCount":"0"}}]}`))
			return
		}
		mutates++
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	args := DeleteBudgetArgs{CustomerID: "123-456-7890", BudgetID: "555"}
	preview, err := runDeleteBudget(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Applied || preview.Token == "" || mutates != 0 {
		t.Fatalf("bad preview: %+v, mutates=%d", preview, mutates)
	}

	args.Confirm = preview.Token
	second, err := runDeleteBudget(t.Context(), c, args)
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	if second.Applied || second.Token == "" || mutates != 0 {
		t.Fatalf("first confirm must restage: %+v, mutates=%d", second, mutates)
	}

	args.Confirm = second.Token
	done, err := runDeleteBudget(t.Context(), c, args)
	if err != nil {
		t.Fatalf("second confirm: %v", err)
	}
	if !done.Applied || mutates != 1 || searches != 3 {
		t.Fatalf("second confirm must apply: %+v, mutates=%d, searches=%d", done, mutates, searches)
	}
}

func TestRunDeleteBudget_RefusesReferencedBudget(t *testing.T) {
	useTempState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"referenceCount":"2"}}]}`))
	}))
	defer srv.Close()

	_, err := runDeleteBudget(t.Context(), newTestClient(t, srv), DeleteBudgetArgs{CustomerID: "1", BudgetID: "555"})
	if err == nil || !strings.Contains(err.Error(), "still used by 2") {
		t.Fatalf("expected in-use rejection, got %v", err)
	}
}

func TestRunDeleteBudget_RechecksReferenceCountOnConfirm(t *testing.T) {
	useTempState(t)
	var searches, mutates int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "googleAds:search") {
			searches++
			if searches == 1 {
				_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"referenceCount":"0"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"campaignBudget":{"referenceCount":"1"}}]}`))
			return
		}
		mutates++
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	args := DeleteBudgetArgs{CustomerID: "1", BudgetID: "555"}
	preview, err := runDeleteBudget(t.Context(), c, args)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	args.Confirm = preview.Token
	if _, err := runDeleteBudget(t.Context(), c, args); err == nil || !strings.Contains(err.Error(), "still used") {
		t.Fatalf("expected in-use rejection at confirm, got %v", err)
	}
	if mutates != 0 {
		t.Fatalf("referenced budget must not mutate (calls=%d)", mutates)
	}
}

func TestRunDeleteBudget_ValidationAndLookupFailures(t *testing.T) {
	useTempState(t)
	for name, args := range map[string]DeleteBudgetArgs{
		"missing customer": {BudgetID: "5"},
		"missing budget":   {CustomerID: "1"},
		"invalid budget":   {CustomerID: "1", BudgetID: "5 OR 1=1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := runDeleteBudget(t.Context(), nil, args); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	if _, err := runDeleteBudget(t.Context(), newTestClient(t, srv), DeleteBudgetArgs{CustomerID: "1", BudgetID: "5"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing-budget error, got %v", err)
	}
}

func TestDeleteBudgetArgs_Operations(t *testing.T) {
	ops := DeleteBudgetArgs{CustomerID: "123-456-7890", BudgetID: "5"}.operations()
	if err := validateMutateOps(ops); err != nil {
		t.Fatalf("operation rejected by allow-list: %v", err)
	}
	op := ops[0].(map[string]any)["campaignBudgetOperation"].(map[string]any)
	if op["remove"] != "customers/1234567890/campaignBudgets/5" {
		t.Errorf("remove = %v", op["remove"])
	}
}

func TestRunDeleteBudget_HonorsBlockedOps(t *testing.T) {
	useTempState(t)
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "delete_campaign_budget")
	if _, err := runDeleteBudget(t.Context(), nil, DeleteBudgetArgs{CustomerID: "1", BudgetID: "5"}); err == nil {
		t.Fatal("blocked operation should be rejected")
	}
}
