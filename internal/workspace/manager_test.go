package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/db"
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
		context.Background(), host, owner, name,
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
	_, err := d.UpsertMergeRequest(context.Background(), mr)
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
	_, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoError(t, err)
}

func TestCreate(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
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
	ctx := context.Background()
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
		ctx, "github.com", "acme", "widget", 99,
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
	ctx := context.Background()
	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		ctx, "github.com", "unknown", "repo", 1,
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "repository not tracked")
}

func TestCreateDuplicate(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()
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
	ctx := context.Background()

	seedRepo(t, d, "github.com", "acme", "widget")

	mgr := NewManager(d, t.TempDir())

	_, err := mgr.Create(
		ctx, "github.com", "acme", "widget", 999,
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
		context.Background(), "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = mgr.Setup(ctx, ws)
	require.Error(err)
	require.Contains(err.Error(), "clone manager not set")

	got, err := d.GetWorkspace(context.Background(), ws.ID)
	require.NoError(err)
	require.NotNil(got)
	assert.Equal("error", got.Status)
	require.NotNil(got.ErrorMessage)
	assert.Contains(*got.ErrorMessage, "clone manager not set")

	events, err := d.ListWorkspaceSetupEvents(
		context.Background(), ws.ID,
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
		context.Background(), "github.com", "acme", "widget", 42,
	)
	require.NoError(err)
	require.NotNil(ws)

	origTimeout := workspacePersistTimeout
	workspacePersistTimeout = 200 * time.Millisecond
	t.Cleanup(func() { workspacePersistTimeout = origTimeout })

	tx, err := d.WriteDB().BeginTx(context.Background(), nil)
	require.NoError(err)
	t.Cleanup(func() { _ = tx.Rollback() })

	start := time.Now()
	err = mgr.failSetup(
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

	ctx := context.Background()

	// Script exits 0 for every invocation, so EnsureTmux observes
	// "session exists" after the has-session call and returns
	// without running new-session.
	require.NoError(t, mgr.EnsureTmux(ctx, "sess-A", t.TempDir()))

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

	ctx := context.Background()
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

func TestManagerEnsureTmuxCreatesSessionOnMiss(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

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

	ctx := context.Background()
	require.NoError(mgr.EnsureTmux(ctx, "sess-B", "/tmp/cwd"))

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

	err := mgr.EnsureTmux(context.Background(), "sess-X", "/tmp")
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

	err := mgr.EnsureTmux(context.Background(), "sess-Y", "/tmp")
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

	err := mgr.EnsureTmux(context.Background(), "sess-Q", "/tmp")
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

	err := mgr.EnsureTmux(context.Background(), "sess-R", "/tmp")
	require.Error(err)
	require.Contains(err.Error(), "tmux has-session")
	require.Contains(err.Error(), "real failure")
}
