package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestIssue(
	t *testing.T, d *DB,
	repoID int64, number int, title string, activity time.Time,
) int64 {
	t.Helper()
	issue := &Issue{
		RepoID:         repoID,
		GitHubID:       repoID*10000 + int64(number),
		Number:         number,
		URL:            "https://github.com/example/repo/issues/" + title,
		Title:          title,
		Author:         "author",
		State:          "open",
		CreatedAt:      activity,
		UpdatedAt:      activity,
		LastActivityAt: activity,
	}
	id, err := d.UpsertIssue(context.Background(), issue)
	require.NoErrorf(t, err, "UpsertIssue %d", number)
	return id
}

func TestListActivity(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	repoA := insertTestRepo(t, d, "alice", "alpha")
	repoB := insertTestRepo(t, d, "bob", "beta")

	prID1 := insertTestPR(t, d, repoA, 1, "Fix bug", base)
	prID2 := insertTestPR(
		t, d, repoB, 2, "Add feature", base.Add(1*time.Minute))
	issueID1 := insertTestIssue(
		t, d, repoA, 10, "Crash on startup", base.Add(2*time.Minute))

	err := d.UpsertPREvents(ctx, []PREvent{
		{PRID: prID1, EventType: "issue_comment", Author: "carol",
			Body:      "Looks good to me",
			CreatedAt: base.Add(3 * time.Minute),
			DedupeKey: "comment-1"},
		{PRID: prID2, EventType: "review", Author: "dave",
			Summary:   "APPROVED",
			CreatedAt: base.Add(4 * time.Minute),
			DedupeKey: "review-1"},
		{PRID: prID1, EventType: "commit", Author: "alice",
			Summary: "abc123", Body: "fix: handle nil",
			CreatedAt: base.Add(5 * time.Minute),
			DedupeKey: "commit-abc123"},
		{PRID: prID1, EventType: "review_comment", Author: "eve",
			Body:      "nit: rename var",
			CreatedAt: base.Add(6 * time.Minute),
			DedupeKey: "review_comment-1"},
	})
	require.NoError(t, err)

	err = d.UpsertIssueEvents(ctx, []IssueEvent{
		{IssueID: issueID1, EventType: "issue_comment", Author: "frank",
			Body:      "Can reproduce on macOS",
			CreatedAt: base.Add(7 * time.Minute),
			DedupeKey: "icomment-1"},
	})
	require.NoError(t, err)

	t.Run("unfiltered returns all types in desc order", func(t *testing.T) {
		assert := Assert.New(t)
		items, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 50})
		require.NoError(t, err)
		// Expected order (newest first):
		// 1. issue comment (base+7m) - review_comment excluded
		// 2. commit (base+5m)
		// 3. review (base+4m)
		// 4. PR comment (base+3m)
		// 5. new issue (base+2m)
		// 6. new PR bob/beta#2 (base+1m)
		// 7. new PR alice/alpha#1 (base)
		require.Len(t, items, 7)
		assert.Equal("comment", items[0].ActivityType)
		assert.Equal("issue", items[0].ItemType)
		assert.Equal("commit", items[1].ActivityType)
		assert.Equal("review", items[2].ActivityType)
		assert.Equal("comment", items[3].ActivityType)
		assert.Equal("pr", items[3].ItemType)
		assert.Equal("new_issue", items[4].ActivityType)
		assert.Equal("new_pr", items[5].ActivityType)
		assert.Equal("bob", items[5].RepoOwner)
		assert.Equal("new_pr", items[6].ActivityType)
		assert.Equal("alice", items[6].RepoOwner)
	})

	t.Run("repo filter", func(t *testing.T) {
		assert := Assert.New(t)
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Repo: "alice/alpha", Limit: 50,
		})
		require.NoError(t, err)
		for _, it := range items {
			assert.Equal("alice", it.RepoOwner)
			assert.Equal("alpha", it.RepoName)
		}
	})

	t.Run("type filter", func(t *testing.T) {
		assert := Assert.New(t)
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Types: []string{"new_pr", "new_issue"},
			Limit: 50,
		})
		require.NoError(t, err)
		require.Len(t, items, 3)
		for _, it := range items {
			assert.Contains([]string{"new_pr", "new_issue"}, it.ActivityType)
		}
	})

	t.Run("search filter", func(t *testing.T) {
		assert := Assert.New(t)
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Search: "bug", Limit: 50,
		})
		require.NoError(t, err)
		require.NotEmpty(t, items)
		for _, it := range items {
			assert.Equal("Fix bug", it.ItemTitle)
		}
	})

	t.Run("limit and before cursor", func(t *testing.T) {
		assert := Assert.New(t)
		require := require.New(t)
		page1, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 3})
		require.NoError(err)
		require.Len(page1, 3)

		last := page1[2]
		page2, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:          3,
			BeforeTime:     &last.CreatedAt,
			BeforeSource:   last.Source,
			BeforeSourceID: last.SourceID,
		})
		require.NoError(err)
		require.Len(page2, 3)

		seen := make(map[string]bool)
		for _, it := range page1 {
			key := fmt.Sprintf("%s:%d", it.Source, it.SourceID)
			seen[key] = true
		}
		for _, it := range page2 {
			key := fmt.Sprintf("%s:%d", it.Source, it.SourceID)
			assert.False(seen[key], "duplicate across pages: %s", key)
		}
	})

	t.Run("after cursor for polling", func(t *testing.T) {
		assert := Assert.New(t)
		require := require.New(t)
		all, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 50})
		require.NoError(err)
		newest := all[0]

		err = d.UpsertPREvents(ctx, []PREvent{
			{PRID: prID1, EventType: "issue_comment", Author: "grace",
				Body:      "New comment",
				CreatedAt: base.Add(10 * time.Minute),
				DedupeKey: "comment-new"},
		})
		require.NoError(err)

		newItems, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:         50,
			AfterTime:     &newest.CreatedAt,
			AfterSource:   newest.Source,
			AfterSourceID: newest.SourceID,
		})
		require.NoError(err)
		require.Len(newItems, 1)
		assert.Equal("grace", newItems[0].Author)
	})

	t.Run("since time window", func(t *testing.T) {
		assert := Assert.New(t)
		since := base.Add(4 * time.Minute)
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Limit: 50,
			Since: &since,
		})
		require.NoError(t, err)
		for _, it := range items {
			assert.Condition(func() bool {
				return !it.CreatedAt.Before(since)
			}, "item %s:%d has created_at %v before since %v", it.Source, it.SourceID, it.CreatedAt, since)
		}
		// base+4m is review, base+5m is commit, base+7m is issue comment,
		// base+10m is comment-new from after cursor test = 4 items
		assert.Len(items, 4)
	})

	_ = prID2
}
