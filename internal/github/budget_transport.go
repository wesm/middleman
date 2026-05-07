package github

import (
	"context"
	"net/http"
)

type syncBudgetKey struct{}

// WithSyncBudget marks a context so that HTTP calls made with
// it count against the sync budget. Background sync entry points
// (RunOnce, syncWatchedMRs) inject this; user-initiated server
// handler paths do not.
func WithSyncBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, syncBudgetKey{}, true)
}

func IsSyncBudgetContext(ctx context.Context) bool {
	_, ok := ctx.Value(syncBudgetKey{}).(bool)
	return ok
}

// budgetTransport wraps an http.RoundTripper and increments a
// SyncBudget for every RoundTrip invocation whose request
// context carries the sync-budget key. This captures paginated
// pages, 304 responses, and GraphQL calls made during background
// sync without counting user-initiated server actions.
//
// Transparent retries inside net/http.Transport are not visible
// to RoundTripper wrappers and are not counted.
type budgetTransport struct {
	base   http.RoundTripper
	budget *SyncBudget
}

func (t *budgetTransport) RoundTrip(
	req *http.Request,
) (*http.Response, error) {
	if IsSyncBudgetContext(req.Context()) {
		t.budget.Spend(1)
	}
	return t.base.RoundTrip(req)
}
