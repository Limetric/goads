package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mutateCapture records calls to the googleAds:mutate endpoint of a fake API.
type mutateCapture struct {
	calls    int
	lastBody map[string]any
}

// lastOps returns the mutateOperations array from the most recent mutate call.
func (m *mutateCapture) lastOps() []any {
	ops, _ := m.lastBody["mutateOperations"].([]any)
	return ops
}

// firstOp returns the single operation object from the most recent mutate call.
func (m *mutateCapture) firstOp(t *testing.T) map[string]any {
	t.Helper()
	ops := m.lastOps()
	if len(ops) == 0 {
		t.Fatalf("no mutate operations captured (body=%v)", m.lastBody)
	}
	op, _ := ops[0].(map[string]any)
	return op
}

// mutateServer returns a fake Ads API that counts and records googleAds:mutate
// calls and replies with a generic success body.
func mutateServer(t *testing.T) (*httptest.Server, *mutateCapture) {
	t.Helper()
	cap := &mutateCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "googleAds:mutate") {
			cap.calls++
			_ = json.NewDecoder(r.Body).Decode(&cap.lastBody)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"resourceName":"customers/1/x/2"}]}`))
	}))
	return srv, cap
}

// opCreate digs out op[<key>]["create"] as a map for assertions.
func opCreate(t *testing.T, op map[string]any, key string) map[string]any {
	t.Helper()
	outer, ok := op[key].(map[string]any)
	if !ok {
		t.Fatalf("operation has no %q key: %v", key, op)
	}
	create, ok := outer["create"].(map[string]any)
	if !ok {
		t.Fatalf("%q has no create: %v", key, outer)
	}
	return create
}

func TestWriteHelpers_PreviewThenApply(t *testing.T) {
	useTempState(t)
	srv, cap := mutateServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	op := map[string]any{"campaignBudgetOperation": map[string]any{"update": map[string]any{"resourceName": "x"}}}
	prev, err := previewMutate("test_tool", "123", "do a thing", []any{op})
	if err != nil {
		t.Fatalf("previewMutate: %v", err)
	}
	if prev.Applied || prev.Token == "" || prev.Preview == "" {
		t.Fatalf("bad preview: %+v", prev)
	}
	if cap.calls != 0 {
		t.Fatalf("preview must not call mutate")
	}

	done, err := applyConfirmed(t.Context(), c, "test_tool", prev.Token)
	if err != nil {
		t.Fatalf("applyConfirmed: %v", err)
	}
	if !done.Applied || cap.calls != 1 {
		t.Fatalf("apply failed: result=%+v calls=%d", done, cap.calls)
	}
}
