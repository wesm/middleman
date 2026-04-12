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
	id, err := d.UpsertRepo(context.Background(), "github.com", owner, name)
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

// insertTestRepoWithHost inserts a repo with a specific platform_host.
func insertTestRepoWithHost(
	t *testing.T, d *DB, owner, name, host string,
) int64 {
	t.Helper()
	ctx := context.Background()
	_, err := d.WriteDB().ExecContext(ctx,
		`INSERT INTO middleman_repos (platform, platform_host, owner, name)
		 VALUES ('github', ?, ?, ?)
		 ON CONFLICT(platform, platform_host, owner, name) DO NOTHING`,
		host, owner, name,
	)
	require.NoError(t, err)
	var id int64
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT id FROM middleman_repos
		 WHERE platform = 'github' AND platform_host = ?
		   AND owner = ? AND name = ?`,
		host, owner, name,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestPurgeOtherHosts(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	// Insert repos for two hosts.
	ghRepoID := insertTestRepoWithHost(
		t, d, "acme", "widget", "github.com",
	)
	gheRepoID := insertTestRepoWithHost(
		t, d, "corp", "internal", "ghes.company.com",
	)

	// Insert MRs for both hosts.
	ghMRID := insertTestMR(
		t, d, ghRepoID, 1, "gh PR", base,
	)
	gheMRID := insertTestMR(
		t, d, gheRepoID, 2, "ghe PR", base,
	)

	// Insert events for both MRs.
	require.NoError(d.UpsertMREvents(ctx, []MREvent{
		{
			MergeRequestID: ghMRID,
			EventType:      "comment",
			Author:         "alice",
			CreatedAt:      base,
			DedupeKey:      "gh-evt-1",
		},
	}))
	require.NoError(d.UpsertMREvents(ctx, []MREvent{
		{
			MergeRequestID: gheMRID,
			EventType:      "comment",
			Author:         "bob",
			CreatedAt:      base,
			DedupeKey:      "ghe-evt-1",
		},
	}))

	// Insert worktree links for both MRs.
	require.NoError(d.SetWorktreeLinks([]WorktreeLink{
		{
			MergeRequestID: ghMRID,
			WorktreeKey:    "wt-gh",
			LinkedAt:       base,
		},
		{
			MergeRequestID: gheMRID,
			WorktreeKey:    "wt-ghe",
			LinkedAt:       base,
		},
	}))

	// Insert starred items for both repos.
	require.NoError(d.SetStarred(ctx, "pr", ghRepoID, 1))
	require.NoError(d.SetStarred(ctx, "pr", gheRepoID, 2))

	// Insert rate limits for both hosts.
	require.NoError(d.UpsertRateLimit(
		"github.com", "rest", 10, base, 4990, -1, nil,
	))
	require.NoError(d.UpsertRateLimit(
		"ghes.company.com", "rest", 5, base, 4995, -1, nil,
	))

	// Purge all hosts except github.com.
	require.NoError(d.PurgeOtherHosts("github.com"))

	// github.com data should remain.
	repos, err := d.ListRepos(ctx)
	require.NoError(err)
	require.Len(repos, 1)
	assert.Equal("github.com", repos[0].PlatformHost)
	assert.Equal("acme", repos[0].Owner)

	// github.com MR should remain.
	ghMR, err := d.GetMergeRequest(ctx, "acme", "widget", 1)
	require.NoError(err)
	require.NotNil(ghMR)

	// github.com events should remain.
	ghEvents, err := d.ListMREvents(ctx, ghMRID)
	require.NoError(err)
	assert.Len(ghEvents, 1)

	// github.com worktree links should remain.
	ghLinks, err := d.GetWorktreeLinksForMR(ghMRID)
	require.NoError(err)
	assert.Len(ghLinks, 1)

	// github.com starred items should remain.
	starred, err := d.IsStarred(ctx, "pr", ghRepoID, 1)
	require.NoError(err)
	assert.True(starred)

	// ghes.company.com repo should be gone.
	var gheCount int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_repos
		 WHERE platform_host = 'ghes.company.com'`,
	).Scan(&gheCount)
	require.NoError(err)
	assert.Equal(0, gheCount)

	// ghes.company.com MR should be gone.
	var gheMRCount int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_merge_requests
		 WHERE repo_id = ?`, gheRepoID,
	).Scan(&gheMRCount)
	require.NoError(err)
	assert.Equal(0, gheMRCount)

	// ghes.company.com events should be gone.
	var gheEvtCount int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_mr_events
		 WHERE dedupe_key = 'ghe-evt-1'`,
	).Scan(&gheEvtCount)
	require.NoError(err)
	assert.Equal(0, gheEvtCount)

	// github.com rate limits should remain.
	ghRL, err := d.GetRateLimit("github.com", "rest")
	require.NoError(err)
	require.NotNil(ghRL)
	assert.Equal(10, ghRL.RequestsHour)

	// ghes.company.com rate limits should be gone.
	gheRL, err := d.GetRateLimit("ghes.company.com", "rest")
	require.NoError(err)
	assert.Nil(gheRL)
}

// TestCascadeDeleteRepo verifies that deleting a repo on a fresh DB
// cascades to all dependent tables (mr_events, kanban_state, issue_events).
func TestCascadeDeleteRepo(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")

	// Create MR with events and kanban state.
	mrID := insertTestMR(t, d, repoID, 1, "test PR", base)
	require.NoError(d.UpsertMREvents(ctx, []MREvent{
		{
			MergeRequestID: mrID,
			EventType:      "comment",
			Author:         "alice",
			CreatedAt:      base,
			DedupeKey:      "cascade-mr-evt",
		},
	}))
	require.NoError(d.SetKanbanState(ctx, mrID, "reviewing"))

	// Create issue with events.
	issueID := insertTestIssue(t, d, repoID, 10, "test issue", base)
	require.NoError(d.UpsertIssueEvents(ctx, []IssueEvent{
		{
			IssueID:   issueID,
			EventType: "comment",
			Author:    "bob",
			CreatedAt: base,
			DedupeKey: "cascade-issue-evt",
		},
	}))

	// Direct delete of the repo should cascade through all dependents.
	_, err := d.WriteDB().ExecContext(ctx,
		`DELETE FROM middleman_repos WHERE id = ?`, repoID,
	)
	require.NoError(err)

	// All dependent rows should be gone.
	var count int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_merge_requests`,
	).Scan(&count)
	require.NoError(err)
	assert.Equal(0, count)

	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_mr_events`,
	).Scan(&count)
	require.NoError(err)
	assert.Equal(0, count)

	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_kanban_state`,
	).Scan(&count)
	require.NoError(err)
	assert.Equal(0, count)

	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_issues`,
	).Scan(&count)
	require.NoError(err)
	assert.Equal(0, count)

	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_issue_events`,
	).Scan(&count)
	require.NoError(err)
	assert.Equal(0, count)
}

func TestUpsertAndListRepos(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	id1, err := d.UpsertRepo(ctx, "github.com", "alice", "alpha")
	require.NoError(err)
	id2, err := d.UpsertRepo(ctx, "github.com", "bob", "beta")
	require.NoError(err)
	assert.NotEqual(id1, id2)

	// Idempotency: re-inserting should return the same ID.
	id1Again, err := d.UpsertRepo(ctx, "github.com", "alice", "alpha")
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

func TestListMergeRequests_AttachesLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)

	_, err = d.UpsertMergeRequest(ctx, &MergeRequest{
		RepoID:         repoID,
		PlatformID:     101,
		Number:         7,
		URL:            "https://github.com/acme/widget/pull/7",
		Title:          "Add labels",
		Author:         "alice",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	mrID, err := d.GetMRIDByRepoAndNumber(ctx, "acme", "widget", 7)
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, mrID, []Label{{
		PlatformID:  5001,
		Name:        "needs-review",
		Description: "Needs another reviewer",
		Color:       "fbca04",
		IsDefault:   true,
		UpdatedAt:   now,
	}}))

	mrs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{})
	require.NoError(err)
	require.Len(mrs, 1)
	require.Len(mrs[0].Labels, 1)
	require.Equal("needs-review", mrs[0].Labels[0].Name)
	require.Equal("Needs another reviewer", mrs[0].Labels[0].Description)
	require.Equal("fbca04", mrs[0].Labels[0].Color)
	require.True(mrs[0].Labels[0].IsDefault)
	require.True(mrs[0].Labels[0].UpdatedAt.Equal(now))
}

func TestGetMergeRequest_AttachesLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")
	_, err := d.UpsertMergeRequest(ctx, &MergeRequest{
		RepoID:         repoID,
		PlatformID:     102,
		Number:         8,
		URL:            "https://github.com/acme/widget/pull/8",
		Title:          "Detail labels",
		Author:         "alice",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	mrID, err := d.GetMRIDByRepoAndNumber(ctx, "acme", "widget", 8)
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, mrID, []Label{{
		PlatformID:  5002,
		Name:        "backend",
		Description: "Backend changes",
		Color:       "5319e7",
		IsDefault:   false,
		UpdatedAt:   now,
	}}))

	mr, err := d.GetMergeRequest(ctx, "acme", "widget", 8)
	require.NoError(err)
	require.NotNil(mr)
	require.Len(mr.Labels, 1)
	require.Equal("backend", mr.Labels[0].Name)
	require.Equal("Backend changes", mr.Labels[0].Description)
	require.Equal("5319e7", mr.Labels[0].Color)
	require.False(mr.Labels[0].IsDefault)
	require.True(mr.Labels[0].UpdatedAt.Equal(now))
}

func TestReplaceMergeRequestLabels_RejectsWrongRepoID(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoA := insertTestRepo(t, d, "acme", "widget")
	repoB := insertTestRepo(t, d, "acme", "gadget")
	mrID := insertTestMR(t, d, repoA, 9, "repo guarded", now)

	err := d.ReplaceMergeRequestLabels(ctx, repoB, mrID, []Label{{
		PlatformID:  9009,
		Name:        "wrong-repo",
		Description: "should fail",
		Color:       "ff0000",
		UpdatedAt:   now,
	}})
	require.Error(err)

	mr, err := d.GetMergeRequest(ctx, "acme", "widget", 9)
	require.NoError(err)
	require.NotNil(mr)
	require.Empty(mr.Labels)
}

func TestUpsertLabels_UsesPlatformIDForRename(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")
	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  41,
		Name:        "old-name",
		Description: "before rename",
		Color:       "111111",
		UpdatedAt:   now,
	}}))
	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  41,
		Name:        "new-name",
		Description: "after rename",
		Color:       "222222",
		IsDefault:   true,
		UpdatedAt:   now.Add(time.Minute),
	}}))

	var count int
	err := d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_labels WHERE repo_id = ?`,
		repoID,
	).Scan(&count)
	require.NoError(err)
	require.Equal(1, count)

	var name, description, color string
	var isDefault bool
	var updatedAt time.Time
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT name, description, color, is_default, updated_at
		 FROM middleman_labels
		 WHERE repo_id = ? AND platform_id = ?`,
		repoID, 41,
	).Scan(&name, &description, &color, &isDefault, &updatedAt)
	require.NoError(err)
	require.Equal("new-name", name)
	require.Equal("after rename", description)
	require.Equal("222222", color)
	require.True(isDefault)
	require.True(updatedAt.Equal(now.Add(time.Minute)))
}

func TestUpsertLabels_MergesStaleNameOnlyRowIntoPlatformRow(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")
	mrID := insertTestMR(t, d, repoID, 17, "rename labels", now)
	issueID := insertTestIssue(t, d, repoID, 23, "rename labels", now)

	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  200,
		Name:        "old-name",
		Description: "old platform label",
		Color:       "111111",
		UpdatedAt:   now,
	}}))
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, mrID, []Label{{
		Name:        "new-name",
		Description: "stale name-only label",
		Color:       "222222",
		UpdatedAt:   now,
	}}))
	require.NoError(d.ReplaceIssueLabels(ctx, repoID, issueID, []Label{{
		Name:        "new-name",
		Description: "stale name-only label",
		Color:       "222222",
		UpdatedAt:   now,
	}}))

	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  200,
		Name:        "new-name",
		Description: "renamed label",
		Color:       "333333",
		IsDefault:   true,
		UpdatedAt:   now.Add(time.Minute),
	}}))

	var count int
	err := d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_labels WHERE repo_id = ?`,
		repoID,
	).Scan(&count)
	require.NoError(err)
	require.Equal(1, count)

	var labelID int64
	var platformID int64
	var name, description, color string
	var isDefault bool
	err = d.ReadDB().QueryRowContext(ctx, `
		SELECT id, platform_id, name, description, color, is_default
		FROM middleman_labels
		WHERE repo_id = ?`, repoID,
	).Scan(&labelID, &platformID, &name, &description, &color, &isDefault)
	require.NoError(err)
	require.Equal(int64(200), platformID)
	require.Equal("new-name", name)
	require.Equal("renamed label", description)
	require.Equal("333333", color)
	require.True(isDefault)

	mr, err := d.GetMergeRequest(ctx, "acme", "widget", 17)
	require.NoError(err)
	require.NotNil(mr)
	require.Len(mr.Labels, 1)
	require.Equal(labelID, mr.Labels[0].ID)
	require.Equal("new-name", mr.Labels[0].Name)

	issue, err := d.GetIssue(ctx, "acme", "widget", 23)
	require.NoError(err)
	require.NotNil(issue)
	require.Len(issue.Labels, 1)
	require.Equal(labelID, issue.Labels[0].ID)
	require.Equal("new-name", issue.Labels[0].Name)
}

func TestUpsertLabels_RejectsAmbiguousNameAndPlatformIDMatch(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")
	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  100,
		Name:        "bug",
		Description: "by name",
		Color:       "111111",
		UpdatedAt:   now,
	}}))
	require.NoError(d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  200,
		Name:        "renamed",
		Description: "by platform",
		Color:       "222222",
		UpdatedAt:   now,
	}}))

	err := d.UpsertLabels(ctx, repoID, []Label{{
		PlatformID:  200,
		Name:        "bug",
		Description: "ambiguous",
		Color:       "333333",
		UpdatedAt:   now.Add(time.Minute),
	}})
	require.Error(err)
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

func TestMREventsDedupeIsScopedToMergeRequest(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	repoID := insertTestRepo(t, d, "o", "r")
	firstMRID := insertTestMR(t, d, repoID, 1, "pr one", base)
	secondMRID := insertTestMR(t, d, repoID, 2, "pr two", base.Add(time.Minute))

	sharedDedupeKey := "force-push-before-sha-after-sha"
	require.NoError(d.UpsertMREvents(ctx, []MREvent{
		{
			MergeRequestID: firstMRID,
			EventType:      "force_push",
			Author:         "alice",
			CreatedAt:      base,
			DedupeKey:      sharedDedupeKey,
		},
		{
			MergeRequestID: secondMRID,
			EventType:      "force_push",
			Author:         "bob",
			CreatedAt:      base.Add(time.Minute),
			DedupeKey:      sharedDedupeKey,
		},
	}))

	firstEvents, err := d.ListMREvents(ctx, firstMRID)
	require.NoError(err)
	require.Len(firstEvents, 1)
	assert.Equal(sharedDedupeKey, firstEvents[0].DedupeKey)

	secondEvents, err := d.ListMREvents(ctx, secondMRID)
	require.NoError(err)
	require.Len(secondEvents, 1)
	assert.Equal(sharedDedupeKey, secondEvents[0].DedupeKey)

	var total int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_mr_events WHERE dedupe_key = ?`,
		sharedDedupeKey,
	).Scan(&total)
	require.NoError(err)
	assert.Equal(2, total)
}

func TestListMREventsHandlesNonUTCTimes(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	prID := insertTestMR(t, d, repoID, 1, "pr one", baseTime())

	// Insert events with times in various non-UTC zones,
	// reproducing the formats the sqlite driver stores.
	//nolint:forbidigo // Test fixtures intentionally use non-UTC zones to verify normalization.
	edt := time.FixedZone("EDT", -4*3600)
	//nolint:forbidigo // Test fixtures intentionally use non-UTC zones to verify normalization.
	cdt := time.FixedZone("CDT", -5*3600)
	//nolint:forbidigo // Test fixtures intentionally use non-UTC zones to verify normalization.
	jst := time.FixedZone("JST", 9*3600)
	zones := []struct {
		key  string
		zone *time.Location
	}{
		{"commit-utc", time.UTC},
		{"commit-edt", edt},
		{"commit-cdt", cdt},
		{"commit-jst", jst},
	}
	var events []MREvent
	base := baseTime()
	for i, z := range zones {
		events = append(events, MREvent{
			MergeRequestID: prID,
			EventType:      "commit",
			Author:         "alice",
			CreatedAt:      base.Add(time.Duration(i) * time.Hour).In(z.zone),
			DedupeKey:      z.key,
		})
	}
	require.NoError(d.UpsertMREvents(ctx, events))

	got, err := d.ListMREvents(ctx, prID)
	require.NoError(err)
	require.Len(got, len(zones))

	for _, e := range got {
		assert.Equal(time.UTC, e.CreatedAt.Location(),
			"event %s should be returned in UTC", e.DedupeKey)
	}
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

	// Insert REST
	require.NoError(d.UpsertRateLimit(host, "rest", 5, hourStart, 4995, -1, &resetAt))

	got, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(host, got.PlatformHost)
	assert.Equal("rest", got.APIType)
	assert.Equal(5, got.RequestsHour)
	assert.True(got.HourStart.Equal(hourStart))
	assert.Equal(4995, got.RateRemaining)
	require.NotNil(got.RateResetAt)
	assert.True(got.RateResetAt.Equal(resetAt))

	// Insert GraphQL for same host — separate row
	require.NoError(d.UpsertRateLimit(host, "graphql", 2, hourStart, 4998, 5000, nil))

	gql, err := d.GetRateLimit(host, "graphql")
	require.NoError(err)
	require.NotNil(gql)
	assert.Equal("graphql", gql.APIType)
	assert.Equal(2, gql.RequestsHour)
	assert.Equal(4998, gql.RateRemaining)

	// REST row unchanged
	rest, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(rest)
	assert.Equal(5, rest.RequestsHour)

	// Update via upsert
	laterStart := hourStart.Add(time.Hour)
	require.NoError(d.UpsertRateLimit(host, "rest", 10, laterStart, 4990, -1, nil))

	got2, err := d.GetRateLimit(host, "rest")
	require.NoError(err)
	require.NotNil(got2)
	assert.Equal(10, got2.RequestsHour)
	assert.True(got2.HourStart.Equal(laterStart))
	assert.Equal(4990, got2.RateRemaining)
	assert.Nil(got2.RateResetAt)

	// Not found
	missing, err := d.GetRateLimit("no.such.host", "rest")
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

func TestListIssues_AttachesLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)

	issueID, err := d.UpsertIssue(ctx, &Issue{
		RepoID:         repoID,
		PlatformID:     201,
		Number:         3,
		URL:            "https://github.com/acme/widget/issues/3",
		Title:          "Bug",
		Author:         "bob",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)
	require.NoError(d.ReplaceIssueLabels(ctx, repoID, issueID, []Label{{
		PlatformID:  11,
		Name:        "bug",
		Description: "Something is broken",
		Color:       "d73a4a",
		IsDefault:   true,
		UpdatedAt:   now,
	}}))

	issues, err := d.ListIssues(ctx, ListIssuesOpts{})
	require.NoError(err)
	require.Len(issues, 1)
	require.Len(issues[0].Labels, 1)
	require.Equal("bug", issues[0].Labels[0].Name)
	require.Equal("Something is broken", issues[0].Labels[0].Description)
	require.Equal("d73a4a", issues[0].Labels[0].Color)
	require.True(issues[0].Labels[0].IsDefault)
	require.True(issues[0].Labels[0].UpdatedAt.Equal(now))
}

func TestGetIssue_AttachesLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoID := insertTestRepo(t, d, "acme", "widget")
	issueID, err := d.UpsertIssue(ctx, &Issue{
		RepoID:         repoID,
		PlatformID:     202,
		Number:         4,
		URL:            "https://github.com/acme/widget/issues/4",
		Title:          "Docs",
		Author:         "bob",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)
	require.NoError(d.ReplaceIssueLabels(ctx, repoID, issueID, []Label{{
		PlatformID:  12,
		Name:        "documentation",
		Description: "Docs updates",
		Color:       "0075ca",
		IsDefault:   false,
		UpdatedAt:   now,
	}}))

	issue, err := d.GetIssue(ctx, "acme", "widget", 4)
	require.NoError(err)
	require.NotNil(issue)
	require.Len(issue.Labels, 1)
	require.Equal("documentation", issue.Labels[0].Name)
	require.Equal("Docs updates", issue.Labels[0].Description)
	require.Equal("0075ca", issue.Labels[0].Color)
	require.False(issue.Labels[0].IsDefault)
	require.True(issue.Labels[0].UpdatedAt.Equal(now))
}

func TestReplaceIssueLabels_RejectsWrongRepoID(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoA := insertTestRepo(t, d, "acme", "widget")
	repoB := insertTestRepo(t, d, "acme", "gadget")
	issueID, err := d.UpsertIssue(ctx, &Issue{
		RepoID:         repoA,
		PlatformID:     204,
		Number:         6,
		URL:            "https://github.com/acme/widget/issues/6",
		Title:          "repo guarded issue",
		Author:         "bob",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	err = d.ReplaceIssueLabels(ctx, repoB, issueID, []Label{{
		PlatformID:  220,
		Name:        "wrong-repo",
		Description: "should fail",
		Color:       "ff0000",
		UpdatedAt:   now,
	}})
	require.Error(err)

	issue, err := d.GetIssue(ctx, "acme", "widget", 6)
	require.NoError(err)
	require.NotNil(issue)
	require.Empty(issue.Labels)
}

func TestListIssues_UsesRepoScopedLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	now := baseTime()

	repoA, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	repoB, err := d.UpsertRepo(ctx, "github.com", "acme", "gadget")
	require.NoError(err)

	issueID, err := d.UpsertIssue(ctx, &Issue{
		RepoID:         repoA,
		PlatformID:     203,
		Number:         5,
		URL:            "https://github.com/acme/widget/issues/5",
		Title:          "Repo scoped bug",
		Author:         "bob",
		State:          "open",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	})
	require.NoError(err)

	require.NoError(d.ReplaceIssueLabels(ctx, repoA, issueID, []Label{{
		PlatformID:  21,
		Name:        "bug",
		Description: "Widget bug",
		Color:       "d73a4a",
		UpdatedAt:   now,
	}}))
	require.NoError(d.UpsertLabels(ctx, repoB, []Label{{
		PlatformID:  22,
		Name:        "bug",
		Description: "Gadget bug",
		Color:       "0e8a16",
		UpdatedAt:   now,
	}}))

	issues, err := d.ListIssues(ctx, ListIssuesOpts{})
	require.NoError(err)
	require.Len(issues, 1)
	require.Len(issues[0].Labels, 1)
	require.Equal("bug", issues[0].Labels[0].Name)
	require.Equal("d73a4a", issues[0].Labels[0].Color)
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
