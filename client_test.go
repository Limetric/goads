package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateMutateOps(t *testing.T) {
	t.Run("accepts known keys", func(t *testing.T) {
		ops := []any{
			map[string]any{"adGroupOperation": map[string]any{"create": map[string]any{}}},
			map[string]any{"campaignBudgetOperation": map[string]any{"create": map[string]any{}}},
		}
		if err := validateMutateOps(ops); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("rejects recommendation ops", func(t *testing.T) {
		ops := []any{map[string]any{"dismissRecommendationOperation": map[string]any{"resourceName": "x"}}}
		err := validateMutateOps(ops)
		if err == nil || !strings.Contains(err.Error(), "dismissRecommendationOperation") {
			t.Errorf("expected rejection, got %v", err)
		}
		if !strings.Contains(err.Error(), "apply_recommendations / dismiss_recommendations") {
			t.Errorf("error missing guidance: %v", err)
		}
	})
	t.Run("rejects apply recommendation op", func(t *testing.T) {
		ops := []any{map[string]any{"applyRecommendationOperation": map[string]any{"resourceName": "x"}}}
		if err := validateMutateOps(ops); err == nil {
			t.Error("expected rejection of applyRecommendationOperation")
		}
	})
	t.Run("rejects non-object op", func(t *testing.T) {
		if err := validateMutateOps([]any{"not-an-object"}); err == nil {
			t.Error("expected rejection of non-object op")
		}
	})
}

func TestMutate_ValidatesAndSetsPartialFailure(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/1/campaignBudgets/2"}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	// Bad op never reaches the server.
	if _, err := c.Mutate(context.Background(), "123", []any{map[string]any{"bogusOperation": map[string]any{}}}); err == nil {
		t.Fatal("expected validation error for unknown op key")
	}
	if gotBody != nil {
		t.Fatal("server should not have been called for an invalid op")
	}

	// Good op is sent with partialFailure set.
	_, err := c.Mutate(context.Background(), "123", []any{map[string]any{"campaignBudgetOperation": map[string]any{"update": map[string]any{}}}})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if pf, ok := gotBody["partialFailure"].(bool); !ok || !pf {
		t.Errorf("partialFailure not set in body: %v", gotBody)
	}
}

func TestGenerateKeywordIdeas(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/customers/1234567890:generateKeywordIdeas") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"text":"shoes"},{"text":"boots"}]}`))
	}))
	defer srv.Close()

	rows, err := newTestClient(t, srv).GenerateKeywordIdeas(context.Background(), "123-456-7890", []string{"shoes"}, 0)
	if err != nil {
		t.Fatalf("GenerateKeywordIdeas: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("got %d ideas, want 2", len(rows))
	}
	// Defaults applied: pageSize 50, search network, seed keyword present.
	if pageSize, _ := asFloat(gotBody["pageSize"]); pageSize != 50 {
		t.Errorf("pageSize default = %v, want 50", gotBody["pageSize"])
	}
	if gotBody["keywordPlanNetwork"] != "GOOGLE_SEARCH" {
		t.Errorf("keywordPlanNetwork = %v", gotBody["keywordPlanNetwork"])
	}
}

func TestListAccessibleCustomers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/customers:listAccessibleCustomers" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("developer-token") == "" {
			t.Error("developer-token header not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resourceNames":["customers/1234567890","customers/9876543210"]}`))
	}))
	defer srv.Close()

	cfg := &Config{BaseURL: srv.URL} // non-prod base → isTest(), lenient auth
	c, err := NewClient(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := c.ListAccessibleCustomers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1234567890", "9876543210"}
	if len(ids) != len(want) || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("got %v, want %v", ids, want)
	}
}

func TestApplyAndDismissRecommendations(t *testing.T) {
	t.Run("apply sends partialFailure and resource names", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/recommendations:apply") {
				t.Errorf("path = %q", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/1/recommendations/r1"}]}`))
		}))
		defer srv.Close()

		res, err := newTestClient(t, srv).ApplyRecommendations(context.Background(), "1", []string{"customers/1/recommendations/r1"})
		if err != nil {
			t.Fatalf("ApplyRecommendations: %v", err)
		}
		if len(res.Results) != 1 {
			t.Errorf("got %d results, want 1", len(res.Results))
		}
		if pf, _ := gotBody["partialFailure"].(bool); !pf {
			t.Error("apply should set partialFailure")
		}
		ops, _ := gotBody["operations"].([]any)
		if len(ops) != 1 {
			t.Fatalf("operations = %v", gotBody["operations"])
		}
	})

	t.Run("dismiss omits partialFailure", func(t *testing.T) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/recommendations:dismiss") {
				t.Errorf("path = %q", r.URL.Path)
			}
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{}]}`))
		}))
		defer srv.Close()

		if _, err := newTestClient(t, srv).DismissRecommendations(context.Background(), "1", []string{"customers/1/recommendations/r1"}); err != nil {
			t.Fatalf("DismissRecommendations: %v", err)
		}
		if _, present := gotBody["partialFailure"]; present {
			t.Error("dismiss should not send partialFailure")
		}
	})
}
