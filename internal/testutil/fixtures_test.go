package testutil

import (
	"context"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func TestSeedFixtures_Repos(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d, _ := OpenFixtureTestDB(t)
	ctx := context.Background()

	repos, err := d.ListRepos(ctx)
	require.NoError(err)
	assert.Len(repos, 3)

	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.FullName()
	}
	assert.Contains(names, "acme/widgets")
	assert.Contains(names, "acme/tools")
	assert.Contains(names, "acme/archived")
}

func TestSeedFixtures_PRCounts(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d, _ := OpenFixtureTestDB(t)
	ctx := context.Background()

	allPRs, err := d.ListPullRequests(ctx, db.ListPullsOpts{State: "all"})
	require.NoError(err)
	assert.Len(allPRs, 9)

	openPRs, err := d.ListPullRequests(ctx, db.ListPullsOpts{State: "open"})
	require.NoError(err)
	assert.Len(openPRs, 5)

	mergedPRs, err := d.ListPullRequests(ctx, db.ListPullsOpts{State: "merged"})
	require.NoError(err)
	assert.Len(mergedPRs, 3)

	closedPRs, err := d.ListPullRequests(ctx, db.ListPullsOpts{State: "closed"})
	require.NoError(err)
	// "closed" state filter returns state IN ('closed','merged'), so 4 total
	assert.Len(closedPRs, 4)

	// Verify the one truly closed-not-merged PR
	var notMergedClosed int
	for _, pr := range allPRs {
		if pr.State == "closed" {
			notMergedClosed++
		}
	}
	assert.Equal(1, notMergedClosed)
}

func TestSeedFixtures_IssueCounts(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d, _ := OpenFixtureTestDB(t)
	ctx := context.Background()

	allIssues, err := d.ListIssues(ctx, db.ListIssuesOpts{State: "all"})
	require.NoError(err)
	assert.Len(allIssues, 5)

	openIssues, err := d.ListIssues(ctx, db.ListIssuesOpts{State: "open"})
	require.NoError(err)
	assert.Len(openIssues, 4)

	closedIssues, err := d.ListIssues(ctx, db.ListIssuesOpts{State: "closed"})
	require.NoError(err)
	assert.Len(closedIssues, 1)
	assert.Equal("Crash on empty input", closedIssues[0].Title)
}

func TestSeedFixtures_Activity(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d, _ := OpenFixtureTestDB(t)
	ctx := context.Background()

	items, err := d.ListActivity(ctx, db.ListActivityOpts{Limit: 200})
	require.NoError(err)

	var prComments, issueComments int
	for _, item := range items {
		if item.ActivityType == "comment" && item.ItemType == "pr" {
			prComments++
		}
		if item.ActivityType == "comment" && item.ItemType == "issue" {
			issueComments++
		}
	}
	// PR comments: w1 (bob+carol), w2 (alice), t1 (alice) = 4
	assert.Equal(4, prComments, "expected 4 PR comments in activity feed")
	// Issue comments: wi10 (alice+bob), wi12 (carol), ti5 (dave) = 4
	assert.Equal(4, issueComments, "expected 4 issue comments in activity feed")
}

func TestSeedFixtures_FixtureClient(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	_, result := OpenFixtureTestDB(t)

	fc := result.FixtureClient()
	require.NotNil(fc)

	widgetsPRs := fc.OpenPRs["acme/widgets"]
	toolsPRs := fc.OpenPRs["acme/tools"]
	widgetsIssues := fc.OpenIssues["acme/widgets"]
	toolsIssues := fc.OpenIssues["acme/tools"]

	// 4 open PRs in widgets: #1, #2, #6, #7
	assert.Len(widgetsPRs, 4, "expected 4 open PRs in acme/widgets")
	// 1 open PR in tools: #1
	assert.Len(toolsPRs, 1, "expected 1 open PR in acme/tools")
	// 3 open issues in widgets: #10, #11, #13
	assert.Len(widgetsIssues, 3, "expected 3 open issues in acme/widgets")
	// 1 open issue in tools: #5
	assert.Len(toolsIssues, 1, "expected 1 open issue in acme/tools")

	// Verify specific PR numbers in widgets
	widgetsPRNums := make([]int, len(widgetsPRs))
	for i, pr := range widgetsPRs {
		widgetsPRNums[i] = pr.GetNumber()
	}
	assert.Contains(widgetsPRNums, 1)
	assert.Contains(widgetsPRNums, 2)
	assert.Contains(widgetsPRNums, 6)
	assert.Contains(widgetsPRNums, 7)

	// Verify tools PR
	assert.Equal(1, toolsPRs[0].GetNumber())

	// Verify specific issue numbers in widgets
	widgetsIssueNums := make([]int, len(widgetsIssues))
	for i, issue := range widgetsIssues {
		widgetsIssueNums[i] = issue.GetNumber()
	}
	assert.Contains(widgetsIssueNums, 10)
	assert.Contains(widgetsIssueNums, 11)
	assert.Contains(widgetsIssueNums, 13)

	// Verify tools issue
	assert.Equal(5, toolsIssues[0].GetNumber())
}
