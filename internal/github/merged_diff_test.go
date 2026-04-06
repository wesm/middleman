package github

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
)

// gitRun runs a git command in the given dir and returns trimmed stdout.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

// initTestRepo creates a fresh git repo in dir with one initial commit on main.
func initTestRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, "", "init", "-b", "main", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644))
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial commit")
}

// setupBareClone creates a bare clone of sourceDir under clonesDir/owner/name.git,
// adds the pull refspec, and fetches.
func setupBareClone(t *testing.T, sourceDir, clonesDir, owner, name string) *gitclone.Manager {
	t.Helper()
	mgr := gitclone.New(clonesDir, "")
	barePath := mgr.ClonePath(owner, name)
	gitRun(t, "", "clone", "--bare", sourceDir, barePath)
	gitRun(t, barePath, "config", "--add", "remote.origin.fetch",
		"+refs/pull/*/head:refs/pull/*/head")
	gitRun(t, barePath, "remote", "set-url", "origin", sourceDir)
	gitRun(t, barePath, "fetch", "--prune", "origin")
	return mgr
}

// setupSyncer creates a syncer with a real DB, bare clone manager, and mock client.
// It runs one sync cycle to create the repo row, then returns the syncer and repo ID.
func setupSyncer(t *testing.T, ctx context.Context, mgr *gitclone.Manager) (*Syncer, int64) {
	t.Helper()
	d := openTestDB(t)
	mc := &mockClient{}
	syncer := NewSyncer(mc, d, mgr, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute, nil, "")
	syncer.RunOnce(ctx) // creates repo row
	repoRow, err := d.GetRepoByOwnerName(ctx, "owner", "repo")
	require.NoError(t, err)
	require.NotNil(t, repoRow)
	return syncer, repoRow.ID
}

// insertMergedPR inserts a merged PR with empty diff SHAs into the DB.
func insertMergedPR(t *testing.T, ctx context.Context, d *db.DB, repoID int64, number int, headSHA string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:          repoID,
		Number:          number,
		Title:           fmt.Sprintf("test PR #%d", number),
		State:           "merged",
		URL:             fmt.Sprintf("https://github.com/owner/repo/pull/%d", number),
		PlatformHeadSHA: headSHA,
		UpdatedAt:       now,
		CreatedAt:       now,
	})
	require.NoError(t, err)
}

func TestComputeMergedPRDiffSHAs_MergeCommit(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	// Create repo: initial commit on main.
	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Feature branch with a change.
	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "feature.txt"), []byte("feature\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add feature")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Merge commit on main (--no-ff ensures a merge commit).
	gitRun(t, sourceDir, "checkout", "main")
	gitRun(t, sourceDir, "merge", "--no-ff", "feature", "-m", "Merge feature")
	mergeCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Create pull ref.
	gitRun(t, sourceDir, "update-ref", "refs/pull/1/head", prHead)

	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")
	syncer, repoID := setupSyncer(t, ctx, mgr)
	insertMergedPR(t, ctx, syncer.db, repoID, 1, prHead)

	syncer.computeMergedMRDiffSHAs(ctx, RepoRef{Owner: "owner", Name: "repo"}, repoID, 1, mergeCommit, false)

	shas, err := syncer.db.GetDiffSHAs(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal(prHead, shas.DiffHeadSHA, "diff_head_sha should be the PR head")
	assert.Equal(forkPoint, shas.DiffBaseSHA, "diff_base_sha should be the fork point")
	assert.Equal(forkPoint, shas.MergeBaseSHA, "merge_base_sha should be the fork point")
	assert.NotEqual(prHead, shas.MergeBaseSHA, "diff should not be empty")
}

func TestComputeMergedPRDiffSHAs_ForceOverwritesIncorrectSHAs(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "feature.txt"), []byte("feature\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add feature")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "checkout", "main")
	gitRun(t, sourceDir, "merge", "--no-ff", "feature", "-m", "Merge feature")
	mergeCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "update-ref", "refs/pull/1/head", prHead)

	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")
	syncer, repoID := setupSyncer(t, ctx, mgr)
	insertMergedPR(t, ctx, syncer.db, repoID, 1, prHead)

	// Seed incorrect diff SHAs (simulating prior SyncMR regression).
	require.NoError(syncer.db.UpdateDiffSHAs(ctx, repoID, 1, "bad-head", "bad-base", "bad-merge-base"))

	// Without force, the existing (incorrect) SHAs are preserved.
	syncer.computeMergedMRDiffSHAs(ctx, RepoRef{Owner: "owner", Name: "repo"}, repoID, 1, mergeCommit, false)
	shas, err := syncer.db.GetDiffSHAs(ctx, "owner", "repo", 1)
	require.NoError(err)
	assert.Equal("bad-head", shas.DiffHeadSHA, "force=false should not overwrite existing SHAs")

	// With force, the incorrect SHAs are replaced with correct values.
	syncer.computeMergedMRDiffSHAs(ctx, RepoRef{Owner: "owner", Name: "repo"}, repoID, 1, mergeCommit, true)
	shas, err = syncer.db.GetDiffSHAs(ctx, "owner", "repo", 1)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal(prHead, shas.DiffHeadSHA, "force=true should overwrite with correct PR head")
	assert.Equal(forkPoint, shas.DiffBaseSHA, "force=true should overwrite with correct fork point")
	assert.Equal(forkPoint, shas.MergeBaseSHA, "force=true should overwrite with correct merge base")
}

func TestComputeMergedPRDiffSHAs_SquashMerge(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "feature.txt"), []byte("feature\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add feature")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Squash merge.
	gitRun(t, sourceDir, "checkout", "main")
	gitRun(t, sourceDir, "merge", "--squash", "feature")
	gitRun(t, sourceDir, "commit", "-m", "squash: add feature")
	squashCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "update-ref", "refs/pull/2/head", prHead)

	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")
	syncer, repoID := setupSyncer(t, ctx, mgr)
	insertMergedPR(t, ctx, syncer.db, repoID, 2, prHead)

	syncer.computeMergedMRDiffSHAs(ctx, RepoRef{Owner: "owner", Name: "repo"}, repoID, 2, squashCommit, false)

	shas, err := syncer.db.GetDiffSHAs(ctx, "owner", "repo", 2)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal(prHead, shas.DiffHeadSHA)
	assert.Equal(forkPoint, shas.DiffBaseSHA)
	assert.Equal(forkPoint, shas.MergeBaseSHA)
	assert.NotEqual(prHead, shas.MergeBaseSHA)
}

func TestComputeMergedPRDiffSHAs_RebaseMerge(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Feature branch with two commits.
	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "a.txt"), []byte("a\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add a")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "b.txt"), []byte("b\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add b")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Advance main so rebase produces new SHAs.
	gitRun(t, sourceDir, "checkout", "main")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "main.txt"), []byte("main\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "advance main")

	// Simulate rebase merge.
	gitRun(t, sourceDir, "checkout", "feature")
	gitRun(t, sourceDir, "rebase", "main")
	gitRun(t, sourceDir, "checkout", "main")
	gitRun(t, sourceDir, "merge", "--ff-only", "feature")
	rebaseLastCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Pull ref points to original (pre-rebase) PR head.
	gitRun(t, sourceDir, "update-ref", "refs/pull/3/head", prHead)

	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")
	syncer, repoID := setupSyncer(t, ctx, mgr)
	insertMergedPR(t, ctx, syncer.db, repoID, 3, prHead)

	syncer.computeMergedMRDiffSHAs(ctx, RepoRef{Owner: "owner", Name: "repo"}, repoID, 3, rebaseLastCommit, false)

	shas, err := syncer.db.GetDiffSHAs(ctx, "owner", "repo", 3)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal(prHead, shas.DiffHeadSHA)
	assert.Equal(forkPoint, shas.DiffBaseSHA)
	assert.Equal(forkPoint, shas.MergeBaseSHA,
		"merge-base should be the original fork point, not the advanced main commit")
	assert.NotEqual(prHead, shas.MergeBaseSHA)
}

// TestSyncOpenToMergedTransition is an end-to-end test that exercises the full
// open -> merged transition through RunOnce. First sync inserts the PR as open
// (before merge); second sync discovers it missing from ListOpenPullRequests,
// calls fetchAndUpdateClosed, and computes diff SHAs via the merged-PR path.
func TestSyncOpenToMergedTransition(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	// Create repo with initial commit.
	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Create feature branch with a change.
	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "feature.txt"), []byte("feature\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add feature")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Snapshot the base SHA while PR is still "open" (before merge).
	gitRun(t, sourceDir, "checkout", "main")
	premergeBaseSHA := gitRun(t, sourceDir, "rev-parse", "HEAD")

	// Create pull ref before merging (as GitHub does for open PRs).
	gitRun(t, sourceDir, "update-ref", "refs/pull/10/head", prHead)

	// Set up bare clone BEFORE the merge -- reflects the pre-merge state.
	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")

	now := time.Now().UTC()
	number := 10
	title := "test merged PR"
	url := "https://github.com/owner/repo/pull/10"
	ghID := int64(10000)
	headRef := "feature"
	baseRef := "main"
	openState := "open"

	openPR := &gh.PullRequest{
		ID:        &ghID,
		Number:    &number,
		Title:     &title,
		HTMLURL:   &url,
		State:     &openState,
		UpdatedAt: makeTimestamp(now),
		CreatedAt: makeTimestamp(now),
		Head: &gh.PullRequestBranch{
			Ref: &headRef,
			SHA: &prHead,
		},
		Base: &gh.PullRequestBranch{
			Ref: &baseRef,
			SHA: &premergeBaseSHA,
		},
	}

	// First sync: PR is open. Diff SHAs should be computed from the
	// pre-merge state (correct fork point).
	mc := &mockClient{
		openPRs: []*gh.PullRequest{openPR},
	}

	d := openTestDB(t)
	syncer := NewSyncer(mc, d, mgr, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute, nil, "")
	syncer.RunOnce(ctx)

	pr, err := d.GetMergeRequest(ctx, "owner", "repo", number)
	require.NoError(err)
	require.NotNil(pr)
	assert.Equal("open", pr.State)

	// Verify diff SHAs were computed during open-state sync.
	shasBeforeMerge, err := d.GetDiffSHAs(ctx, "owner", "repo", number)
	require.NoError(err)
	require.NotNil(shasBeforeMerge)
	assert.Equal(prHead, shasBeforeMerge.DiffHeadSHA, "open-state diff_head_sha")
	assert.Equal(forkPoint, shasBeforeMerge.DiffBaseSHA, "open-state diff_base_sha")
	assert.Equal(forkPoint, shasBeforeMerge.MergeBaseSHA, "open-state merge_base_sha")

	// Now perform the merge in the source repo.
	gitRun(t, sourceDir, "merge", "--no-ff", "feature", "-m", "Merge feature")
	mergeCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")
	postmergeBaseSHA := gitRun(t, sourceDir, "rev-parse", "main")

	// Re-fetch the bare clone to pick up the merge commit.
	barePath := mgr.ClonePath("owner", "repo")
	gitRun(t, barePath, "fetch", "--prune", "origin")

	closedState := "closed"
	merged := true
	mergedPR := &gh.PullRequest{
		ID:             &ghID,
		Number:         &number,
		Title:          &title,
		HTMLURL:        &url,
		State:          &closedState,
		Merged:         &merged,
		MergeCommitSHA: &mergeCommit,
		UpdatedAt:      makeTimestamp(now.Add(time.Minute)),
		CreatedAt:      makeTimestamp(now),
		MergedAt:       makeTimestamp(now.Add(time.Minute)),
		ClosedAt:       makeTimestamp(now.Add(time.Minute)),
		Head: &gh.PullRequestBranch{
			Ref: &headRef,
			SHA: &prHead,
		},
		Base: &gh.PullRequestBranch{
			Ref: &baseRef,
			SHA: &postmergeBaseSHA,
		},
	}

	// Second sync: PR disappeared from open list, GetPullRequest returns merged.
	mc.openPRs = nil
	mc.singlePR = mergedPR
	syncer.RunOnce(ctx)

	// Verify PR transitioned to merged state.
	shas, err := d.GetDiffSHAs(ctx, "owner", "repo", number)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal("merged", shas.State, "PR state should be merged")
	// Diff SHAs should still reflect the correct fork point (either preserved
	// from open-state sync or recomputed correctly by the merged path).
	assert.Equal(prHead, shas.DiffHeadSHA, "diff_head_sha should be the PR head")
	assert.Equal(forkPoint, shas.DiffBaseSHA, "diff_base_sha should be the fork point")
	assert.Equal(forkPoint, shas.MergeBaseSHA, "merge_base_sha should be the fork point")
	assert.NotEqual(prHead, shas.MergeBaseSHA, "diff should not be empty")
}

// TestSyncFirstSeenMergedPR exercises the first-seen merged PR path through
// RunOnce. The PR is inserted as open WITHOUT a clone manager (no diff SHAs
// computed), then on the second sync (with clone manager) it transitions to
// merged and computeMergedMRDiffSHAs must fill in the diff SHAs.
func TestSyncFirstSeenMergedPR(t *testing.T) {
	require := require.New(t)
	assert := assert.New(t)

	ctx := context.Background()
	sourceDir := t.TempDir()
	clonesDir := t.TempDir()

	initTestRepo(t, sourceDir)
	forkPoint := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "checkout", "-b", "feature")
	require.NoError(os.WriteFile(filepath.Join(sourceDir, "feature.txt"), []byte("feature\n"), 0o644))
	gitRun(t, sourceDir, "add", ".")
	gitRun(t, sourceDir, "commit", "-m", "add feature")
	prHead := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "checkout", "main")
	premergeBaseSHA := gitRun(t, sourceDir, "rev-parse", "HEAD")

	gitRun(t, sourceDir, "update-ref", "refs/pull/10/head", prHead)

	now := time.Now().UTC()
	number := 10
	title := "test PR"
	url := "https://github.com/owner/repo/pull/10"
	ghID := int64(10000)
	headRef := "feature"
	baseRef := "main"
	openState := "open"

	openPR := &gh.PullRequest{
		ID:        &ghID,
		Number:    &number,
		Title:     &title,
		HTMLURL:   &url,
		State:     &openState,
		UpdatedAt: makeTimestamp(now),
		CreatedAt: makeTimestamp(now),
		Head:      &gh.PullRequestBranch{Ref: &headRef, SHA: &prHead},
		Base:      &gh.PullRequestBranch{Ref: &baseRef, SHA: &premergeBaseSHA},
	}

	mc := &mockClient{openPRs: []*gh.PullRequest{openPR}}

	// First sync WITHOUT clone manager -- PR inserted as open, no diff SHAs.
	d := openTestDB(t)
	syncer := NewSyncer(mc, d, nil, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute, nil, "")
	syncer.RunOnce(ctx)

	shasEmpty, err := d.GetDiffSHAs(ctx, "owner", "repo", number)
	require.NoError(err)
	require.NotNil(shasEmpty)
	assert.Empty(shasEmpty.DiffHeadSHA, "diff SHAs should be empty without clone manager")

	// Merge the PR.
	gitRun(t, sourceDir, "merge", "--no-ff", "feature", "-m", "Merge feature")
	mergeCommit := gitRun(t, sourceDir, "rev-parse", "HEAD")
	postmergeBaseSHA := gitRun(t, sourceDir, "rev-parse", "main")

	// Set up bare clone (post-merge state).
	mgr := setupBareClone(t, sourceDir, clonesDir, "owner", "repo")

	closedState := "closed"
	merged := true
	mergedPR := &gh.PullRequest{
		ID:             &ghID,
		Number:         &number,
		Title:          &title,
		HTMLURL:        &url,
		State:          &closedState,
		Merged:         &merged,
		MergeCommitSHA: &mergeCommit,
		UpdatedAt:      makeTimestamp(now.Add(time.Minute)),
		CreatedAt:      makeTimestamp(now),
		MergedAt:       makeTimestamp(now.Add(time.Minute)),
		ClosedAt:       makeTimestamp(now.Add(time.Minute)),
		Head:           &gh.PullRequestBranch{Ref: &headRef, SHA: &prHead},
		Base:           &gh.PullRequestBranch{Ref: &baseRef, SHA: &postmergeBaseSHA},
	}

	// Second sync WITH clone manager via a new syncer sharing the same DB.
	// PR transitions to merged, computeMergedMRDiffSHAs must run since
	// diff SHAs are empty.
	mc.openPRs = nil
	mc.singlePR = mergedPR
	syncer2 := NewSyncer(mc, d, mgr, []RepoRef{{Owner: "owner", Name: "repo"}}, time.Minute, nil, "")
	syncer2.RunOnce(ctx)

	shas, err := d.GetDiffSHAs(ctx, "owner", "repo", number)
	require.NoError(err)
	require.NotNil(shas)
	assert.Equal("merged", shas.State)
	assert.Equal(prHead, shas.DiffHeadSHA, "diff_head_sha should be the PR head")
	assert.Equal(forkPoint, shas.DiffBaseSHA, "diff_base_sha should be the fork point")
	assert.Equal(forkPoint, shas.MergeBaseSHA, "merge_base_sha should be the fork point")
	assert.NotEqual(prHead, shas.MergeBaseSHA, "diff should not be empty")
}
