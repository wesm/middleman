package localruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagerLaunchesSingletonPerWorkspaceTarget(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	ctx := context.Background()
	mgr := NewManager(Options{Targets: []LaunchTarget{
		helperTarget("helper", "sleep"),
	}})
	t.Cleanup(mgr.Shutdown)

	session1, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "helper")
	require.NoError(t, err)
	session2, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "helper")
	require.NoError(t, err)

	sessions := mgr.ListSessions("ws-1")
	assert := Assert.New(t)
	assert.Equal(session1.Key, session2.Key)
	assert.Equal(SessionStatusRunning, session1.Status)
	assert.Len(sessions, 1)
}

func TestManagerLaunchConcurrentStartsOneProcess(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")
	require := require.New(t)
	assert := Assert.New(t)

	ctx := context.Background()
	record := filepath.Join(t.TempDir(), "starts")
	mgr := NewManager(Options{Targets: []LaunchTarget{
		{
			Key: "helper", Label: "helper", Kind: LaunchTargetAgent,
			Source: "config", Command: helperRecordCommand(record),
			Available: true,
		},
	}})
	t.Cleanup(mgr.Shutdown)

	const launches = 12
	var wg sync.WaitGroup
	errs := make(chan error, launches)
	infos := make(chan SessionInfo, launches)
	cwd := t.TempDir()
	for range launches {
		wg.Go(func() {
			info, err := mgr.Launch(ctx, "ws-1", cwd, "helper")
			errs <- err
			infos <- info
		})
	}
	wg.Wait()
	close(errs)
	close(infos)

	for err := range errs {
		require.NoError(err)
	}
	var firstKey string
	for info := range infos {
		if firstKey == "" {
			firstKey = info.Key
		}
		assert.Equal(firstKey, info.Key)
	}
	require.Eventually(func() bool {
		data, err := os.ReadFile(record)
		if err != nil {
			return false
		}
		return strings.Count(string(data), "\n") == 1
	}, 2*time.Second, 20*time.Millisecond)
	assert.Len(mgr.ListSessions("ws-1"), 1)
}

func TestManagerLaunchUnavailableTarget(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(Options{Targets: []LaunchTarget{{
		Key: "missing", Label: "Missing", Kind: LaunchTargetAgent,
		Available: false, DisabledReason: "not found",
	}}})
	t.Cleanup(mgr.Shutdown)

	_, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not available")
}

func TestManagerLaunchMissingTarget(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(Options{})
	t.Cleanup(mgr.Shutdown)

	_, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "missing")
	require.Error(t, err)
	require.Contains(t, err.Error(), "target not found")
}

func TestManagerStopRemovesSession(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	ctx := context.Background()
	mgr := NewManager(Options{Targets: []LaunchTarget{
		helperTarget("helper", "sleep"),
	}})
	t.Cleanup(mgr.Shutdown)

	session, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "helper")
	require.NoError(t, err)
	require.NoError(t, mgr.Stop(ctx, "ws-1", session.Key))

	assert := Assert.New(t)
	assert.Empty(mgr.ListSessions("ws-1"))
	assert.Error(mgr.Stop(ctx, "ws-1", session.Key))
}

func TestManagerReportsExitedProcess(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	ctx := context.Background()
	mgr := NewManager(Options{Targets: []LaunchTarget{
		helperTarget("helper", "exit"),
	}})
	t.Cleanup(mgr.Shutdown)

	session, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "helper")
	require.NoError(t, err)

	var got SessionInfo
	require.Eventually(t, func() bool {
		sessions := mgr.ListSessions("ws-1")
		if len(sessions) != 1 {
			return false
		}
		got = sessions[0]
		return got.Status == SessionStatusExited
	}, 2*time.Second, 20*time.Millisecond)

	assert := Assert.New(t)
	assert.Equal(session.Key, got.Key)
	assert.NotNil(got.ExitedAt)
	assert.NotNil(got.ExitCode)
	assert.Equal(3, *got.ExitCode)
}

func TestManagerShellSingletonPerWorkspace(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	ctx := context.Background()
	mgr := NewManager(Options{
		ShellCommand: helperCommand("sleep"),
	})
	t.Cleanup(mgr.Shutdown)

	shell1, err := mgr.EnsureShell(ctx, "ws-1", t.TempDir())
	require.NoError(t, err)
	shell2, err := mgr.EnsureShell(ctx, "ws-1", t.TempDir())
	require.NoError(t, err)

	assert := Assert.New(t)
	assert.Equal(shell1.Key, shell2.Key)
	assert.Equal(SessionStatusRunning, shell1.Status)
	assert.Empty(mgr.ListSessions("ws-1"))
}

func TestManagerShellConcurrentStartsOneProcess(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")
	require := require.New(t)
	assert := Assert.New(t)

	ctx := context.Background()
	record := filepath.Join(t.TempDir(), "shell-starts")
	mgr := NewManager(Options{
		ShellCommand: helperRecordCommand(record),
	})
	t.Cleanup(mgr.Shutdown)

	const launches = 12
	var wg sync.WaitGroup
	errs := make(chan error, launches)
	infos := make(chan SessionInfo, launches)
	cwd := t.TempDir()
	for range launches {
		wg.Go(func() {
			info, err := mgr.EnsureShell(ctx, "ws-1", cwd)
			errs <- err
			infos <- info
		})
	}
	wg.Wait()
	close(errs)
	close(infos)

	for err := range errs {
		require.NoError(err)
	}
	var firstKey string
	for info := range infos {
		if firstKey == "" {
			firstKey = info.Key
		}
		assert.Equal(firstKey, info.Key)
	}
	require.Eventually(func() bool {
		data, err := os.ReadFile(record)
		if err != nil {
			return false
		}
		return strings.Count(string(data), "\n") == 1
	}, 2*time.Second, 20*time.Millisecond)
	assert.NotNil(mgr.ShellSession("ws-1"))
}

func TestManagerShutdownRejectsNewLaunches(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	mgr := NewManager(Options{Targets: []LaunchTarget{
		helperTarget("helper", "sleep"),
	}})
	t.Cleanup(mgr.Shutdown)

	mgr.Shutdown()

	_, err := mgr.Launch(
		context.Background(), "ws-1", t.TempDir(), "helper",
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "runtime manager is shut down")
}

func TestSessionBroadcastClosesSlowSubscriber(t *testing.T) {
	s := &session{
		subscribers: make(map[chan []byte]struct{}),
	}
	ch := make(chan []byte, 1)
	ch <- []byte("queued")
	s.subscribers[ch] = struct{}{}

	s.broadcast([]byte("new"))

	got := <-ch
	assert := Assert.New(t)
	assert.Equal([]byte("queued"), got)
	select {
	case _, ok := <-ch:
		assert.False(ok)
	case <-time.After(100 * time.Millisecond):
		assert.Fail("slow subscriber was not closed")
	}
	s.mu.Lock()
	_, subscribed := s.subscribers[ch]
	s.mu.Unlock()
	assert.False(subscribed)
}

func TestSessionSubscribeReplaysBufferedOutput(t *testing.T) {
	s := &session{
		subscribers: make(map[chan []byte]struct{}),
	}
	s.broadcast([]byte("startup-banner\r\n"))
	s.broadcast([]byte("$ "))

	ch, cancel := s.subscribe()
	t.Cleanup(cancel)

	assert := Assert.New(t)
	select {
	case data := <-ch:
		assert.Equal("startup-banner\r\n$ ", string(data))
	case <-time.After(100 * time.Millisecond):
		assert.Fail("subscriber did not receive replay")
	}

	s.broadcast([]byte("ls\r\n"))
	select {
	case data := <-ch:
		assert.Equal("ls\r\n", string(data))
	case <-time.After(100 * time.Millisecond):
		assert.Fail("subscriber did not receive new output after replay")
	}
}

func TestSessionSubscribeAfterCloseStillReplays(t *testing.T) {
	s := &session{
		subscribers: make(map[chan []byte]struct{}),
	}
	s.broadcast([]byte("hello\r\nbye\r\n"))
	s.closeSubscribers()

	ch, cancel := s.subscribe()
	t.Cleanup(cancel)

	assert := Assert.New(t)
	select {
	case data, ok := <-ch:
		assert.True(ok)
		assert.Equal("hello\r\nbye\r\n", string(data))
	case <-time.After(100 * time.Millisecond):
		assert.Fail("expected replay before channel close")
	}
	select {
	case _, ok := <-ch:
		assert.False(ok)
	case <-time.After(100 * time.Millisecond):
		assert.Fail("expected channel to close after replay")
	}
}

func TestSessionOutputBufferIsBounded(t *testing.T) {
	s := &session{
		subscribers: make(map[chan []byte]struct{}),
	}
	chunk := make([]byte, 8*1024)
	for i := range chunk {
		chunk[i] = 'x'
	}
	for range 12 {
		s.broadcast(chunk)
	}

	s.mu.Lock()
	bufLen := len(s.outputBuffer)
	s.mu.Unlock()
	Assert.New(t).LessOrEqual(bufLen, maxSessionOutputReplay)
}

func TestManagerStopWorkspaceStopsAllSessions(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")

	require := require.New(t)
	assert := Assert.New(t)

	ctx := context.Background()
	mgr := NewManager(Options{
		Targets: []LaunchTarget{
			helperTarget("agent-a", "sleep"),
			helperTarget("agent-b", "sleep"),
		},
		ShellCommand: helperCommand("sleep"),
	})
	t.Cleanup(mgr.Shutdown)

	_, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "agent-a")
	require.NoError(err)
	_, err = mgr.Launch(ctx, "ws-1", t.TempDir(), "agent-b")
	require.NoError(err)
	_, err = mgr.EnsureShell(ctx, "ws-1", t.TempDir())
	require.NoError(err)

	// A second workspace's sessions must survive.
	_, err = mgr.Launch(ctx, "ws-2", t.TempDir(), "agent-a")
	require.NoError(err)

	mgr.StopWorkspace(ctx, "ws-1")

	assert.Empty(mgr.ListSessions("ws-1"))
	assert.Nil(mgr.ShellSession("ws-1"))
	assert.Len(mgr.ListSessions("ws-2"), 1)
}

func helperTarget(key, mode string) LaunchTarget {
	return LaunchTarget{
		Key: key, Label: key, Kind: LaunchTargetAgent,
		Source: "config", Command: helperCommand(mode),
		Available: true,
	}
}

func helperRecordCommand(record string) []string {
	return []string{
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
		"sleep-record",
		record,
	}
}

func helperCommand(mode string) []string {
	return []string{
		os.Args[0],
		"-test.run=TestHelperProcess",
		"--",
		mode,
	}
}

// TestResolveExecutableRejectsRelativePaths ensures startSession
// refuses commands that would resolve inside the workspace worktree
// (PR-controlled content). Absolute paths and PATH-resolvable
// names are accepted; relative names with separators are rejected.
func TestResolveExecutableRejectsRelativePaths(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	// Absolute path: pass through unchanged.
	got, err := resolveExecutable("/usr/local/bin/codex")
	require.NoError(err)
	assert.Equal("/usr/local/bin/codex", got)

	// PATH-resolvable: returns the full path. /bin/sh is present
	// on every supported platform.
	got, err = resolveExecutable("sh")
	require.NoError(err)
	assert.True(filepath.IsAbs(got), "expected absolute path, got %q", got)

	// Relative paths must be rejected.
	for _, rel := range []string{
		"./agent",
		"../scripts/codex",
		"scripts/codex",
		"a/b",
	} {
		_, err := resolveExecutable(rel)
		require.Error(err, "expected error for %q", rel)
		assert.Contains(err.Error(), "absolute path")
	}

	// Empty name.
	_, err = resolveExecutable("")
	require.Error(err)

	// Bare name not on PATH should surface a LookPath error.
	_, err = resolveExecutable(
		"middleman-localruntime-bogus-name-zzz",
	)
	require.Error(err)
}

// TestSessionEnvironmentStripsCredentials verifies that the
// environment passed to runtime sessions has GitHub-token-shaped
// variables removed so that launched agents cannot exfiltrate
// the maintainer's credentials.
func TestSessionEnvironmentStripsCredentials(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	in := []string{
		"PATH=/usr/bin",
		"HOME=/home/me",
		"MIDDLEMAN_GITHUB_TOKEN=secret-1",
		"GITHUB_TOKEN=secret-2",
		"GH_TOKEN=secret-3",
		"GITHUB_PAT=secret-4",
		"GH_PAT=secret-5",
		"GITHUB_ENTERPRISE_TOKEN=secret-6",
		"GH_ENTERPRISE_TOKEN=secret-7",
		"GITHUB_TOKEN_GHE=secret-8",
		"NOTSECRET=ok",
	}
	out := sessionEnvironment(in)

	require.Contains(out, "PATH=/usr/bin")
	require.Contains(out, "HOME=/home/me")
	require.Contains(out, "NOTSECRET=ok")

	for _, kv := range out {
		assert.NotContains(
			kv, "secret-",
			"credential leaked through sessionEnvironment: %q", kv,
		)
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("MIDDLEMAN_LOCALRUNTIME_HELPER") != "1" {
		return
	}
	args := os.Args
	helperArgs := args[len(args)-1:]
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			helperArgs = args[i+1:]
			break
		}
	}
	mode := helperArgs[0]
	switch mode {
	case "sleep":
		select {}
	case "sleep-record":
		if len(helperArgs) < 2 {
			os.Exit(2)
		}
		f, err := os.OpenFile(
			helperArgs[1],
			os.O_APPEND|os.O_CREATE|os.O_WRONLY,
			0o644,
		)
		if err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
		_ = f.Close()
		select {}
	case "exit":
		os.Exit(3)
	default:
		os.Exit(2)
	}
}
