package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/gitclone"
)

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
	if testing.Short() {
		t.Skip("e2e tests skipped in short mode")
	}

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
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
	srv := New(database, syncer, nil, "/", cfg, ServerOptions{
		Clones:      clones,
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

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

	return client, ts.URL
}

func TestTmuxWrapperNewSession(t *testing.T) {
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)
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
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)

	// Workspace setup runs asynchronously. Poll the record file
	// until the new-session invocation shows up, up to ~5s.
	var argvs [][]string
	require.Eventually(
		t,
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
	require.GreaterOrEqual(t, len(newSession), 9)
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

func TestTmuxWrapperAttachSession(t *testing.T) {
	assert := Assert.New(t)
	client, baseURL, record := setupWrapperServer(t)
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
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll for status == "ready".
	require.Eventually(
		t,
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
	require.NoError(t, err)
	conn, httpResp, err := websocket.Dial(
		dialCtx, u.String(), nil,
	)
	require.NoError(t, err)
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
	require.NotNil(t, attach, "attach-session argv not recorded")
	require.Len(t, attach, 4)
	assert.Equal("wrap", attach[0])
	assert.Equal("attach-session", attach[1])
	assert.Equal("-t", attach[2])
	assert.NotEmpty(attach[3])
}

// TestReadTmuxRecordPreservesEmptyArgs pins down the parser's
// empty-arg handling. The NUL-delimited record format was chosen to
// round-trip argv with empty-string elements unambiguously; the
// parser must keep interior and trailing empties rather than
// collapsing them.
func TestReadTmuxRecordPreservesEmptyArgs(t *testing.T) {
	assert := Assert.New(t)
	path := filepath.Join(t.TempDir(), "record")

	// First record: 3 args with an interior empty ("a", "", "b").
	// Second record: 2 args with a trailing empty ("x", "").
	body := "3\x00a\x00\x00b\x00" + "2\x00x\x00\x00"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	argvs := readTmuxRecord(t, path)
	require.Len(t, argvs, 2)
	assert.Equal([]string{"a", "", "b"}, argvs[0])
	assert.Equal([]string{"x", ""}, argvs[1])
}

// TestTmuxWrapperKillSession proves the configured tmux.command
// prefix reaches the kill-session exec issued by DELETE /workspaces/{id}.
// This complements TestTmuxWrapperNewSession and TestTmuxWrapperAttachSession —
// together they cover all three tmux verbs that cross the HTTP boundary.
func TestTmuxWrapperKillSession(t *testing.T) {
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)
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
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll for status == "ready" before deleting so the tmux
	// session is known to exist from the manager's perspective.
	require.Eventually(
		t,
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
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, delResp.StatusCode())

	// The recorded argv should contain a kill-session invocation
	// with our "wrap" prefix.
	var kill []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 2 && argv[1] == "kill-session" {
			kill = argv
			break
		}
	}
	require.NotNil(t, kill, "kill-session argv not recorded")
	require.Len(t, kill, 4)
	assert.Equal("wrap", kill[0])
	assert.Equal("kill-session", kill[1])
	assert.Equal("-t", kill[2])
	assert.NotEmpty(kill[3])
}
