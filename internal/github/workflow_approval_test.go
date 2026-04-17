package github

import (
	"testing"

	gh "github.com/google/go-github/v84/github"
	Assert "github.com/stretchr/testify/assert"
)

func TestFilterWorkflowRunsAwaitingApproval(t *testing.T) {
	tests := []struct {
		name    string
		runs    []*gh.WorkflowRun
		headSHA string
		wantIDs []int64
	}{
		{
			name:    "matches runs at head sha",
			headSHA: "abc123",
			runs: []*gh.WorkflowRun{
				{
					ID:           new(int64(101)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{{Number: new(42)}},
				},
				{
					ID:      new(int64(103)),
					HeadSHA: new("def456"),
					Event:   new("pull_request"),
				},
			},
			wantIDs: []int64{101},
		},
		{
			name:    "includes fork PR runs with empty PullRequests",
			headSHA: "abc123",
			runs: []*gh.WorkflowRun{
				{
					ID:      new(int64(201)),
					HeadSHA: new("abc123"),
					Event:   new("pull_request"),
				},
				{
					ID:           new(int64(202)),
					HeadSHA:      new("abc123"),
					Event:        new("pull_request"),
					PullRequests: []*gh.PullRequest{},
				},
			},
			wantIDs: []int64{201, 202},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterWorkflowRunsAwaitingApproval(tt.runs, tt.headSHA)
			gotIDs := make([]int64, 0, len(got))
			for _, run := range got {
				gotIDs = append(gotIDs, run.GetID())
			}
			Assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestWorkflowApprovalStateFromRuns(t *testing.T) {
	assert := Assert.New(t)
	runs := []*gh.WorkflowRun{
		{ID: new(int64(11))},
		{ID: new(int64(12))},
	}

	got := WorkflowApprovalStateFromRuns(runs)

	assert.True(got.Checked)
	assert.True(got.Required)
	assert.Equal(2, got.Count)
	assert.Equal([]int64{11, 12}, got.RunIDs)
}
