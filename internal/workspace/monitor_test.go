package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/db"
)

func setupMonitorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")

	runWorkspaceTestGit(
		t, dir, "init", "--bare", "--initial-branch=main", remote,
	)
	runWorkspaceTestGit(t, dir, "clone", remote, work)
	runWorkspaceTestGit(
		t, work, "config", "user.email", "test@test.com",
	)
	runWorkspaceTestGit(
		t, work, "config", "user.name", "Test",
	)
	require.NoError(t, os.WriteFile(
		filepath.Join(work, "base.txt"), []byte("base\n"), 0o644,
	))
	runWorkspaceTestGit(t, work, "add", ".")
	runWorkspaceTestGit(t, work, "commit", "-m", "base commit")
	runWorkspaceTestGit(t, work, "push", "origin", "main")

	return work
}

func insertMonitorWorkspace(
	t *testing.T,
	d *db.DB,
	worktreePath string,
	associatedPRNumber *int,
) string {
	t.Helper()
	ws := &db.Workspace{
		ID:                 "ws-issue",
		PlatformHost:       "github.com",
		RepoOwner:          "acme",
		RepoName:           "widget",
		ItemType:           db.WorkspaceItemTypeIssue,
		ItemNumber:         7,
		GitHeadRef:         "middleman/issue-7",
		AssociatedPRNumber: associatedPRNumber,
		WorktreePath:       worktreePath,
		TmuxSession:        "middleman-ws-issue",
		Status:             "ready",
	}
	require.NoError(t, d.InsertWorkspace(context.Background(), ws))
	return ws.ID
}

func TestPRMonitorRunOnceUsesUpstreamBranchMatch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMRWithFork(
		t, d, repoID, 42,
		"feature/issue-7", "https://github.com/acme/widget.git",
	)

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "feature/issue-7")
	require.NoError(os.WriteFile(
		filepath.Join(worktreePath, "feature.txt"), []byte("feature\n"), 0o644,
	))
	runWorkspaceTestGit(t, worktreePath, "add", ".")
	runWorkspaceTestGit(t, worktreePath, "commit", "-m", "feature commit")
	runWorkspaceTestGit(t, worktreePath, "push", "-u", "origin", "feature/issue-7")
	runWorkspaceTestGit(
		t, worktreePath,
		"remote", "set-url", "origin", "git@github.com:acme/widget.git",
	)
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	require.Len(updates, 1)
	assert.Equal("ws-issue", updates[0].WorkspaceID)
	assert.Equal(42, updates[0].PRNumber)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	require.NotNil(ws.AssociatedPRNumber)
	assert.Equal(42, *ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceFallsBackToLocalBranchName(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMR(t, d, repoID, 42, "feature/local-only")

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "feature/local-only")
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	require.Len(updates, 1)
	assert.Equal("ws-issue", updates[0].WorkspaceID)
	assert.Equal(42, updates[0].PRNumber)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	require.NotNil(ws.AssociatedPRNumber)
	assert.Equal(42, *ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceSkipsSyntheticIssueBranch(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMR(t, d, repoID, 42, "middleman/issue-7")

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "middleman/issue-7")
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	assert.Nil(ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceUsesUpstreamRemoteIdentity(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMRWithFork(
		t, d, repoID, 41,
		"shared-branch", "https://github.com/Fork-One/Widget.git",
	)
	seedMRWithFork(
		t, d, repoID, 42,
		"shared-branch", "https://github.com/fork-two/widget.git",
	)

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "shared-branch")
	runWorkspaceTestGit(
		t, worktreePath,
		"remote", "set-url", "origin", "git@github.com:Fork-Two/Widget.git",
	)
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.shared-branch.remote", "origin",
	)
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.shared-branch.merge", "refs/heads/shared-branch",
	)
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	require.Len(updates, 1)
	assert.Equal("ws-issue", updates[0].WorkspaceID)
	assert.Equal(42, updates[0].PRNumber)
}

func TestPRMonitorRunOnceRequiresCandidateRemoteIdentityForUpstreamMatch(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
	}{
		{name: "empty clone url", cloneURL: ""},
		{name: "malformed clone url", cloneURL: "not a clone url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := Assert.New(t)
			require := require.New(t)
			d := openTestDB(t)
			ctx := context.Background()

			repoID := seedRepo(t, d, "github.com", "acme", "widget")
			seedIssue(t, d, repoID, 7, "Track workspace association")
			seedMRWithFork(
				t, d, repoID, 42,
				"shared-branch", tt.cloneURL,
			)

			worktreePath := setupMonitorRepo(t)
			runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "shared-branch")
			runWorkspaceTestGit(
				t, worktreePath,
				"remote", "set-url", "origin", "git@github.com:Fork-Two/Widget.git",
			)
			runWorkspaceTestGit(
				t, worktreePath,
				"config", "branch.shared-branch.remote", "origin",
			)
			runWorkspaceTestGit(
				t, worktreePath,
				"config", "branch.shared-branch.merge", "refs/heads/shared-branch",
			)
			insertMonitorWorkspace(t, d, worktreePath, nil)

			monitor := NewPRMonitor(d)
			updates, err := monitor.RunOnce(ctx)
			require.NoError(err)
			assert.Empty(updates)

			ws, err := d.GetWorkspace(ctx, "ws-issue")
			require.NoError(err)
			require.NotNil(ws)
			assert.Nil(ws.AssociatedPRNumber)
		})
	}
}

func TestPRMonitorRunOnceRequiresParseableUpstreamRemoteIdentity(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMRWithFork(
		t, d, repoID, 42,
		"shared-branch", "https://github.com/fork-two/widget.git",
	)

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "shared-branch")
	runWorkspaceTestGit(
		t, worktreePath,
		"remote", "set-url", "origin", "not a clone url",
	)
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.shared-branch.remote", "origin",
	)
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.shared-branch.merge", "refs/heads/shared-branch",
	)
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	assert.Nil(ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceSkipsWhenConfiguredUpstreamRemoteURLFails(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMR(t, d, repoID, 42, "transient-remote")

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "transient-remote")
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.transient-remote.remote", "missing-remote",
	)
	runWorkspaceTestGit(
		t, worktreePath,
		"config", "branch.transient-remote.merge", "refs/heads/transient-remote",
	)
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	assert.Nil(ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceSkipsAmbiguousLocalBranchFallback(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMRWithFork(
		t, d, repoID, 41,
		"shared-local", "https://github.com/fork-one/widget.git",
	)
	seedMRWithFork(
		t, d, repoID, 42,
		"shared-local", "https://github.com/fork-two/widget.git",
	)

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "shared-local")
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	assert.Nil(ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceScopesCandidatesByPlatformHost(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	githubRepoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, githubRepoID, 7, "Track workspace association")
	gheRepoID := seedRepo(t, d, "ghe.example.com", "acme", "widget")
	seedMR(t, d, gheRepoID, 42, "same-branch")

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "same-branch")
	insertMonitorWorkspace(t, d, worktreePath, nil)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	assert.Nil(ws.AssociatedPRNumber)
}

func TestPRMonitorRunOnceDoesNotOverwriteExistingAssociation(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedIssue(t, d, repoID, 7, "Track workspace association")
	seedMR(t, d, repoID, 42, "feature/original")
	seedMR(t, d, repoID, 99, "feature/new")

	worktreePath := setupMonitorRepo(t)
	runWorkspaceTestGit(t, worktreePath, "checkout", "-b", "feature/new")
	existing := 42
	insertMonitorWorkspace(t, d, worktreePath, &existing)

	monitor := NewPRMonitor(d)
	updates, err := monitor.RunOnce(ctx)
	require.NoError(err)
	assert.Empty(updates)

	ws, err := d.GetWorkspace(ctx, "ws-issue")
	require.NoError(err)
	require.NotNil(ws)
	require.NotNil(ws.AssociatedPRNumber)
	assert.Equal(42, *ws.AssociatedPRNumber)
}

func TestNormalizeCloneRepoIdentity(t *testing.T) {
	assert := Assert.New(t)

	assert.Equal(
		"fork/widget",
		normalizeCloneRepoIdentity(" git@GitHub.com:Fork/Widget.git "),
	)
	assert.Equal(
		"fork/widget",
		normalizeCloneRepoIdentity("https://token@github.com/Fork/Widget/"),
	)
	assert.Equal(
		"fork/widget",
		normalizeCloneRepoIdentity("ssh://git@github.com:22/Fork/Widget.git"),
	)
	assert.Empty(normalizeCloneRepoIdentity("/tmp/workspace/remote.git"))
	assert.Empty(normalizeCloneRepoIdentity("not a clone url"))
}
