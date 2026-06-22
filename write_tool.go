package main

import "context"

// WriteResult is the standard structured output for a write tool. The first
// call (no confirm token) returns Token+Preview; the confirm call returns
// Applied=true with a Detail summary.
type WriteResult struct {
	Applied bool   `json:"applied"`
	Token   string `json:"confirm_token,omitempty"`
	Preview string `json:"preview,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// previewResult wraps a freshly staged pending mutation as a preview WriteResult.
func previewResult(p *PendingMutation) WriteResult {
	return WriteResult{Applied: false, Token: p.Token, Preview: p.previewText()}
}

// previewMutate stages a default googleAds:mutate write and returns its preview.
func previewMutate(tool, customerID, summary string, ops []any) (WriteResult, error) {
	p, err := stageMutation(tool, customerID, summary, ops)
	if err != nil {
		return WriteResult{}, err
	}
	return previewResult(p), nil
}

// applyConfirmed consumes a confirm token and applies the staged write via the
// correct dispatch, writing an audit line on both success and failure.
func applyConfirmed(ctx context.Context, c *Client, tool, confirm string) (WriteResult, error) {
	p, err := consumeMutation(confirm)
	if err != nil {
		return WriteResult{}, err
	}
	if err := applyPending(ctx, c, p); err != nil {
		auditLog(p, false)
		return WriteResult{}, toolError(tool, err)
	}
	auditLog(p, true)
	return WriteResult{Applied: true, Detail: p.Summary}, nil
}
