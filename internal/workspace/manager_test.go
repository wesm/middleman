package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitenv"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func seedRepo(
	t *testing.T, d *db.DB,
	host, owner, name string,
) int64 {
	t.Helper()
	id, err := d.UpsertRepo(
		t.Context(), host, owner, name,
	)
	require.NoError(t, err)
	return id
}

func seedMR(
	t *testing.T, d *db.DB,
	repoID int64, number int, headBranch string,
) {
	t.Helper()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mr := &db.MergeRequest{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(number),
		Number:         number,
		Title:          "Test PR",
		Author:         "author",
		State:          "open",
		HeadBranch:     headBranch,
		BaseBranch:     "main",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	_, err := d.UpsertMergeRequest(t.Context(), mr)
	require.NoError(t, err)
}

func seedMRWithFork(
	t *testing.T, d *db.DB,
	repoID int64, number int,
	headBranch, cloneURL string,
) {
	t.Helper()
	now := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	mr := &db.MergeRequest{
		RepoID:           repoID,
		PlatformID:       repoID*10000 + int64(number),
		Number:           number,
		Title:            "Fork PR",
		Author:           "contributor",
		State:            "open",
		HeadBranch:       headBranch,
		BaseBranch:       "main",
		HeadRepoCloneURL: cloneURL,
		CreatedAt:        now,
		UpdatedAt:        now,
		LastActivityAt:   now,
	}
	_, err := d.UpsertMergeRequest(t.Context(), mr)
	require.NoError(t, err)
}

func TestCreate(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := t.Context()
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)

	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	assert.NotEmpty(ws.ID)
	assert.Len(ws.ID, 16) // 8 bytes hex-encoded
	assert.Equal("creating", ws.Status)
	assert.Equal("github.com", ws.PlatformHost)
	assert.Equal("acme", ws.RepoOwner)
	assert.Equal("widget", ws.RepoName)
	assert.Equal(42, ws.MRNumber)
	assert.Equal("feature/thing", ws.MRHeadRef)
	assert.Nil(ws.MRHeadRepo)
	assert.Contains(ws.WorktreePath, "pr-42")
	assert.Equal("middleman-"+ws.ID, ws.TmuxSession)

	// Verify persisted in DB.
	got, err := d.GetWorkspace(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal(ws.ID, got.ID)
	assert.Equal("creating", got.Status)
}

func TestCreateForkPR(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMRWithFork(
		t, d, repoID, 99, "fix/typo",
		"https://github.com/contributor/widget.git",
	)

	mgr := NewManager(d, wtDir)

	ws, err := mgr.Create(
		t.Context(), "github.com", "acme", "widget", 99,
	)
	require.NoError(t, err)
	require.NotNil(t, ws)

	assert.NotNil(ws.MRHeadRepo)
	assert.Equal(
		"https://github.com/contributor/widget.git",
		*ws.MRHeadRepo,
	)
}

func TestCreateRepoNotTracked(t *testing.T) {
	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		t.Context(), "github.com", "unknown", "repo", 1,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repository not tracked")
}

func TestCreateDuplicate(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := t.Context()
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)

	// First create succeeds.
	ws, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	// Second create for same MR fails with unique constraint.
	_, err = mgr.Create(
		ctx, "github.com", "acme", "widget", 42,
	)
	require.Error(err)
	require.Contains(err.Error(), "UNIQUE constraint")
}

func TestCreateMRNotSynced(t *testing.T) {
	d := openTestDB(t)

	seedRepo(t, d, "github.com", "acme", "widget")

	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		t.Context(), "github.com", "acme", "widget", 999,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not synced yet")
}

func TestSetupFailurePersistsStatusWhenContextCanceled(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)
	ws, err := mgr.Create(
		t.Context(), "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err = mgr.Setup(ctx, ws)
	require.Error(err)
	require.Contains(err.Error(), "clone manager not set")

	got, err := d.GetWorkspace(t.Context(), ws.ID)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal("error", got.Status)
	require.NotNil(got.ErrorMessage)
	assert.Contains(*got.ErrorMessage, "clone manager not set")

	events, err := d.ListWorkspaceSetupEvents(
		t.Context(), ws.ID,
	)
	require.NoError(err)
	require.Len(events, 2)
	assert.Equal("setup", events[0].Stage)
	assert.Equal("started", events[0].Outcome)
	assert.Equal("clone", events[1].Stage)
	assert.Equal("failure", events[1].Outcome)
	assert.Contains(events[1].Message, "clone manager not set")
}

func TestFailSetupUsesSinglePersistenceBudget(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)
	ws, err := mgr.Create(
		t.Context(), "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	origTimeout := workspacePersistTimeout
	workspacePersistTimeout = 200 * time.Millisecond
	t.Cleanup(func() { workspacePersistTimeout = origTimeout })

	tx, err := d.WriteDB().BeginTx(t.Context(), nil)
	require.NoError(err)
	t.Cleanup(func() { _ = tx.Rollback() })

	start := time.Now()
	err = mgr.failSetup(
		t.Context(),
		ws.ID, workspaceSetupStageClone,
		errors.New("forced persistence timeout"),
	)
	elapsed := time.Since(start)

	require.Error(err)
	assert.Contains(err.Error(), "forced persistence timeout")
	assert.Less(
		elapsed,
		workspacePersistTimeout+(workspacePersistTimeout/2),
	)
}

func TestFailSetupRespectsParentDeadline(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	wtDir := t.TempDir()

	repoID := seedRepo(
		t, d, "github.com", "acme", "widget",
	)
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, wtDir)
	ws, err := mgr.Create(
		t.Context(), "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	origTimeout := workspacePersistTimeout
	workspacePersistTimeout = time.Second
	t.Cleanup(func() { workspacePersistTimeout = origTimeout })

	tx, err := d.WriteDB().BeginTx(t.Context(), nil)
	require.NoError(err)
	t.Cleanup(func() { _ = tx.Rollback() })

	parent, cancel := context.WithTimeout(
		t.Context(), 100*time.Millisecond,
	)
	defer cancel()

	start := time.Now()
	err = mgr.failSetup(
		parent,
		ws.ID, workspaceSetupStageClone,
		errors.New("forced persistence timeout"),
	)
	elapsed := time.Since(start)

	require.Error(err)
	assert.Contains(err.Error(), "forced persistence timeout")
	assert.Less(elapsed, 300*time.Millisecond)
}

func TestAddPreferredWorktreeRejectsUnsafeBranchName(t *testing.T) {
	require := require.New(t)

	cloneDir := setupBareCloneForWorkspaceGitTest(t)
	mgr := NewManager(openTestDB(t), t.TempDir())
	ws := &Workspace{
		MRNumber:     42,
		MRHeadRef:    "-unsafe",
		WorktreePath: filepath.Join(t.TempDir(), "worktree"),
	}

	_, err := mgr.addPreferredWorktree(
		t.Context(), cloneDir, ws,
	)
	require.Error(err)
	require.Contains(err.Error(), "invalid branch name")
}

func TestRollbackWorktreeDeletesBranchWhenContextCanceled(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	cloneDir := setupBareCloneForWorkspaceGitTest(t)
	branch := syntheticWorktreeBranch(42)
	require.NoError(runGit(
		t.Context(), cloneDir,
		"branch", branch, "main",
	))

	ws := &Workspace{
		MRNumber:     42,
		WorktreePath: filepath.Join(t.TempDir(), "missing-worktree"),
	}
	mgr := NewManager(openTestDB(t), t.TempDir())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	mgr.rollbackWorktree(ctx, cloneDir, ws, workspaceBranchUnknown)

	_, exists, err := gitRefSHA(
		t.Context(), cloneDir, "refs/heads/"+branch,
	)
	require.NoError(err)
	assert.False(exists)
}

func TestCleanupContextRespectsParentDeadline(t *testing.T) {
	require := require.New(t)

	parent, cancel := context.WithTimeout(
		t.Context(), 100*time.Millisecond,
	)
	defer cancel()

	cleanupCtx, cleanupCancel := cleanupContext(parent)
	defer cleanupCancel()

	deadline, ok := cleanupCtx.Deadline()
	require.True(ok)

	remaining := time.Until(deadline)
	require.LessOrEqual(remaining, 100*time.Millisecond)
	require.Greater(remaining, 0*time.Millisecond)
}

func setupBareCloneForWorkspaceGitTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	work := filepath.Join(dir, "work")
	cloneDir := filepath.Join(dir, "clone.git")

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
	runWorkspaceTestGit(t, dir, "clone", "--bare", remote, cloneDir)

	return cloneDir
}

func runWorkspaceTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

func TestShellFromPasswdLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			"normal zsh",
			"wesm:x:501:20:Wes McKinney:/Users/wesm:/bin/zsh",
			"/bin/zsh",
		},
		{
			"normal bash",
			"dev:x:1000:1000::/home/dev:/bin/bash",
			"/bin/bash",
		},
		{
			"nologin filtered",
			"nobody:x:65534:65534:Nobody:/nonexistent:/sbin/nologin",
			"",
		},
		{
			"false filtered",
			"git:x:998:998::/home/git:/usr/bin/false",
			"",
		},
		{
			"bin/false filtered",
			"svc:x:999:999::/srv:/bin/false",
			"",
		},
		{
			"empty shell",
			"user:x:1000:1000::/home/user:",
			"",
		},
		{
			"too few fields",
			"broken:line",
			"",
		},
		{
			"empty line",
			"",
			"",
		},
		{
			"trailing whitespace",
			"user:x:1000:1000::/home/user:/bin/zsh\n",
			"/bin/zsh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellFromPasswdLine(tt.line)
			require.Equal(t, tt.want, got)
		})
	}
}

// writeRecorderScript creates an executable shell script at a
// fresh path under t.TempDir() that appends the count and each
// argument, NUL-delimited, to TMUX_RECORD. Returns the script path
// and the record file path.
func writeRecorderScript(t *testing.T) (scriptPath, recordPath string) {
	t.Helper()
	dir := t.TempDir()
	recordPath = filepath.Join(dir, "record")
	scriptPath = filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", recordPath)
	return scriptPath, recordPath
}

// readRecorderArgv reads the NUL-delimited record file and returns
// each recorded invocation as a []string. Each invocation is stored
// as "<argc>\0<arg0>\0<arg1>...\0", so this reads argc then slurps
// that many args per invocation.
func readRecorderArgv(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Split on NUL. Each record is "<argc>\0<arg0>\0<arg1>\0...\0",
	// so the flushed stream always ends with a trailing \0 and Split
	// produces a final empty element after it. Strip exactly one
	// trailing empty so we don't mistake it for part of the next
	// record. Interior empty elements are real args (the NUL framing
	// exists to preserve them) and must NOT be skipped.
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	var out [][]string
	for i := 0; i < len(parts); {
		n, err := strconv.Atoi(parts[i])
		require.NoError(t, err)
		i++
		argv := parts[i : i+n]
		out = append(out, argv)
		i += n
	}
	return out
}

func TestManagerEnsureTmuxHasSessionPrefix(t *testing.T) {
	assert := Assert.New(t)

	script, record := writeRecorderScript(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	// Script exits 0 for every invocation, so EnsureTmux observes
	// "session exists" after the has-session call and returns
	// without running new-session.
	require.NoError(t, mgr.EnsureTmux(t.Context(), "sess-A", t.TempDir()))

	argvs := readRecorderArgv(t, record)
	require.Len(t, argvs, 1)
	assert.Equal(
		[]string{"wrap", "has-session", "-t", "sess-A"},
		argvs[0],
	)
}

func TestManagerDeleteUsesTmuxPrefix(t *testing.T) {
	assert := Assert.New(t)

	script, record := writeRecorderScript(t)

	d := openTestDB(t)
	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	ctx := t.Context()
	ws, err := mgr.Create(ctx, "github.com", "acme", "widget", 42)
	require.NoError(t, err)

	// force=true skips the dirty-files check. m.clones is nil, so
	// Delete takes the clones==nil short-circuit after killing the
	// tmux session — no git operations are required.
	_, err = mgr.Delete(ctx, ws.ID, true)
	require.NoError(t, err)

	// Delete invokes exactly one tmux command on this path
	// (kill-session). It ignores the exit code because the session
	// may not exist, but our script exits 0 so the invocation is
	// still recorded.
	argvs := readRecorderArgv(t, record)
	require.Len(t, argvs, 1)
	assert.Equal(
		[]string{"wrap", "kill-session", "-t", ws.TmuxSession},
		argvs[0],
	)
}

func TestManagerDeleteAllowsMissingTmuxSession(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "kill-session" ]; then` + "\n" +
		`    echo "can't find session: missing" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	ctx := context.Background()
	ws, err := mgr.Create(ctx, "github.com", "acme", "widget", 42)
	require.NoError(err)

	dirty, err := mgr.Delete(ctx, ws.ID, true)
	require.NoError(err)
	assert.Nil(dirty)

	got, err := mgr.Get(ctx, ws.ID)
	require.NoError(err)
	assert.Nil(got)

	argvs := readRecorderArgv(t, record)
	require.Len(argvs, 1)
	assert.Equal(
		[]string{"wrap", "kill-session", "-t", ws.TmuxSession},
		argvs[0],
	)
}

func TestManagerDeleteFailsWhenTmuxKillFails(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "kill-session" ]; then` + "\n" +
		`    echo "permission denied" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	ctx := context.Background()
	ws, err := mgr.Create(ctx, "github.com", "acme", "widget", 42)
	require.NoError(err)
	require.NoError(d.UpdateWorkspaceStatus(ctx, ws.ID, "ready", nil))

	dirty, err := mgr.Delete(ctx, ws.ID, true)
	assert.Nil(dirty)
	require.Error(err)
	assert.Contains(err.Error(), "kill tmux session")
	assert.Contains(err.Error(), "permission denied")

	got, getErr := mgr.Get(ctx, ws.ID)
	require.NoError(getErr)
	require.NotNil(got)
	assert.Equal(ws.ID, got.ID)

	argvs := readRecorderArgv(t, record)
	require.Len(argvs, 1)
	assert.Equal(
		[]string{"wrap", "kill-session", "-t", ws.TmuxSession},
		argvs[0],
	)
}

func TestManagerDeleteAllowsErroredWorkspaceWhenTmuxUnavailable(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{
		filepath.Join(t.TempDir(), "missing-tmux"),
	})

	ctx := context.Background()
	ws := &Workspace{
		ID:              "ws-tmux-unavailable",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		MRNumber:        42,
		MRHeadRef:       "feature/thing",
		WorkspaceBranch: workspaceBranchUnknown,
		WorktreePath:    filepath.Join(t.TempDir(), "worktree"),
		TmuxSession:     "middleman-0000000000000042",
		Status:          "error",
	}
	require.NoError(d.InsertWorkspace(ctx, ws))

	dirty, err := mgr.Delete(ctx, ws.ID, true)
	require.NoError(err)
	assert.Nil(dirty)

	got, err := mgr.Get(ctx, ws.ID)
	require.NoError(err)
	assert.Nil(got)
}

func TestManagerReapOrphanTmuxSessionsKillsUnknownManagedSessions(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "list-sessions" ]; then` + "\n" +
		`    printf 'middleman-0000000000000001\nmiddleman-ffffffffffffffff\nmiddleman-notes\nother-session\n'` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	live := &Workspace{
		ID:           "ws-live",
		PlatformHost: "github.com",
		RepoOwner:    "acme",
		RepoName:     "widget",
		MRNumber:     1,
		MRHeadRef:    "feature/live",
		WorktreePath: filepath.Join(t.TempDir(), "live"),
		TmuxSession:  "middleman-0000000000000001",
		Status:       "ready",
	}
	require.NoError(d.InsertWorkspace(context.Background(), live))

	require.NoError(mgr.ReapOrphanTmuxSessions(context.Background()))

	argvs := readRecorderArgv(t, record)
	require.Len(argvs, 2)
	assert.Equal(
		[]string{"wrap", "list-sessions", "-F", "#{session_name}"},
		argvs[0],
	)
	assert.Equal(
		[]string{"wrap", "kill-session", "-t", "middleman-ffffffffffffffff"},
		argvs[1],
	)
}

func TestManagerRequestRetryQueuesWhileCreatingAndStartsIfErrored(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	ctx := context.Background()
	ws := &Workspace{
		ID:              "ws-queued-retry",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		MRNumber:        42,
		MRHeadRef:       "feature/retry",
		WorkspaceBranch: workspaceBranchUnknown,
		WorktreePath:    "/tmp/ws-queued-retry",
		TmuxSession:     "middleman-ws-queued-retry",
		Status:          "creating",
	}
	require.NoError(d.InsertWorkspace(ctx, ws))

	current, startNow, err := mgr.RequestRetry(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(current)
	assert.False(startNow)
	assert.Equal("creating", current.Status)

	errMsg := "ensure clone failed"
	require.NoError(d.UpdateWorkspaceStatus(ctx, ws.ID, "error", &errMsg))

	next, queued, err := mgr.StartQueuedRetryIfErrored(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(next)
	assert.True(queued)
	assert.Equal("creating", next.Status)
	assert.Nil(next.ErrorMessage)

	stored, err := d.GetWorkspace(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("creating", stored.Status)
	assert.Nil(stored.ErrorMessage)
	assert.Equal(workspaceBranchUnknown, stored.WorkspaceBranch)

	next, queued, err = mgr.StartQueuedRetryIfErrored(ctx, ws.ID)
	require.NoError(err)
	assert.Nil(next)
	assert.False(queued)
}

func TestManagerRequestRetryStartsWhenSetupFailedBeforeQueue(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	ctx := context.Background()
	errMsg := "ensure clone failed"
	ws := &Workspace{
		ID:              "ws-raced-retry",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		MRNumber:        42,
		MRHeadRef:       "feature/retry",
		WorkspaceBranch: "middleman/pr-42",
		WorktreePath:    "/tmp/ws-raced-retry",
		TmuxSession:     "middleman-ws-raced-retry",
		Status:          "error",
		ErrorMessage:    &errMsg,
	}
	require.NoError(d.InsertWorkspace(ctx, ws))

	next, startNow, err := mgr.queueRetryOrStartErrored(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(next)
	assert.True(startNow)
	assert.Equal("creating", next.Status)
	assert.Nil(next.ErrorMessage)
	assert.Equal(workspaceBranchUnknown, next.WorkspaceBranch)

	stored, err := d.GetWorkspace(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("creating", stored.Status)
	assert.Nil(stored.ErrorMessage)
	assert.Equal(workspaceBranchUnknown, stored.WorkspaceBranch)

	next, queued, err := mgr.StartQueuedRetryIfErrored(ctx, ws.ID)
	require.NoError(err)
	assert.Nil(next)
	assert.False(queued)
}

func TestManagerRequestRetryDiscardsQueuedRetryWhenSetupSucceeds(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	ctx := context.Background()
	ws := &Workspace{
		ID:              "ws-discard-retry",
		PlatformHost:    "github.com",
		RepoOwner:       "acme",
		RepoName:        "widget",
		MRNumber:        42,
		MRHeadRef:       "feature/retry",
		WorkspaceBranch: workspaceBranchUnknown,
		WorktreePath:    "/tmp/ws-discard-retry",
		TmuxSession:     "middleman-ws-discard-retry",
		Status:          "creating",
	}
	require.NoError(d.InsertWorkspace(ctx, ws))

	current, startNow, err := mgr.RequestRetry(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(current)
	assert.False(startNow)

	require.NoError(d.UpdateWorkspaceStatus(ctx, ws.ID, "ready", nil))

	next, queued, err := mgr.StartQueuedRetryIfErrored(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(next)
	assert.False(queued)
	assert.Equal("ready", next.Status)

	stored, err := d.GetWorkspace(ctx, ws.ID)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("ready", stored.Status)
}

func TestManagerEnsureTmuxCreatesSessionOnMiss(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	// Script: "has-session" emits tmux's canonical "can't find
	// session" stderr and exits 1 (so isTmuxSessionAbsent classifies
	// it as session-missing rather than wrapper failure); everything
	// else succeeds, so EnsureTmux calls newTmuxSession.
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	require.NoError(mgr.EnsureTmux(t.Context(), "sess-B", "/tmp/cwd"))

	argvs := readRecorderArgv(t, record)
	require.Len(argvs, 2)
	assert.Equal(
		[]string{"has-session", "-t", "sess-B"},
		argvs[0],
	)
	// new-session argv: "new-session -d -s sess-B -c /tmp/cwd <shell> -l"
	// We check the prefix up to the shell; the shell resolves per
	// runtime so just assert it is non-empty and ends with "-l".
	require.GreaterOrEqual(len(argvs[1]), 8)
	assert.Equal("new-session", argvs[1][0])
	assert.Equal("-d", argvs[1][1])
	assert.Equal("-s", argvs[1][2])
	assert.Equal("sess-B", argvs[1][3])
	assert.Equal("-c", argvs[1][4])
	assert.Equal("/tmp/cwd", argvs[1][5])
	assert.NotEmpty(argvs[1][6])
	assert.Equal("-l", argvs[1][7])
}

func TestManagerEnsureTmuxCreatesSessionOnMacOSMissingServer(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "error connecting to /private/tmp/tmux-501/default (No such file or directory)" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	require.NoError(mgr.EnsureTmux(context.Background(), "sess-macos", "/tmp/cwd"))

	argvs := readRecorderArgv(t, record)
	require.Len(argvs, 2)
	assert.Equal(
		[]string{"has-session", "-t", "sess-macos"},
		argvs[0],
	)
	assert.Equal("new-session", argvs[1][0])
	assert.Equal("sess-macos", argvs[1][3])
}

// TestReadRecorderArgvPreservesEmptyArgs pins down the parser's
// empty-arg handling. The NUL-delimited record format was chosen to
// round-trip argv with empty-string elements unambiguously; the
// parser must keep interior and trailing empties rather than
// collapsing them.
func TestReadRecorderArgvPreservesEmptyArgs(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	path := filepath.Join(t.TempDir(), "record")

	// First record: 3 args with an interior empty ("a", "", "b").
	// Second record: 2 args with a trailing empty ("x", "").
	body := "3\x00a\x00\x00b\x00" + "2\x00x\x00\x00"
	require.NoError(os.WriteFile(path, []byte(body), 0o644))

	argvs := readRecorderArgv(t, path)
	require.Len(argvs, 2)
	assert.Equal([]string{"a", "", "b"}, argvs[0])
	assert.Equal([]string{"x", ""}, argvs[1])
}

// TestManagerEnsureTmuxPropagatesBinaryError verifies that a wrapper
// misconfiguration (binary not on disk) surfaces as an error rather
// than being silently conflated with "session does not exist, please
// create one." The previous boolean-only tmuxSessionExists swallowed
// this case — EnsureTmux would proceed to run new-session with the
// same broken wrapper and the error would only surface on the second
// exec, masking the real cause.
func TestManagerEnsureTmuxPropagatesBinaryError(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	// Path that cannot possibly exist — exec returns a non-exit
	// error (ENOENT), not an *exec.ExitError.
	mgr.SetTmuxCommand(
		[]string{filepath.Join(t.TempDir(), "does-not-exist")},
	)

	err := mgr.EnsureTmux(t.Context(), "sess-X", "/tmp")
	require.Error(err)
	require.Contains(err.Error(), "tmux has-session")
}

// TestManagerEnsureTmuxPropagatesNon1ExitCode pins down the
// exit-code-1 carve-out in tmuxSessionExists. tmux's has-session
// exits 1 specifically when the session is not found; wrappers that
// fail for their own reasons typically exit with other codes (127
// "command not found", 203 "exec failed", etc.). A wrapper exiting
// with a non-1 code used to be silently treated as "session absent"
// because the old check matched any *exec.ExitError. Now it must
// propagate to the caller so misconfiguration surfaces cleanly.
func TestManagerEnsureTmuxPropagatesNon1ExitCode(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	// exit 127 mimics "command not found" — a common wrapper failure
	// signal that is NOT tmux's own "session missing" response.
	body := "#!/bin/sh\nexit 127\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	err := mgr.EnsureTmux(t.Context(), "sess-Y", "/tmp")
	require.Error(err)
	require.Contains(err.Error(), "tmux has-session")
}

// TestManagerEnsureTmuxPropagatesExit1NonTmuxError covers the
// second half of the session-absent heuristic: exit code 1 alone is
// not enough, the output must match tmux's canonical "session
// missing" phrases too. Many real wrappers and shell scripts use
// exit 1 as a generic failure signal — treating that as "session
// absent" would mask the wrapper bug by immediately trying
// new-session through the same broken wrapper.
func TestManagerEnsureTmuxPropagatesExit1NonTmuxError(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\necho 'wrapper blew up' >&2\nexit 1\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	err := mgr.EnsureTmux(t.Context(), "sess-Q", "/tmp")
	require.Error(err)
	require.Contains(err.Error(), "tmux has-session")
	require.Contains(err.Error(), "wrapper blew up")
}

// TestManagerEnsureTmuxIgnoresAbsencePhraseOnStdout pins down the
// stdout vs. stderr distinction. A wrapper that exits 1 with the
// tmux phrase on stdout (e.g. one that mirrors stderr to stdout for
// logging, or a script that coincidentally prints the phrase for
// unrelated reasons) must NOT be treated as session-absent — only
// stderr carries the authoritative tmux signal.
func TestManagerEnsureTmuxIgnoresAbsencePhraseOnStdout(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`echo "can't find session: sim"` + "\n" + // stdout only
		`echo "real failure" >&2` + "\n" +
		"exit 1\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	err := mgr.EnsureTmux(t.Context(), "sess-R", "/tmp")
	require.Error(err)
	require.Contains(err.Error(), "tmux has-session")
	require.Contains(err.Error(), "real failure")
}
