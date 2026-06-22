package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunListRecommendations(t *testing.T) {
	srv := gaqlServer(t, `[{"recommendation":{"type":"KEYWORD"}}]`, "FROM recommendation", "recommendation.dismissed = FALSE", "LIMIT 50")
	defer srv.Close()
	res, err := runListRecommendations(t.Context(), newTestClient(t, srv), RecommendationsArgs{CustomerID: "1"})
	if err != nil || res.TotalCount != 1 {
		t.Fatalf("runListRecommendations: res=%+v err=%v", res, err)
	}
}

func TestApplyRecommendation_PreviewThenApply(t *testing.T) {
	useTempState(t)
	var applyHits, otherHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/recommendations:apply"):
			applyHits++
		default:
			otherHits++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	// Preview: stages a token, touches no endpoint.
	prev, err := runApplyRecommendation(t.Context(), c, RecommendationActionArgs{CustomerID: "123-456-7890", RecommendationID: "rec-1"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if prev.Applied || prev.Token == "" {
		t.Fatalf("expected un-applied preview with token, got %+v", prev)
	}
	if applyHits != 0 || otherHits != 0 {
		t.Fatalf("preview should not call the API (apply=%d other=%d)", applyHits, otherHits)
	}

	// Apply: consumes token, routes to recommendations:apply (not mutate).
	done, err := runApplyRecommendation(t.Context(), c, RecommendationActionArgs{CustomerID: "123-456-7890", RecommendationID: "rec-1", Confirm: prev.Token})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !done.Applied {
		t.Errorf("expected Applied=true, got %+v", done)
	}
	if applyHits != 1 {
		t.Errorf("expected 1 apply call, got %d (other=%d)", applyHits, otherHits)
	}
}

func TestDismissRecommendation_RoutesToDismiss(t *testing.T) {
	useTempState(t)
	var dismissHits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/recommendations:dismiss") {
			dismissHits++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{}]}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)

	prev, err := runDismissRecommendation(t.Context(), c, RecommendationActionArgs{CustomerID: "1", RecommendationID: "rec-2"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := runDismissRecommendation(t.Context(), c, RecommendationActionArgs{CustomerID: "1", RecommendationID: "rec-2", Confirm: prev.Token}); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if dismissHits != 1 {
		t.Errorf("expected 1 dismiss call, got %d", dismissHits)
	}
}

func TestRecommendationAction_Blocked(t *testing.T) {
	t.Setenv("GOOGLE_ADS_BLOCKED_OPS", "apply_recommendation")
	if _, err := runApplyRecommendation(t.Context(), nil, RecommendationActionArgs{CustomerID: "1", RecommendationID: "rec-1"}); err == nil {
		t.Fatal("expected blocked-operation error")
	}
}

func TestRecommendationAction_RequiresIDs(t *testing.T) {
	if _, err := runApplyRecommendation(t.Context(), nil, RecommendationActionArgs{CustomerID: "1"}); err == nil {
		t.Fatal("expected error for missing recommendation_id")
	}
}
