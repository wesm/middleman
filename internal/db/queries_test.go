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

func insertTestPR(t *testing.T, d *DB, repoID int64, number int, title string, activity time.Time) int64 {
	t.Helper()
	pr := &PullRequest{
		RepoID:         repoID,
		GitHubID:       repoID*10000 + int64(number),
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
	id, err := d.UpsertPullRequest(context.Background(), pr)
	require.NoErrorf(t, err, "UpsertPullRequest %d", number)
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

	pr := &PullRequest{
		RepoID:         repoID,
		GitHubID:       42,
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

	id, err := d.UpsertPullRequest(ctx, pr)
	require.NoError(err)
	assert.NotZero(id)

	got, err := d.GetPullRequest(ctx, "owner", "repo", 7)
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

	id2, err := d.UpsertPullRequest(ctx, pr)
	require.NoError(err)
	assert.Equal(id, id2)

	got2, _ := d.GetPullRequest(ctx, "owner", "repo", 7)
	require.NotNil(got2)
	assert.Equal("fix: something updated", got2.Title)
	assert.Equal(20, got2.Additions)
	// created_at must not change.
	assert.True(got2.CreatedAt.Equal(now))

	// Missing PR returns nil.
	missing, err := d.GetPullRequest(ctx, "owner", "repo", 999)
	require.NoError(err)
	assert.Nil(missing)
}

func TestListPullRequests(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	// Insert 3 PRs with different last_activity_at.
	insertTestPR(t, d, repoID, 1, "oldest", base)
	insertTestPR(t, d, repoID, 2, "middle", base.Add(time.Hour))
	insertTestPR(t, d, repoID, 3, "newest", base.Add(2*time.Hour))

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{})
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

	insertTestPR(t, d, repo1, 1, "pr in repo1", base)
	insertTestPR(t, d, repo2, 1, "pr in repo2", base)

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{RepoOwner: "owner", RepoName: "repo1"})
	require.NoError(t, err)
	require.Len(t, prs, 1)
	Assert.Equal(t, repo1, prs[0].RepoID)
}

func TestListPullRequestsFilterBySearch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	insertTestPR(t, d, repoID, 1, "add feature", base)
	insertTestPR(t, d, repoID, 2, "fix bug", base.Add(time.Hour))

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{Search: "feature"})
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

	id1 := insertTestPR(t, d, repoID, 1, "pr 1", base)
	id2 := insertTestPR(t, d, repoID, 2, "pr 2", base.Add(time.Hour))
	id3 := insertTestPR(t, d, repoID, 3, "pr 3", base.Add(2*time.Hour))

	// Set PR 2 to "reviewing".
	require.NoError(d.SetKanbanState(ctx, id2, "reviewing"))
	// Ensure kanban for PR 1 and 3 (status = "new").
	require.NoError(d.EnsureKanbanState(ctx, id1))
	require.NoError(d.EnsureKanbanState(ctx, id3))

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{KanbanState: "reviewing"})
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
	prID := insertTestPR(t, d, repoID, 1, "pr", baseTime())

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
	prID := insertTestPR(t, d, repoID, 1, "pr", baseTime())
	base := baseTime()

	events := []PREvent{
		{
			PRID:      prID,
			EventType: "comment",
			Author:    "alice",
			Summary:   "LGTM",
			CreatedAt: base,
			DedupeKey: "comment-1",
		},
		{
			PRID:      prID,
			EventType: "review",
			Author:    "bob",
			Summary:   "approved",
			CreatedAt: base.Add(time.Hour),
			DedupeKey: "review-1",
		},
	}

	require.NoError(d.UpsertPREvents(ctx, events))

	got, err := d.ListPREvents(ctx, prID)
	require.NoError(err)
	require.Len(got, 2)
	// Newest first.
	assert.Equal("review-1", got[0].DedupeKey)
	assert.Equal("comment-1", got[1].DedupeKey)

	// Inserting duplicate dedupe_key must be silently ignored.
	dup := []PREvent{
		{
			PRID:      prID,
			EventType: "comment",
			Author:    "alice",
			Summary:   "dupe",
			CreatedAt: base,
			DedupeKey: "comment-1",
		},
	}
	require.NoError(d.UpsertPREvents(ctx, dup))
	got2, _ := d.ListPREvents(ctx, prID)
	assert.Len(got2, 2)
}

func TestGetPRIDByRepoAndNumber(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestPR(t, d, repoID, 5, "pr five", baseTime())

	id, err := d.GetPRIDByRepoAndNumber(ctx, "o", "r", 5)
	require.NoError(t, err)
	Assert.NotZero(t, id)

	_, err = d.GetPRIDByRepoAndNumber(ctx, "o", "r", 999)
	require.Error(t, err)
}

func TestGetPreviouslyOpenPRNumbers(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	base := baseTime()
	insertTestPR(t, d, repoID, 1, "pr1", base)
	insertTestPR(t, d, repoID, 2, "pr2", base.Add(time.Hour))
	insertTestPR(t, d, repoID, 3, "pr3", base.Add(2*time.Hour))

	// PRs 1 and 3 are still open; 2 was closed externally.
	stillOpen := map[int]bool{1: true, 3: true}
	closed, err := d.GetPreviouslyOpenPRNumbers(ctx, repoID, stillOpen)
	require.NoError(t, err)
	Assert.Equal(t, []int{2}, closed)
}

func TestUpdatePRState(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestPR(t, d, repoID, 1, "pr", baseTime())

	mergedAt := baseTime().Add(time.Hour)
	require.NoError(d.UpdatePRState(ctx, repoID, 1, "merged", &mergedAt, nil))

	pr, err := d.GetPullRequest(ctx, "o", "r", 1)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("merged", pr.State)
	require.NotNil(pr.MergedAt)
	assert.True(pr.MergedAt.Equal(mergedAt))
}
