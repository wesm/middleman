package db

import (
	"context"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func baseTime() time.Time {
	return time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
}

func insertTestRepo(t *testing.T, d *DB, owner, name string) int64 {
	t.Helper()
	id, err := d.UpsertRepo(context.Background(), owner, name)
	require.NoErrorf(t, err, "UpsertRepo(%s/%s)", owner, name)
	return id
}

func insertTestMR(t *testing.T, d *DB, repoID int64, number int, title string, activity time.Time) int64 {
	t.Helper()
	mr := &MergeRequest{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(number),
		Number:         number,
		URL:            "https://github.com/example/repo/pull/" + title,
		Title:          title,
		Author:         "author",
		State:          "open",
		IsDraft:        false,
		HeadBranch:     "feature",
		BaseBranch:     "main",
		CreatedAt:      activity,
		UpdatedAt:      activity,
		LastActivityAt: activity,
	}
	id, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoErrorf(t, err, "UpsertMergeRequest %d", number)
	return id
}

func TestUpsertAndListRepos(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	id1, err := d.UpsertRepo(ctx, "alice", "alpha")
	require.NoError(err)
	id2, err := d.UpsertRepo(ctx, "bob", "beta")
	require.NoError(err)
	assert.NotEqual(id1, id2)

	// Idempotency: re-inserting should return the same ID.
	id1Again, err := d.UpsertRepo(ctx, "alice", "alpha")
	require.NoError(err)
	assert.Equal(id1, id1Again)

	repos, err := d.ListRepos(ctx)
	require.NoError(err)
	require.Len(repos, 2)
	// Ordered by owner, name: alice/alpha, bob/beta.
	assert.Equal("alice", repos[0].Owner)
	assert.Equal("alpha", repos[0].Name)
	assert.Equal("bob", repos[1].Owner)
	assert.Equal("beta", repos[1].Name)
}

func TestGetRepoByOwnerName(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	id := insertTestRepo(t, d, "owner", "repo")

	r, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.Equal(id, r.ID)

	missing, err := d.GetRepoByOwnerName(ctx, "no", "such")
	require.NoError(t, err)
	assert.Nil(missing)
}

func TestUpdateRepoSync(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	id := insertTestRepo(t, d, "o", "r")
	now := baseTime()

	require.NoError(d.UpdateRepoSyncStarted(ctx, id, now))
	later := now.Add(time.Minute)
	require.NoError(d.UpdateRepoSyncCompleted(ctx, id, later, ""))

	r, err := d.GetRepoByOwnerName(ctx, "o", "r")
	require.NoError(err)
	require.NotNil(r)
	require.NotNil(r.LastSyncStartedAt)
	require.NotNil(r.LastSyncCompletedAt)
	assert.True(r.LastSyncStartedAt.Equal(now))
	assert.True(r.LastSyncCompletedAt.Equal(later))
	assert.Empty(r.LastSyncError)

	// Record a sync error.
	require.NoError(d.UpdateRepoSyncCompleted(ctx, id, later, "rate limited"))
	r2, _ := d.GetRepoByOwnerName(ctx, "o", "r")
	require.NotNil(r2)
	assert.Equal("rate limited", r2.LastSyncError)
}

func TestUpsertAndGetPullRequest(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	now := baseTime()

	pr := &MergeRequest{
		RepoID:         repoID,
		PlatformID:     42,
		Number:         7,
		URL:            "https://github.com/owner/repo/pull/7",
		Title:          "fix: something",
		Author:         "alice",
		State:          "open",
		IsDraft:        false,
		Body:           "body text",
		HeadBranch:     "fix/something",
		BaseBranch:     "main",
		Additions:      10,
		Deletions:      3,
		CommentCount:   2,
		ReviewDecision: "APPROVED",
		CIStatus:       "SUCCESS",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	id, err := d.UpsertMergeRequest(ctx, pr)
	require.NoError(err)
	assert.NotZero(id)

	got, err := d.GetMergeRequest(ctx, "owner", "repo", 7)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(id, got.ID)
	assert.Equal(pr.Title, got.Title)
	assert.Equal(pr.Author, got.Author)
	assert.Equal(pr.Additions, got.Additions)
	assert.Empty(got.KanbanStatus)

	// Update via upsert — change title and additions.
	pr.Title = "fix: something updated"
	pr.Additions = 20
	pr.UpdatedAt = now.Add(time.Hour)
	pr.LastActivityAt = now.Add(time.Hour)

	id2, err := d.UpsertMergeRequest(ctx, pr)
	require.NoError(err)
	assert.Equal(id, id2)

	got2, _ := d.GetMergeRequest(ctx, "owner", "repo", 7)
	require.NotNil(got2)
	assert.Equal("fix: something updated", got2.Title)
	assert.Equal(20, got2.Additions)
	// created_at must not change.
	assert.True(got2.CreatedAt.Equal(now))

	// Missing PR returns nil.
	missing, err := d.GetMergeRequest(ctx, "owner", "repo", 999)
	require.NoError(err)
	assert.Nil(missing)
}

func TestListPullRequests(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	// Insert 3 PRs with different last_activity_at.
	insertTestMR(t, d, repoID, 1, "oldest", base)
	insertTestMR(t, d, repoID, 2, "middle", base.Add(time.Hour))
	insertTestMR(t, d, repoID, 3, "newest", base.Add(2*time.Hour))

	prs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{})
	require.NoError(t, err)
	require.Len(t, prs, 3)
	// Newest first.
	Assert.Equal(t, []int{3, 2, 1}, []int{prs[0].Number, prs[1].Number, prs[2].Number})
}

func TestListPullRequestsFilterByRepo(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repo1 := insertTestRepo(t, d, "owner", "repo1")
	repo2 := insertTestRepo(t, d, "owner", "repo2")
	base := baseTime()

	insertTestMR(t, d, repo1, 1, "pr in repo1", base)
	insertTestMR(t, d, repo2, 1, "pr in repo2", base)

	prs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{RepoOwner: "owner", RepoName: "repo1"})
	require.NoError(t, err)
	require.Len(t, prs, 1)
	Assert.Equal(t, repo1, prs[0].RepoID)
}

func TestListPullRequestsFilterBySearch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	insertTestMR(t, d, repoID, 1, "add feature", base)
	insertTestMR(t, d, repoID, 2, "fix bug", base.Add(time.Hour))

	prs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{Search: "feature"})
	require.NoError(t, err)
	require.Len(t, prs, 1)
	Assert.Equal(t, 1, prs[0].Number)
}

func TestListPullRequestsFilterByKanban(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	id1 := insertTestMR(t, d, repoID, 1, "pr 1", base)
	id2 := insertTestMR(t, d, repoID, 2, "pr 2", base.Add(time.Hour))
	id3 := insertTestMR(t, d, repoID, 3, "pr 3", base.Add(2*time.Hour))

	// Set PR 2 to "reviewing".
	require.NoError(d.SetKanbanState(ctx, id2, "reviewing"))
	// Ensure kanban for PR 1 and 3 (status = "new").
	require.NoError(d.EnsureKanbanState(ctx, id1))
	require.NoError(d.EnsureKanbanState(ctx, id3))

	prs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{KanbanState: "reviewing"})
	require.NoError(err)
	require.Len(prs, 1)
	assert.Equal(2, prs[0].Number)
	assert.Equal("reviewing", prs[0].KanbanStatus)
}

func TestKanbanState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	prID := insertTestMR(t, d, repoID, 1, "pr", baseTime())

	// Before EnsureKanbanState, GetKanbanState returns nil.
	k, err := d.GetKanbanState(ctx, prID)
	require.NoError(err)
	assert.Nil(k)

	// EnsureKanbanState creates "new".
	require.NoError(d.EnsureKanbanState(ctx, prID))
	k, err = d.GetKanbanState(ctx, prID)
	require.NoError(err)
	require.NotNil(k)
	assert.Equal("new", k.Status)

	// SetKanbanState changes the status.
	require.NoError(d.SetKanbanState(ctx, prID, "reviewing"))
	k, _ = d.GetKanbanState(ctx, prID)
	require.NotNil(k)
	assert.Equal("reviewing", k.Status)

	// EnsureKanbanState does NOT overwrite an existing row.
	require.NoError(d.EnsureKanbanState(ctx, prID))
	k, _ = d.GetKanbanState(ctx, prID)
	require.NotNil(k)
	assert.Equal("reviewing", k.Status)
}

func TestPREvents(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	prID := insertTestMR(t, d, repoID, 1, "pr", baseTime())
	base := baseTime()

	events := []MREvent{
		{
			MergeRequestID: prID,
			EventType:      "comment",
			Author:         "alice",
			Summary:        "LGTM",
			CreatedAt:      base,
			DedupeKey:      "comment-1",
		},
		{
			MergeRequestID: prID,
			EventType:      "review",
			Author:         "bob",
			Summary:        "approved",
			CreatedAt:      base.Add(time.Hour),
			DedupeKey:      "review-1",
		},
	}

	require.NoError(d.UpsertMREvents(ctx, events))

	got, err := d.ListMREvents(ctx, prID)
	require.NoError(err)
	require.Len(got, 2)
	// Newest first.
	assert.Equal("review-1", got[0].DedupeKey)
	assert.Equal("comment-1", got[1].DedupeKey)

	// Inserting duplicate dedupe_key must be silently ignored.
	dup := []MREvent{
		{
			MergeRequestID: prID,
			EventType:      "comment",
			Author:         "alice",
			Summary:        "dupe",
			CreatedAt:      base,
			DedupeKey:      "comment-1",
		},
	}
	require.NoError(d.UpsertMREvents(ctx, dup))
	got2, _ := d.ListMREvents(ctx, prID)
	assert.Len(got2, 2)
}

func TestGetPRIDByRepoAndNumber(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestMR(t, d, repoID, 5, "pr five", baseTime())

	id, err := d.GetMRIDByRepoAndNumber(ctx, "o", "r", 5)
	require.NoError(t, err)
	Assert.NotZero(t, id)

	_, err = d.GetMRIDByRepoAndNumber(ctx, "o", "r", 999)
	require.Error(t, err)
}

func TestGetPreviouslyOpenPRNumbers(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	base := baseTime()
	insertTestMR(t, d, repoID, 1, "pr1", base)
	insertTestMR(t, d, repoID, 2, "pr2", base.Add(time.Hour))
	insertTestMR(t, d, repoID, 3, "pr3", base.Add(2*time.Hour))

	// PRs 1 and 3 are still open; 2 was closed externally.
	stillOpen := map[int]bool{1: true, 3: true}
	closed, err := d.GetPreviouslyOpenMRNumbers(ctx, repoID, stillOpen)
	require.NoError(t, err)
	Assert.Equal(t, []int{2}, closed)
}

func TestUpsertPullRequestMergeableState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	d := openTestDB(t)

	repoID := insertTestRepo(t, d, "acme", "widget")
	now := baseTime()
	pr := &MergeRequest{
		RepoID:         repoID,
		PlatformID:     9001,
		Number:         42,
		State:          "open",
		MergeableState: "dirty",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}

	_, err := d.UpsertMergeRequest(ctx, pr)
	require.NoError(err)

	got, err := d.GetMergeRequest(ctx, "acme", "widget", 42)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal("dirty", got.MergeableState)

	pr.MergeableState = "clean"
	_, err = d.UpsertMergeRequest(ctx, pr)
	require.NoError(err)

	got, err = d.GetMergeRequest(ctx, "acme", "widget", 42)
	require.NoError(err)
	assert.Equal("clean", got.MergeableState)
}

func TestRateLimitCRUD(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	host := "github.com"
	hourStart := baseTime()
	resetAt := hourStart.Add(30 * time.Minute)

	// Insert
	require.NoError(d.UpsertRateLimit(host, 5, hourStart, 4995, &resetAt))

	got, err := d.GetRateLimit(host)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(host, got.PlatformHost)
	assert.Equal(5, got.RequestsHour)
	assert.True(got.HourStart.Equal(hourStart))
	assert.Equal(4995, got.RateRemaining)
	require.NotNil(got.RateResetAt)
	assert.True(got.RateResetAt.Equal(resetAt))

	// Update via upsert
	laterStart := hourStart.Add(time.Hour)
	require.NoError(d.UpsertRateLimit(host, 10, laterStart, 4990, nil))

	got2, err := d.GetRateLimit(host)
	require.NoError(err)
	require.NotNil(got2)
	assert.Equal(10, got2.RequestsHour)
	assert.True(got2.HourStart.Equal(laterStart))
	assert.Equal(4990, got2.RateRemaining)
	assert.Nil(got2.RateResetAt)

	// Not found
	missing, err := d.GetRateLimit("no.such.host")
	require.NoError(err)
	assert.Nil(missing)
}

func TestUpdatePRState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestMR(t, d, repoID, 1, "pr", baseTime())

	mergedAt := baseTime().Add(time.Hour)
	require.NoError(d.UpdateMRState(ctx, repoID, 1, "merged", &mergedAt, nil))

	pr, err := d.GetMergeRequest(ctx, "o", "r", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("merged", pr.State)
	require.NotNil(pr.MergedAt)
	assert.True(pr.MergedAt.Equal(mergedAt))
}

func TestSetWorktreeLinks(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	repoID := insertTestRepo(t, d, "o", "r")
	mrID1 := insertTestMR(t, d, repoID, 1, "pr1", baseTime())
	mrID2 := insertTestMR(t, d, repoID, 2, "pr2", baseTime().Add(time.Hour))

	now := baseTime()
	links := []WorktreeLink{
		{MergeRequestID: mrID1, WorktreeKey: "wt-1", WorktreePath: "/tmp/wt1", WorktreeBranch: "feature-1", LinkedAt: now},
		{MergeRequestID: mrID2, WorktreeKey: "wt-2", WorktreePath: "/tmp/wt2", WorktreeBranch: "feature-2", LinkedAt: now.Add(time.Hour)},
	}
	require.NoError(d.SetWorktreeLinks(links))

	all, err := d.GetAllWorktreeLinks()
	require.NoError(err)
	require.Len(all, 2)
	// Newest first.
	assert.Equal("wt-2", all[0].WorktreeKey)
	assert.Equal("wt-1", all[1].WorktreeKey)
	assert.Equal("/tmp/wt1", all[1].WorktreePath)
	assert.Equal("feature-1", all[1].WorktreeBranch)

	// Bulk replace with a different set.
	replacement := []WorktreeLink{
		{MergeRequestID: mrID1, WorktreeKey: "wt-3", WorktreePath: "/tmp/wt3", WorktreeBranch: "hotfix", LinkedAt: now},
	}
	require.NoError(d.SetWorktreeLinks(replacement))

	all2, err := d.GetAllWorktreeLinks()
	require.NoError(err)
	require.Len(all2, 1)
	assert.Equal("wt-3", all2[0].WorktreeKey)
}

func TestGetWorktreeLinksForMR(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)

	repoID := insertTestRepo(t, d, "o", "r")
	mrID1 := insertTestMR(t, d, repoID, 1, "pr1", baseTime())
	mrID2 := insertTestMR(t, d, repoID, 2, "pr2", baseTime().Add(time.Hour))

	now := baseTime()
	links := []WorktreeLink{
		{MergeRequestID: mrID1, WorktreeKey: "wt-a", LinkedAt: now},
		{MergeRequestID: mrID2, WorktreeKey: "wt-b", LinkedAt: now},
	}
	require.NoError(d.SetWorktreeLinks(links))

	forMR1, err := d.GetWorktreeLinksForMR(mrID1)
	require.NoError(err)
	require.Len(forMR1, 1)
	assert.Equal("wt-a", forMR1[0].WorktreeKey)

	// Empty when no links for a given MR.
	forMR999, err := d.GetWorktreeLinksForMR(999)
	require.NoError(err)
	assert.Empty(forMR999)
}

func TestWorktreeLinksCascadeOnMRDelete(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	mrID := insertTestMR(t, d, repoID, 1, "pr1", baseTime())

	links := []WorktreeLink{
		{MergeRequestID: mrID, WorktreeKey: "wt-del", LinkedAt: baseTime()},
	}
	require.NoError(d.SetWorktreeLinks(links))

	all, err := d.GetAllWorktreeLinks()
	require.NoError(err)
	require.Len(all, 1)

	// Delete the MR; the ON DELETE CASCADE should remove the link.
	_, err = d.WriteDB().ExecContext(ctx,
		`DELETE FROM middleman_merge_requests WHERE id = ?`, mrID)
	require.NoError(err)

	after, err := d.GetAllWorktreeLinks()
	require.NoError(err)
	require.Empty(after)
}
