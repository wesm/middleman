package github

import "testing"

// Compile-time assertion that liveClient satisfies Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c := NewClient("fake-token")
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
