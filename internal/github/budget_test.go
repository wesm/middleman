package github

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
)

func TestSyncBudgetBasics(t *testing.T) {
	assert := Assert.New(t)
	b := NewSyncBudget(100)

	assert.Equal(100, b.Limit())
	assert.Equal(0, b.Spent())
	assert.Equal(100, b.Remaining())
	assert.True(b.CanSpend(6))

	b.Spend(50)
	assert.Equal(50, b.Spent())
	assert.Equal(50, b.Remaining())
	assert.True(b.CanSpend(6))
	assert.False(b.CanSpend(51))

	b.Reset()
	assert.Equal(0, b.Spent())
	assert.Equal(100, b.Remaining())
}

func TestSyncBudgetWorstCase(t *testing.T) {
	b := NewSyncBudget(10)
	b.Spend(5)
	Assert.False(t, b.CanSpend(PRDetailWorstCase))   // 6 > 5 remaining
	Assert.True(t, b.CanSpend(IssueDetailWorstCase)) // 2 <= 5 remaining
}
