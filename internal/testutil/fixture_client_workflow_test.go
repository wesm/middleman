package testutil

import (
	"sync"
	"sync/atomic"
	"testing"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixtureClientCheckRunsConcurrentStatusUpdates(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	client := NewFixtureClient()
	fc, ok := client.(*FixtureClient)
	require.True(ok)

	headSHA := "head-sha"
	status := "queued"
	conclusion := ""
	fc.PRs["acme/widgets"] = []*gh.PullRequest{{
		Number: new(1),
		Head:   &gh.PullRequestBranch{SHA: &headSHA},
	}}
	fc.CheckRuns["acme/widgets@head-sha"] = []*gh.CheckRun{{
		Name:       new("test"),
		Status:     &status,
		Conclusion: &conclusion,
	}}

	const goroutines = 8
	const iterations = 100
	var errors atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range iterations {
				if !fc.SetPullRequestCheckRunStatus(
					"acme", "widgets", 1, "completed", "success",
				) {
					errors.Add(1)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				runs, err := fc.ListCheckRunsForRef(t.Context(), "acme", "widgets", headSHA)
				if err != nil || len(runs) != 1 {
					errors.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	runs, err := fc.ListCheckRunsForRef(t.Context(), "acme", "widgets", headSHA)
	require.NoError(err)
	require.Len(runs, 1)
	assert.Zero(errors.Load())
	assert.Equal("completed", runs[0].GetStatus())
	assert.Equal("success", runs[0].GetConclusion())
}

func TestFixtureClientWorkflowRunsConcurrentAccess(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	client := NewFixtureClient()
	fc, ok := client.(*FixtureClient)
	require.True(ok)

	const goroutines = 8
	const iterations = 100
	var errors atomic.Int64
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range iterations {
				runID := int64(j)
				fc.SetWorkflowRuns(
					"acme", "widgets", "head-sha",
					[]*gh.WorkflowRun{{ID: &runID}},
				)
			}
		}()
		go func() {
			defer wg.Done()
			for range iterations {
				_, err := fc.ListWorkflowRunsForHeadSHA(
					t.Context(), "acme", "widgets", "head-sha",
				)
				if err != nil {
					errors.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	runs, err := fc.ListWorkflowRunsForHeadSHA(t.Context(), "acme", "widgets", "head-sha")
	require.NoError(err)
	assert.Zero(errors.Load())
	assert.Len(runs, 1)
}
