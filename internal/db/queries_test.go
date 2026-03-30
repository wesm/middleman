package db

import (
	"context"
	"testing"
	"time"
)

func baseTime() time.Time {
	return time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
}

func insertTestRepo(t *testing.T, d *DB, owner, name string) int64 {
	t.Helper()
	id, err := d.UpsertRepo(context.Background(), owner, name)
	if err != nil {
		t.Fatalf("UpsertRepo(%s/%s): %v", owner, name, err)
	}
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
	if err != nil {
		t.Fatalf("UpsertPullRequest %d: %v", number, err)
	}
	return id
}

func TestUpsertAndListRepos(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id1, err := d.UpsertRepo(ctx, "alice", "alpha")
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	id2, err := d.UpsertRepo(ctx, "bob", "beta")
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if id1 == id2 {
		t.Fatal("expected distinct IDs for different repos")
	}

	// Idempotency: re-inserting should return the same ID.
	id1Again, err := d.UpsertRepo(ctx, "alice", "alpha")
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if id1Again != id1 {
		t.Fatalf("re-upsert returned %d, want %d", id1Again, id1)
	}

	repos, err := d.ListRepos(ctx)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}
	// Ordered by owner, name: alice/alpha, bob/beta.
	if repos[0].Owner != "alice" || repos[0].Name != "alpha" {
		t.Errorf("repos[0] = %s/%s, want alice/alpha", repos[0].Owner, repos[0].Name)
	}
	if repos[1].Owner != "bob" || repos[1].Name != "beta" {
		t.Errorf("repos[1] = %s/%s, want bob/beta", repos[1].Owner, repos[1].Name)
	}
}

func TestGetRepoByOwnerName(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id := insertTestRepo(t, d, "owner", "repo")

	r, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	if err != nil {
		t.Fatalf("GetRepoByOwnerName: %v", err)
	}
	if r == nil {
		t.Fatal("expected repo, got nil")
	}
	if r.ID != id {
		t.Errorf("ID = %d, want %d", r.ID, id)
	}

	missing, err := d.GetRepoByOwnerName(ctx, "no", "such")
	if err != nil {
		t.Fatalf("GetRepoByOwnerName (missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing repo, got %+v", missing)
	}
}

func TestUpdateRepoSync(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id := insertTestRepo(t, d, "o", "r")
	now := baseTime()

	if err := d.UpdateRepoSyncStarted(ctx, id, now); err != nil {
		t.Fatalf("UpdateRepoSyncStarted: %v", err)
	}
	later := now.Add(time.Minute)
	if err := d.UpdateRepoSyncCompleted(ctx, id, later, ""); err != nil {
		t.Fatalf("UpdateRepoSyncCompleted: %v", err)
	}

	r, err := d.GetRepoByOwnerName(ctx, "o", "r")
	if err != nil || r == nil {
		t.Fatalf("GetRepoByOwnerName: %v %v", r, err)
	}
	if r.LastSyncStartedAt == nil || !r.LastSyncStartedAt.Equal(now) {
		t.Errorf("LastSyncStartedAt = %v, want %v", r.LastSyncStartedAt, now)
	}
	if r.LastSyncCompletedAt == nil || !r.LastSyncCompletedAt.Equal(later) {
		t.Errorf("LastSyncCompletedAt = %v, want %v", r.LastSyncCompletedAt, later)
	}
	if r.LastSyncError != "" {
		t.Errorf("LastSyncError = %q, want empty", r.LastSyncError)
	}

	// Record a sync error.
	if err := d.UpdateRepoSyncCompleted(ctx, id, later, "rate limited"); err != nil {
		t.Fatalf("UpdateRepoSyncCompleted with error: %v", err)
	}
	r2, _ := d.GetRepoByOwnerName(ctx, "o", "r")
	if r2.LastSyncError != "rate limited" {
		t.Errorf("LastSyncError = %q, want %q", r2.LastSyncError, "rate limited")
	}
}

func TestUpsertAndGetPullRequest(t *testing.T) {
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
	if err != nil {
		t.Fatalf("UpsertPullRequest: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := d.GetPullRequest(ctx, "owner", "repo", 7)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if got == nil {
		t.Fatal("expected PR, got nil")
	}
	if got.ID != id {
		t.Errorf("ID = %d, want %d", got.ID, id)
	}
	if got.Title != pr.Title {
		t.Errorf("Title = %q, want %q", got.Title, pr.Title)
	}
	if got.Author != pr.Author {
		t.Errorf("Author = %q, want %q", got.Author, pr.Author)
	}
	if got.Additions != pr.Additions {
		t.Errorf("Additions = %d, want %d", got.Additions, pr.Additions)
	}
	if got.KanbanStatus != "" {
		t.Errorf("KanbanStatus = %q, want empty before EnsureKanbanState", got.KanbanStatus)
	}

	// Update via upsert — change title and additions.
	pr.Title = "fix: something updated"
	pr.Additions = 20
	pr.UpdatedAt = now.Add(time.Hour)
	pr.LastActivityAt = now.Add(time.Hour)

	id2, err := d.UpsertPullRequest(ctx, pr)
	if err != nil {
		t.Fatalf("second UpsertPullRequest: %v", err)
	}
	if id2 != id {
		t.Errorf("second upsert returned different ID %d vs %d", id2, id)
	}

	got2, _ := d.GetPullRequest(ctx, "owner", "repo", 7)
	if got2.Title != "fix: something updated" {
		t.Errorf("Title not updated: %q", got2.Title)
	}
	if got2.Additions != 20 {
		t.Errorf("Additions not updated: %d", got2.Additions)
	}
	// created_at must not change.
	if !got2.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt changed: %v", got2.CreatedAt)
	}

	// Missing PR returns nil.
	missing, err := d.GetPullRequest(ctx, "owner", "repo", 999)
	if err != nil {
		t.Fatalf("GetPullRequest (missing): %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing PR, got %+v", missing)
	}
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
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("expected 3 PRs, got %d", len(prs))
	}
	// Newest first.
	if prs[0].Number != 3 || prs[1].Number != 2 || prs[2].Number != 1 {
		t.Errorf("order wrong: %d %d %d", prs[0].Number, prs[1].Number, prs[2].Number)
	}
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
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	if prs[0].RepoID != repo1 {
		t.Errorf("RepoID = %d, want %d", prs[0].RepoID, repo1)
	}
}

func TestListPullRequestsFilterBySearch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	insertTestPR(t, d, repoID, 1, "add feature", base)
	insertTestPR(t, d, repoID, 2, "fix bug", base.Add(time.Hour))

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{Search: "feature"})
	if err != nil {
		t.Fatalf("ListPullRequests search: %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 1 {
		t.Errorf("search filter wrong: got %d PRs", len(prs))
	}
}

func TestListPullRequestsFilterByKanban(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "owner", "repo")
	base := baseTime()

	id1 := insertTestPR(t, d, repoID, 1, "pr 1", base)
	id2 := insertTestPR(t, d, repoID, 2, "pr 2", base.Add(time.Hour))
	id3 := insertTestPR(t, d, repoID, 3, "pr 3", base.Add(2*time.Hour))

	// Set PR 2 to "reviewing".
	if err := d.SetKanbanState(ctx, id2, "reviewing"); err != nil {
		t.Fatalf("SetKanbanState: %v", err)
	}
	// Ensure kanban for PR 1 and 3 (status = "new").
	if err := d.EnsureKanbanState(ctx, id1); err != nil {
		t.Fatalf("EnsureKanbanState: %v", err)
	}
	if err := d.EnsureKanbanState(ctx, id3); err != nil {
		t.Fatalf("EnsureKanbanState: %v", err)
	}

	prs, err := d.ListPullRequests(ctx, ListPullsOpts{KanbanState: "reviewing"})
	if err != nil {
		t.Fatalf("ListPullRequests by kanban: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR with kanban=reviewing, got %d", len(prs))
	}
	if prs[0].Number != 2 {
		t.Errorf("expected PR #2, got #%d", prs[0].Number)
	}
	if prs[0].KanbanStatus != "reviewing" {
		t.Errorf("KanbanStatus = %q, want reviewing", prs[0].KanbanStatus)
	}
}

func TestKanbanState(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	prID := insertTestPR(t, d, repoID, 1, "pr", baseTime())

	// Before EnsureKanbanState, GetKanbanState returns nil.
	k, err := d.GetKanbanState(ctx, prID)
	if err != nil {
		t.Fatalf("GetKanbanState (before): %v", err)
	}
	if k != nil {
		t.Fatalf("expected nil before ensure, got %+v", k)
	}

	// EnsureKanbanState creates "new".
	if err := d.EnsureKanbanState(ctx, prID); err != nil {
		t.Fatalf("EnsureKanbanState: %v", err)
	}
	k, err = d.GetKanbanState(ctx, prID)
	if err != nil || k == nil {
		t.Fatalf("GetKanbanState after ensure: %v %v", k, err)
	}
	if k.Status != "new" {
		t.Errorf("status = %q, want new", k.Status)
	}

	// SetKanbanState changes the status.
	if err := d.SetKanbanState(ctx, prID, "reviewing"); err != nil {
		t.Fatalf("SetKanbanState: %v", err)
	}
	k, _ = d.GetKanbanState(ctx, prID)
	if k.Status != "reviewing" {
		t.Errorf("status = %q, want reviewing", k.Status)
	}

	// EnsureKanbanState does NOT overwrite an existing row.
	if err := d.EnsureKanbanState(ctx, prID); err != nil {
		t.Fatalf("second EnsureKanbanState: %v", err)
	}
	k, _ = d.GetKanbanState(ctx, prID)
	if k.Status != "reviewing" {
		t.Errorf("EnsureKanbanState overwrote status: got %q, want reviewing", k.Status)
	}
}

func TestPREvents(t *testing.T) {
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

	if err := d.UpsertPREvents(ctx, events); err != nil {
		t.Fatalf("UpsertPREvents: %v", err)
	}

	got, err := d.ListPREvents(ctx, prID)
	if err != nil {
		t.Fatalf("ListPREvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	// Newest first.
	if got[0].DedupeKey != "review-1" || got[1].DedupeKey != "comment-1" {
		t.Errorf("order wrong: %q %q", got[0].DedupeKey, got[1].DedupeKey)
	}

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
	if err := d.UpsertPREvents(ctx, dup); err != nil {
		t.Fatalf("UpsertPREvents (dup): %v", err)
	}
	got2, _ := d.ListPREvents(ctx, prID)
	if len(got2) != 2 {
		t.Errorf("expected still 2 events after dup, got %d", len(got2))
	}
}

func TestGetPRIDByRepoAndNumber(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestPR(t, d, repoID, 5, "pr five", baseTime())

	id, err := d.GetPRIDByRepoAndNumber(ctx, "o", "r", 5)
	if err != nil {
		t.Fatalf("GetPRIDByRepoAndNumber: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	_, err = d.GetPRIDByRepoAndNumber(ctx, "o", "r", 999)
	if err == nil {
		t.Fatal("expected error for missing PR")
	}
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
	if err != nil {
		t.Fatalf("GetPreviouslyOpenPRNumbers: %v", err)
	}
	if len(closed) != 1 || closed[0] != 2 {
		t.Errorf("expected [2], got %v", closed)
	}
}

func TestUpdatePRState(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	repoID := insertTestRepo(t, d, "o", "r")
	insertTestPR(t, d, repoID, 1, "pr", baseTime())

	mergedAt := baseTime().Add(time.Hour)
	if err := d.UpdatePRState(ctx, repoID, 1, "merged", &mergedAt, nil); err != nil {
		t.Fatalf("UpdatePRState: %v", err)
	}

	pr, err := d.GetPullRequest(ctx, "o", "r", 1)
	if err != nil || pr == nil {
		t.Fatalf("GetPullRequest after update: %v %v", pr, err)
	}
	if pr.State != "merged" {
		t.Errorf("State = %q, want merged", pr.State)
	}
	if pr.MergedAt == nil || !pr.MergedAt.Equal(mergedAt) {
		t.Errorf("MergedAt = %v, want %v", pr.MergedAt, mergedAt)
	}
}
