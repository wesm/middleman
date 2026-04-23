package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	ghclient "github.com/wesm/middleman/internal/github"
)

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// writeTmuxRecorder creates an executable fake-tmux script at a
// fresh temp path. The script appends NUL-delimited argv to
// record. For has-session it emits tmux's "can't find session"
// stderr and exits 1 (so EnsureTmux's isTmuxSessionAbsent check
// sees the canonical signal and proceeds to new-session); all
// other invocations exit 0. Returns the script path and the record
// path.
func writeTmuxRecorder(t *testing.T) (script, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	script = filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "display-message" ]; then` + "\n" +
		`    printf '%s\n' "$TMUX_PANE_TITLE"` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "capture-pane" ]; then` + "\n" +
		`    printf '%s\n' "$TMUX_PANE_OUTPUT"` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)
	return script, record
}

func readTmuxRecord(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	// Split on NUL. Each record is "<argc>\0<arg0>\0<arg1>\0...\0",
	// so a flushed stream always ends with a trailing \0 and Split
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
		if err != nil {
			// Trailing record is mid-write: argc isn't a valid
			// integer yet. Stop; the next poll will see the full
			// record once the recorder script flushes.
			break
		}
		if i+1+n > len(parts) {
			// argc is parsed but not all args are on disk yet.
			// Same treatment: defer to the next poll.
			break
		}
		i++
		argv := parts[i : i+n]
		out = append(out, argv)
		i += n
	}
	return out
}

// setupWrapperServer constructs a full server wired with a
// recording-script tmux command, a bare repo, and a seeded PR.
// Returns a generated API client pointed at the httptest server,
// the httptest baseURL (needed for WebSocket dialing), and the
// record-file path.
func setupWrapperServer(t *testing.T) (client *apiclient.Client, baseURL, record string) {
	t.Helper()
	script, record := writeTmuxRecorder(t)
	client, baseURL = setupWrapperServerWithScript(t, script)
	return client, baseURL, record
}

// setupWrapperServerWithScript is setupWrapperServer parameterized
// by the tmux-command path. Tests that want a non-default wrapper
// (e.g. one that fails has-session with a non-1 exit code) write
// their own script first and call this helper instead.
func setupWrapperServerWithScript(
	t *testing.T, script string,
) (client *apiclient.Client, baseURL string) {
	t.Helper()
	client, baseURL, _ = setupWrapperServerWithScriptAndDB(
		t, script,
	)
	return client, baseURL
}

func setupWrapperServerWithScriptAndDB(
	t *testing.T, script string,
) (client *apiclient.Client, baseURL string, database *db.DB) {
	t.Helper()
	client, baseURL, database, _ = setupWrapperServerWithScriptAndDBAndServer(
		t, script,
	)
	return client, baseURL, database
}

func setupWrapperServerWithScriptAndDBAndServer(
	t *testing.T, script string,
) (
	client *apiclient.Client,
	baseURL string,
	database *db.DB,
	srv *Server,
) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e tests skipped in short mode")
	}

	dir := t.TempDir()
	var err error
	database, err = db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bareDir := filepath.Join(dir, "clones")
	require.NoError(t, os.MkdirAll(bareDir, 0o755))
	bare := filepath.Join(
		bareDir, "github.com", "acme", "widget.git",
	)
	tmpWork := filepath.Join(dir, "work")
	runGit(t, dir, "init", "--bare", "--initial-branch=main", bare)
	runGit(t, dir, "clone", bare, tmpWork)
	runGit(t, tmpWork, "config", "user.email", "test@test.com")
	runGit(t, tmpWork, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "base.txt"),
		[]byte("base\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "base commit")
	runGit(t, tmpWork, "push", "origin", "main")
	runGit(t, tmpWork, "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "new.txt"),
		[]byte("new\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "feature commit")
	runGit(t, tmpWork, "push", "origin", "feature")

	// Point bare origin at itself so EnsureClone fetch works.
	runGit(t, bare, "remote", "add", "origin", bare)

	clones := gitclone.New(bareDir, nil)
	worktreeDir := filepath.Join(dir, "worktrees")

	repos := []ghclient.RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}
	mock := &mockGH{}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database, nil, repos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	cfg := &config.Config{
		Tmux: config.Tmux{
			Command: []string{script, "wrap"},
		},
	}
	srv = New(database, syncer, nil, "/", cfg, ServerOptions{
		Clones:      clones,
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() { gracefulShutdown(t, srv) })

	seedPR(t, database, "acme", "widget", 1)

	// Real listener — WebSocket Dial needs a real TCP endpoint.
	// The generated API client also points at this URL rather than
	// the in-process roundtripper used elsewhere, because we cannot
	// split HTTP and WebSocket transports per-request.
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	// Wrap the underlying TCP transport with the same Content-Type
	// shim setupTestClient uses — the server's CSRF check rejects
	// non-GET requests without Content-Type (e.g. DELETE with no
	// body) as 415 Unsupported Media Type. The shim runs in addition
	// to the normal transport, which still reaches the httptest
	// server over TCP so WebSocket upgrades continue to work.
	httpClient := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet && req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", "application/json")
			}
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	client, err = apiclient.NewWithHTTPClient(ts.URL, httpClient)
	require.NoError(t, err)

	return client, ts.URL, database, srv
}

func TestTmuxWrapperNewSession(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		t.Context(),
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)

	// Workspace setup runs asynchronously. Poll the record file
	// until the new-session invocation shows up, up to ~5s.
	var argvs [][]string
	require.Eventually(
		func() bool {
			argvs = readTmuxRecord(t, record)
			for _, argv := range argvs {
				if len(argv) >= 2 && argv[1] == "new-session" {
					return true
				}
			}
			return false
		},
		5*time.Second, 50*time.Millisecond,
		"new-session argv not recorded",
	)

	var newSession []string
	for _, argv := range argvs {
		if len(argv) >= 2 && argv[1] == "new-session" {
			newSession = argv
			break
		}
	}

	// "wrap" prefix, then "new-session -d -s <id> -c <path> <shell> -l"
	require.GreaterOrEqual(len(newSession), 9)
	assert.Equal("wrap", newSession[0])
	assert.Equal("new-session", newSession[1])
	assert.Equal("-d", newSession[2])
	assert.Equal("-s", newSession[3])
	assert.NotEmpty(newSession[4])
	assert.Equal("-c", newSession[5])
	assert.NotEmpty(newSession[6])
	assert.NotEmpty(newSession[7])
	assert.Equal("-l", newSession[8])
}

func TestWorkspaceResponseIncludesTmuxWorkingState(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	t.Setenv("TMUX_PANE_TITLE", "⠴ t3code-b5014b03")
	t.Setenv("TMUX_PANE_OUTPUT", "stable output")

	client, _, _ := setupWrapperServer(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	var ready *generated.WorkspaceResponse
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
			ctx, wsID,
		)
		require.NoError(getErr)
		if getResp.JSON200 != nil && getResp.JSON200.Status == "ready" {
			ready = getResp.JSON200
			break
		}
	}
	require.NotNil(ready, "workspace never became ready")

	getResp, err := client.HTTP.GetWorkspacesById(ctx, wsID)
	require.NoError(err)
	defer getResp.Body.Close()
	require.Equal(http.StatusOK, getResp.StatusCode)

	var got struct {
		TmuxPaneTitle *string `json:"tmux_pane_title"`
		TmuxWorking   bool    `json:"tmux_working"`
	}
	require.NoError(json.NewDecoder(getResp.Body).Decode(&got))
	require.NotNil(got.TmuxPaneTitle)
	assert.Equal("⠴ t3code-b5014b03", *got.TmuxPaneTitle)
	assert.True(got.TmuxWorking)

	listResp, err := client.HTTP.GetWorkspaces(ctx)
	require.NoError(err)
	defer listResp.Body.Close()
	require.Equal(http.StatusOK, listResp.StatusCode)

	var listed struct {
		Workspaces []struct {
			ID            string  `json:"id"`
			TmuxPaneTitle *string `json:"tmux_pane_title"`
			TmuxWorking   bool    `json:"tmux_working"`
		} `json:"workspaces"`
	}
	require.NoError(json.NewDecoder(listResp.Body).Decode(&listed))
	require.Len(listed.Workspaces, 1)
	assert.Equal(wsID, listed.Workspaces[0].ID)
	require.NotNil(listed.Workspaces[0].TmuxPaneTitle)
	assert.Equal("⠴ t3code-b5014b03", *listed.Workspaces[0].TmuxPaneTitle)
	assert.True(listed.Workspaces[0].TmuxWorking)
}

func TestWorkspaceResponseTracksTmuxOutputActivity(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "pane-output")
	require.NoError(os.WriteFile(outputPath, []byte("initial\n"), 0o644))

	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "display-message" ]; then` + "\n" +
		`    printf '%s\n' "$TMUX_PANE_TITLE"` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "capture-pane" ]; then` + "\n" +
		`    cat "$TMUX_PANE_OUTPUT_FILE"` + "\n" +
		`    exit 0` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_PANE_TITLE", "workspace")
	t.Setenv("TMUX_PANE_OUTPUT_FILE", outputPath)

	client, _, database, srv := setupWrapperServerWithScriptAndDBAndServer(
		t, script,
	)
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	srv.tmuxActivity = newTmuxActivityTracker(func() time.Time { return now })
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	for range 50 {
		time.Sleep(100 * time.Millisecond)
		workspace, dbErr := database.GetWorkspace(ctx, wsID)
		require.NoError(dbErr)
		if workspace != nil && workspace.Status == "ready" {
			break
		}
	}

	first := getRawWorkspaceActivity(t, client, ctx, wsID)
	require.NotNil(first.TmuxPaneTitle)
	assert.Equal("workspace", *first.TmuxPaneTitle)
	assert.False(first.TmuxWorking)
	assert.Equal(tmuxActivitySourceNone, first.TmuxActivitySource)
	assert.Nil(first.TmuxLastOutputAt)

	require.NoError(os.WriteFile(
		outputPath,
		[]byte("initial\nnew output\n"),
		0o644,
	))
	now = now.Add(tmuxSampleMinInterval + time.Second)
	second := getRawWorkspaceActivity(t, client, ctx, wsID)
	assert.True(second.TmuxWorking)
	assert.Equal(tmuxActivitySourceOutput, second.TmuxActivitySource)
	require.NotNil(second.TmuxLastOutputAt)
	assert.Equal(now.Format(time.RFC3339), *second.TmuxLastOutputAt)

	now = now.Add(tmuxActivityTTL + time.Second)
	expired := getRawWorkspaceActivity(t, client, ctx, wsID)
	assert.False(expired.TmuxWorking)
	assert.Equal(tmuxActivitySourceNone, expired.TmuxActivitySource)
	require.NotNil(expired.TmuxLastOutputAt)
	assert.Equal(*second.TmuxLastOutputAt, *expired.TmuxLastOutputAt)
}

func getRawWorkspaceActivity(
	t *testing.T,
	client *apiclient.Client,
	ctx context.Context,
	wsID string,
) struct {
	TmuxPaneTitle      *string `json:"tmux_pane_title"`
	TmuxWorking        bool    `json:"tmux_working"`
	TmuxActivitySource string  `json:"tmux_activity_source"`
	TmuxLastOutputAt   *string `json:"tmux_last_output_at"`
} {
	t.Helper()
	resp, err := client.HTTP.GetWorkspacesById(ctx, wsID)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var got struct {
		TmuxPaneTitle      *string `json:"tmux_pane_title"`
		TmuxWorking        bool    `json:"tmux_working"`
		TmuxActivitySource string  `json:"tmux_activity_source"`
		TmuxLastOutputAt   *string `json:"tmux_last_output_at"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	return got
}

func TestIsWorkingTmuxTitleDetectsCodexSpinner(t *testing.T) {
	assert := Assert.New(t)

	cases := []struct {
		name    string
		title   string
		working bool
	}{
		{
			name:    "codex spinner frame",
			title:   "⠴ t3code-b5014b03",
			working: true,
		},
		{
			name:    "another codex spinner frame",
			title:   "⠦ t3code-b5014b03",
			working: true,
		},
		{
			name:    "settled codex title",
			title:   "t3code-b5014b03",
			working: false,
		},
		{
			name:    "english busy title is not protocol",
			title:   "codex working",
			working: false,
		},
		{
			name:    "opencode style title is not protocol",
			title:   "OC | Run sleep 10",
			working: false,
		},
		{
			name:    "pi style title is not protocol",
			title:   "π - tmp.foo",
			working: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(tc.working, isWorkingTmuxTitle(tc.title))
		})
	}
}

func TestWorkspaceCreateFailureLogsAndPersistsAuditEvent(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    echo "wrapper failed" >&2` + "\n" +
		`    exit 42` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	var logBuf lockedBuffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	client, _, database := setupWrapperServerWithScriptAndDB(
		t, script,
	)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	var failed *generated.WorkspaceResponse
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
			ctx, wsID,
		)
		require.NoError(getErr)
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			continue
		}
		if getResp.JSON200.Status == "error" {
			failed = getResp.JSON200
			break
		}
	}
	require.NotNil(failed, "workspace never entered error status")
	require.NotNil(failed.ErrorMessage)
	assert.Contains(*failed.ErrorMessage, "tmux new-session")
	assert.Contains(*failed.ErrorMessage, "wrapper failed")

	rows, err := database.ReadDB().QueryContext(ctx, `
		SELECT stage, outcome, message
		FROM middleman_workspace_setup_events
		WHERE workspace_id = ?
		ORDER BY id`, wsID,
	)
	require.NoError(err)
	defer rows.Close()

	type auditEvent struct {
		stage   string
		outcome string
		message string
	}

	var events []auditEvent
	for rows.Next() {
		var ev auditEvent
		require.NoError(rows.Scan(&ev.stage, &ev.outcome, &ev.message))
		events = append(events, ev)
	}
	require.NoError(rows.Err())
	require.NotEmpty(events)
	last := events[len(events)-1]
	assert.Equal("tmux_session", last.stage)
	assert.Equal("failure", last.outcome)
	assert.Contains(last.message, "wrapper failed")

	logs := logBuf.String()
	assert.Contains(logs, "workspace setup failed")
	assert.Contains(logs, wsID)
	assert.Contains(logs, "tmux_session")
}

func TestWorkspaceShutdownCancellationPersistsFailureViaAPI(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

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
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    while :; do sleep 1; done` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	client, _, database, srv := setupWrapperServerWithScriptAndDBAndServer(
		t, script,
	)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			argvs := readTmuxRecord(t, record)
			for _, argv := range argvs {
				if len(argv) >= 2 && argv[1] == "new-session" {
					return true
				}
			}
			return false
		},
		5*time.Second,
		50*time.Millisecond,
	)

	shutdownCtx, cancel := context.WithTimeout(
		t.Context(), 5*time.Second,
	)
	defer cancel()
	require.NoError(srv.Shutdown(shutdownCtx))

	restartSyncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": &mockGH{}},
		database, nil, defaultTestRepos, time.Minute, nil, nil,
	)
	t.Cleanup(restartSyncer.Stop)
	restarted := New(
		database, restartSyncer, nil, "/",
		nil, ServerOptions{WorktreeDir: filepath.Join(dir, "restart-worktrees")},
	)
	t.Cleanup(func() { gracefulShutdown(t, restarted) })
	restartedClient := setupTestClient(t, restarted)

	getResp, err := restartedClient.HTTP.GetWorkspacesByIdWithResponse(
		ctx, wsID,
	)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	assert.Equal("error", getResp.JSON200.Status)
	require.NotNil(getResp.JSON200.ErrorMessage)
	assert.Contains(*getResp.JSON200.ErrorMessage, "tmux new-session")

	rows, err := database.ReadDB().QueryContext(ctx, `
		SELECT stage, outcome, message
		FROM middleman_workspace_setup_events
		WHERE workspace_id = ?
		ORDER BY id`, wsID,
	)
	require.NoError(err)
	defer rows.Close()

	type auditEvent struct {
		stage   string
		outcome string
		message string
	}

	var events []auditEvent
	for rows.Next() {
		var ev auditEvent
		require.NoError(rows.Scan(&ev.stage, &ev.outcome, &ev.message))
		events = append(events, ev)
	}
	require.NoError(rows.Err())
	require.Len(events, 2)
	assert.Equal("setup", events[0].stage)
	assert.Equal("started", events[0].outcome)
	assert.Equal("tmux_session", events[1].stage)
	assert.Equal("failure", events[1].outcome)
	assert.Contains(events[1].message, "tmux new-session")
}

func TestWorkspaceSetupFailureRollbackCleansWorktreeViaAPI(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    echo "wrapper failed" >&2` + "\n" +
		`    exit 42` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	client, _, database, srv := setupWrapperServerWithScriptAndDBAndServer(
		t, script,
	)
	ctx := t.Context()
	clonePath := srv.clones.ClonePath("github.com", "acme", "widget")
	featureSHA := testGitSHA(t, clonePath, "refs/heads/feature")

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	var failed *generated.WorkspaceResponse
	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			require.NoError(getErr)
			if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
				return false
			}
			if getResp.JSON200.Status != "error" {
				return false
			}
			failed = getResp.JSON200
			return true
		},
		5*time.Second,
		50*time.Millisecond,
	)

	require.NotNil(failed)
	assert.Equal(featureSHA, testGitSHA(t, clonePath, "refs/heads/feature"))
	assert.Eventually(
		func() bool {
			_, err := os.Stat(failed.WorktreePath)
			return os.IsNotExist(err)
		},
		5*time.Second,
		50*time.Millisecond,
	)

	stored, err := database.GetWorkspace(ctx, wsID)
	require.NoError(err)
	require.NotNil(stored)
	assert.Equal("error", stored.Status)
	assert.Empty(stored.WorkspaceBranch)
}

func TestWorkspaceRetryWhileCreatingQueuesAndRunsAfterFailureViaAPI(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	release := filepath.Join(dir, "release-first")
	countFile := filepath.Join(dir, "new-session-count")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    count=0` + "\n" +
		`    if [ -f "$TMUX_COUNT" ]; then count=$(cat "$TMUX_COUNT"); fi` + "\n" +
		`    count=$((count + 1))` + "\n" +
		`    printf '%s' "$count" > "$TMUX_COUNT"` + "\n" +
		`    if [ "$count" = "1" ]; then` + "\n" +
		`      while [ ! -f "$TMUX_RELEASE" ]; do sleep 0.05; done` + "\n" +
		`      echo "first setup failed" >&2` + "\n" +
		`      exit 42` + "\n" +
		`    fi` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)
	t.Setenv("TMUX_RELEASE", release)
	t.Setenv("TMUX_COUNT", countFile)

	client, _, database := setupWrapperServerWithScriptAndDB(
		t, script,
	)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			argvs := readTmuxRecord(t, record)
			for _, argv := range argvs {
				if len(argv) >= 2 && argv[1] == "new-session" {
					return true
				}
			}
			return false
		},
		5*time.Second,
		50*time.Millisecond,
	)

	retryResp, err := client.HTTP.RetryWorkspaceWithResponse(ctx, wsID)
	require.NoError(err)
	require.Equal(http.StatusAccepted, retryResp.StatusCode())
	require.NotNil(retryResp.JSON202)
	assert.Equal("creating", retryResp.JSON202.Status)

	require.NoError(os.WriteFile(release, []byte("go\n"), 0o644))

	var ready *generated.WorkspaceResponse
	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			require.NoError(getErr)
			if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
				return false
			}
			if getResp.JSON200.Status != "ready" {
				return false
			}
			ready = getResp.JSON200
			return true
		},
		5*time.Second,
		50*time.Millisecond,
	)
	require.NotNil(ready)
	assert.Nil(ready.ErrorMessage)

	argvs := readTmuxRecord(t, record)
	var newSessionCount int
	for _, argv := range argvs {
		if len(argv) >= 2 && argv[1] == "new-session" {
			newSessionCount++
		}
	}
	assert.Equal(2, newSessionCount)

	events, err := database.ListWorkspaceSetupEvents(ctx, wsID)
	require.NoError(err)
	var retryEvents int
	for _, event := range events {
		if event.Stage == "setup" && event.Outcome == "retrying" {
			retryEvents++
		}
	}
	assert.Equal(1, retryEvents)
}

func TestWorkspaceShutdownCancellationDoesNotPersistAfterDeadlineBudgetExhausted(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

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
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    while :; do sleep 1; done` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	client, baseURL, database, srv := setupWrapperServerWithScriptAndDBAndServer(
		t, script,
	)

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		t.Context(),
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			argvs := readTmuxRecord(t, record)
			for _, argv := range argvs {
				if len(argv) >= 2 && argv[1] == "new-session" {
					return true
				}
			}
			return false
		},
		5*time.Second,
		50*time.Millisecond,
	)

	tx, err := database.WriteDB().BeginTx(t.Context(), nil)
	require.NoError(err)
	t.Cleanup(func() { _ = tx.Rollback() })

	origHandler := srv.handler
	blockStarted := make(chan struct{}, 1)
	blockRelease := make(chan struct{})
	srv.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block" {
			select {
			case blockStarted <- struct{}{}:
			default:
			}
			<-blockRelease
			w.WriteHeader(http.StatusOK)
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	blockErrCh := make(chan error, 1)
	go func() {
		resp, err := http.Get(baseURL + "/block")
		if err == nil && resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		blockErrCh <- err
	}()

	select {
	case <-blockStarted:
	case <-time.After(2 * time.Second):
		require.FailNow("blocking request never started")
	}

	time.AfterFunc(250*time.Millisecond, func() {
		close(blockRelease)
	})

	shutdownCtx, cancel := context.WithTimeout(
		t.Context(), 400*time.Millisecond,
	)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	require.ErrorIs(err, context.DeadlineExceeded)

	require.NoError(tx.Rollback())

	time.Sleep(300 * time.Millisecond)

	ws, err := database.GetWorkspace(t.Context(), wsID)
	require.NoError(err)
	require.NotNil(ws)
	assert.Equal("creating", ws.Status)
	assert.Nil(ws.ErrorMessage)

	events, err := database.ListWorkspaceSetupEvents(
		t.Context(), wsID,
	)
	require.NoError(err)
	require.Len(events, 1)
	assert.Equal("setup", events[0].Stage)
	assert.Equal("started", events[0].Outcome)

	longCtx, longCancel := context.WithTimeout(
		t.Context(), 2*time.Second,
	)
	defer longCancel()
	require.NoError(srv.Shutdown(longCtx))
	require.NoError(<-blockErrCh)
}

func TestTmuxWrapperAttachSession(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	client, baseURL, record := setupWrapperServer(t)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll for status == "ready".
	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "ready"
		},
		5*time.Second, 50*time.Millisecond,
	)

	// Connect to the WebSocket terminal endpoint using the
	// httptest baseURL (the generated client cannot upgrade to
	// WebSocket, so we dial directly with coder/websocket).
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) +
		"/api/v1/workspaces/" + wsID + "/terminal"
	dialCtx, dialCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer dialCancel()
	u, err := url.Parse(wsURL)
	require.NoError(err)
	conn, httpResp, err := websocket.Dial(
		dialCtx, u.String(), nil,
	)
	require.NoError(err)
	if httpResp != nil && httpResp.Body != nil {
		httpResp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// The recording script exits 0 immediately, so the PTY
	// closes and the handler sends an "exited" message. Read
	// until the connection closes or 3s elapses.
	readCtx, readCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer readCancel()
	for {
		_, _, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
	}

	// The recorded argv should contain an attach-session invocation
	// with our "wrap" prefix.
	var attach []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 2 && argv[1] == "attach-session" {
			attach = argv
			break
		}
	}
	require.NotNil(attach, "attach-session argv not recorded")
	require.Len(attach, 4)
	assert.Equal("wrap", attach[0])
	assert.Equal("attach-session", attach[1])
	assert.Equal("-t", attach[2])
	assert.NotEmpty(attach[3])
}

func TestWorkspaceSetupResourceExhaustionGetsHelpfulErrorViaAPI(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    echo "fork/exec /opt/homebrew/bin/tmux: resource temporarily unavailable" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	client, _, _ := setupWrapperServerWithScriptAndDB(t, script)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	var failed *generated.WorkspaceResponse
	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			require.NoError(getErr)
			if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
				return false
			}
			if getResp.JSON200.Status != "error" {
				return false
			}
			failed = getResp.JSON200
			return true
		},
		5*time.Second, 50*time.Millisecond,
	)
	require.NotNil(failed)
	require.NotNil(failed.ErrorMessage)
	assert.Contains(*failed.ErrorMessage, "host process limit reached")
}

// TestReadTmuxRecordPreservesEmptyArgs pins down the parser's
// empty-arg handling. The NUL-delimited record format was chosen to
// round-trip argv with empty-string elements unambiguously; the
// parser must keep interior and trailing empties rather than
// collapsing them.
func TestReadTmuxRecordPreservesEmptyArgs(t *testing.T) {
	assert := Assert.New(t)
	require := require.New(t)
	path := filepath.Join(t.TempDir(), "record")

	// First record: 3 args with an interior empty ("a", "", "b").
	// Second record: 2 args with a trailing empty ("x", "").
	body := "3\x00a\x00\x00b\x00" + "2\x00x\x00\x00"
	require.NoError(os.WriteFile(path, []byte(body), 0o644))

	argvs := readTmuxRecord(t, path)
	require.Len(argvs, 2)
	assert.Equal([]string{"a", "", "b"}, argvs[0])
	assert.Equal([]string{"x", ""}, argvs[1])
}

// TestTmuxWrapperKillSession proves the configured tmux.command
// prefix reaches the kill-session exec issued by DELETE /workspaces/{id}.
// This complements TestTmuxWrapperNewSession and TestTmuxWrapperAttachSession —
// together they cover all three tmux verbs that cross the HTTP boundary.
func TestTmuxWrapperKillSession(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll for status == "ready" before deleting so the tmux
	// session is known to exist from the manager's perspective.
	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "ready"
		},
		5*time.Second, 50*time.Millisecond,
	)

	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delResp.StatusCode())

	// The recorded argv should contain a kill-session invocation
	// with our "wrap" prefix.
	var kill []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 2 && argv[1] == "kill-session" {
			kill = argv
			break
		}
	}
	require.NotNil(kill, "kill-session argv not recorded")
	require.Len(kill, 4)
	assert.Equal("wrap", kill[0])
	assert.Equal("kill-session", kill[1])
	assert.Equal("-t", kill[2])
	assert.NotEmpty(kill[3])
}

func TestDeleteWorkspacePreservesRowWhenTmuxKillFails(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "kill-session" ]; then` + "\n" +
		`    echo "permission denied" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	client, _, _ := setupWrapperServerWithScriptAndDB(t, script)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "ready"
		},
		5*time.Second, 50*time.Millisecond,
	)

	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusInternalServerError, delResp.StatusCode())
	require.NotNil(delResp.ApplicationproblemJSONDefault)
	require.NotNil(delResp.ApplicationproblemJSONDefault.Detail)
	assert.Contains(
		*delResp.ApplicationproblemJSONDefault.Detail,
		"kill tmux session",
	)
	assert.Contains(
		*delResp.ApplicationproblemJSONDefault.Detail,
		"permission denied",
	)

	getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(ctx, wsID)
	require.NoError(err)
	require.Equal(http.StatusOK, getResp.StatusCode())
	require.NotNil(getResp.JSON200)
	assert.Equal(wsID, getResp.JSON200.Id)
}

func TestDeleteErroredWorkspaceAllowsUnavailableTmux(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "new-session" ]; then` + "\n" +
		`    echo "tmux unavailable" >&2` + "\n" +
		`    exit 127` + "\n" +
		`  fi` + "\n" +
		`  if [ "$a" = "kill-session" ]; then` + "\n" +
		`    echo "tmux unavailable" >&2` + "\n" +
		`    exit 127` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(os.WriteFile(script, []byte(body), 0o755))

	client, _, _ := setupWrapperServerWithScriptAndDB(t, script)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "error"
		},
		5*time.Second, 50*time.Millisecond,
	)

	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(err)
	require.Equal(http.StatusNoContent, delResp.StatusCode())

	getResp, err := client.HTTP.GetWorkspacesByIdWithResponse(ctx, wsID)
	require.NoError(err)
	assert.Equal(http.StatusNotFound, getResp.StatusCode())
}

// TestTmuxWrapperAttachSurfacesWrapperFailure exercises the
// error-propagation path end-to-end. Workspace setup uses a wrapper
// that succeeds for new-session (so the workspace reaches "ready")
// but fails has-session with exit code 127 — the kind of exit a
// broken wrapper like systemd-run would produce. Under the old
// boolean-only tmuxSessionExists, this silently passed through as
// "session absent" and the bug hid behind a confusing new-session
// failure. With the bool/error split plus the exit-code-1 carve-out,
// the terminal handler sees the error and closes the WebSocket with
// StatusInternalError.
func TestTmuxWrapperAttachSurfacesWrapperFailure(t *testing.T) {
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then exit 127; fi` + "\n" +
		"done\n" +
		"exit 0\n"
	attachWebsocketAndExpectInternalError(t, body)
}

// attachWebsocketAndExpectInternalError drives the end-to-end
// attach path with a custom fake-tmux script, asserting the
// WebSocket is closed by the handler with StatusInternalError
// rather than attaching to a session. Callers provide the script
// body; the helper handles server setup, workspace creation,
// ready-polling, dial, and close-status assertion.
func attachWebsocketAndExpectInternalError(t *testing.T, scriptBody string) {
	t.Helper()
	assert := Assert.New(t)
	require := require.New(t)

	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	require.NoError(os.WriteFile(script, []byte(scriptBody), 0o755))
	t.Setenv("TMUX_RECORD", record)

	client, baseURL := setupWrapperServerWithScript(t, script)
	ctx := t.Context()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(err)
	require.Equal(http.StatusAccepted, createResp.StatusCode())
	require.NotNil(createResp.JSON202)
	wsID := createResp.JSON202.Id

	require.Eventually(
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "ready"
		},
		5*time.Second, 50*time.Millisecond,
	)

	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) +
		"/api/v1/workspaces/" + wsID + "/terminal"
	dialCtx, dialCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer dialCancel()
	u, err := url.Parse(wsURL)
	require.NoError(err)
	conn, httpResp, err := websocket.Dial(
		dialCtx, u.String(), nil,
	)
	require.NoError(err)
	if httpResp != nil && httpResp.Body != nil {
		httpResp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	readCtx, readCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer readCancel()
	_, _, readErr := conn.Read(readCtx)
	require.Error(readErr)
	assert.Equal(
		websocket.StatusInternalError,
		websocket.CloseStatus(readErr),
	)
}

// TestTmuxWrapperAttachSurfacesExit1Failure covers the second half
// of the session-absent heuristic at the HTTP layer: exit code 1
// without tmux's "can't find session" or "no server running"
// stderr must be treated as a real wrapper failure, not as
// "session absent, please create one." This is the common case the
// reviewer flagged — shell wrappers often exit 1 for their own
// generic errors.
func TestTmuxWrapperAttachSurfacesExit1Failure(t *testing.T) {
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "wrapper failed" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	attachWebsocketAndExpectInternalError(t, body)
}

// TestTmuxWrapperAttachIgnoresAbsencePhraseOnStdout verifies that
// the absent-session heuristic is stderr-only at the HTTP layer:
// a wrapper that exits 1 with the tmux phrase on stdout (and an
// unrelated stderr message) must surface as an error, not as
// "session absent." Pairs with the unit-level
// TestManagerEnsureTmuxIgnoresAbsencePhraseOnStdout.
func TestTmuxWrapperAttachIgnoresAbsencePhraseOnStdout(t *testing.T) {
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim"` + "\n" + // stdout only
		`    echo "real failure" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	attachWebsocketAndExpectInternalError(t, body)
}
