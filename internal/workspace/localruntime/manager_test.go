package localruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

func TestManagerTmuxSessionsReturnsWrappedAgentSessions(t *testing.T) {
	assert := Assert.New(t)
	mgr := NewManager(Options{})
	mgr.sessions["ws-1:codex"] = &session{
		info: SessionInfo{
			Key:         "ws-1:codex",
			WorkspaceID: "ws-1",
			TargetKey:   "codex",
			Kind:        LaunchTargetAgent,
		},
		tmuxSession: "middleman-ws-1-codex",
	}
	mgr.sessions["ws-1:direct"] = &session{
		info: SessionInfo{
			Key:         "ws-1:direct",
			WorkspaceID: "ws-1",
			TargetKey:   "direct",
			Kind:        LaunchTargetAgent,
		},
	}
	mgr.sessions["ws-2:codex"] = &session{
		info: SessionInfo{
			Key:         "ws-2:codex",
			WorkspaceID: "ws-2",
			TargetKey:   "codex",
			Kind:        LaunchTargetAgent,
		},
		tmuxSession: "middleman-ws-2-codex",
	}

	assert.Equal(
		[]string{"middleman-ws-1-codex"},
		mgr.TmuxSessions("ws-1"),
	)
}

func TestManagerLaunchCommandWrapsAgentsInTmuxWhenEnabled(t *testing.T) {
	assert := Assert.New(t)
	agent := helperTarget("codex", "sleep")
	agent.Label = "Codex"
	mgr := NewManager(Options{
		Targets: []LaunchTarget{
			agent,
			{
				Key: "tmux", Label: "tmux", Kind: LaunchTargetTmux,
				Source: "system", Command: []string{"/usr/bin/tmux"},
				Available: true,
			},
		},
		TmuxCommand:             []string{"/usr/bin/tmux"},
		WrapAgentSessionsInTmux: true,
	})
	t.Cleanup(mgr.Shutdown)

	launch, err := mgr.launchCommand(
		agent, "ws:alpha", "/tmp/work tree",
	)
	require.NoError(t, err)

	assert.Equal("/usr/bin/tmux", launch.Command[0])
	assert.Equal(
		[]string{
			"new-session",
			"-A",
			"-s",
			"middleman-ws-alpha-codex",
			"-c",
			"/tmp/work tree",
		},
		launch.Command[1:7],
	)
	assert.Contains(launch.Command[7], "exec ")
	assert.Contains(launch.Command[7], shellQuote(agent.Command[0]))
	assert.Equal("middleman-ws-alpha-codex", launch.TmuxSession)
}

func TestManagerLaunchCommandFallsBackWhenTmuxUnavailable(t *testing.T) {
	assert := Assert.New(t)
	agent := helperTarget("codex", "sleep")
	mgr := NewManager(Options{
		Targets: []LaunchTarget{
			agent,
			{
				Key: "tmux", Label: "tmux", Kind: LaunchTargetTmux,
				Source: "system", Command: []string{"tmux"},
				Available: false, DisabledReason: "tmux not found",
			},
		},
		TmuxCommand:             []string{"tmux"},
		WrapAgentSessionsInTmux: true,
	})
	t.Cleanup(mgr.Shutdown)

	launch, err := mgr.launchCommand(agent, "ws-1", t.TempDir())
	require.NoError(t, err)

	assert.Equal(agent.Command, launch.Command)
	assert.Empty(launch.TmuxSession)
}

func TestManagerLaunchCommandDoesNotWrapWhenConfigDisabled(t *testing.T) {
	assert := Assert.New(t)
	agent := helperTarget("codex", "sleep")
	mgr := NewManager(Options{
		Targets: []LaunchTarget{
			agent,
			{
				Key: "tmux", Label: "tmux", Kind: LaunchTargetTmux,
				Source: "system", Command: []string{"/usr/bin/tmux"},
				Available: true,
			},
		},
		TmuxCommand:             []string{"/usr/bin/tmux"},
		WrapAgentSessionsInTmux: false,
	})
	t.Cleanup(mgr.Shutdown)

	launch, err := mgr.launchCommand(agent, "ws-1", t.TempDir())
	require.NoError(t, err)

	assert.Equal(agent.Command, launch.Command)
	assert.Empty(launch.TmuxSession)
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

func TestManagerLaunchRejectsWhileWorkspaceStopping(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")
	require := require.New(t)
	assert := Assert.New(t)

	record := filepath.Join(t.TempDir(), "pids")
	mgr := NewManager(Options{Targets: []LaunchTarget{{
		Key: "helper", Label: "helper", Kind: LaunchTargetAgent,
		Source: "config", Available: true,
		Command: helperRecordCommand(record),
	}}})
	t.Cleanup(mgr.Shutdown)

	mgr.mu.Lock()
	mgr.stoppingWS["ws-1"] = 1
	mgr.mu.Unlock()

	_, err := mgr.Launch(context.Background(), "ws-1", t.TempDir(), "helper")
	require.ErrorIs(err, errWorkspaceStopping)
	assert.Empty(mgr.ListSessions("ws-1"))

	// Whatever PID the helper recorded before being killed must be
	// gone — no orphan from the rejected launch.
	assert.Eventually(func() bool {
		data, err := os.ReadFile(record)
		if err != nil || len(data) == 0 {
			return true // helper died before recording
		}
		for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
			pid, _ := strconv.Atoi(line)
			if pid > 0 && syscall.Kill(pid, 0) == nil {
				return false
			}
		}
		return true
	}, 5*time.Second, 25*time.Millisecond,
		"rejected launch's helper process must be reaped")

	// Launches succeed again once the marker clears.
	mgr.mu.Lock()
	delete(mgr.stoppingWS, "ws-1")
	mgr.mu.Unlock()
	_, err = mgr.Launch(context.Background(), "ws-1", t.TempDir(), "helper")
	require.NoError(err)
}

func TestBeginStoppingRejectsLaunchUntilEnd(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")
	require := require.New(t)

	mgr := NewManager(Options{Targets: []LaunchTarget{
		helperTarget("helper", "sleep"),
	}})
	t.Cleanup(mgr.Shutdown)

	mgr.BeginStopping("ws-1")
	_, err := mgr.Launch(context.Background(), "ws-1", t.TempDir(), "helper")
	require.ErrorIs(err, errWorkspaceStopping)

	// Other workspaces are unaffected.
	_, err = mgr.Launch(context.Background(), "ws-2", t.TempDir(), "helper")
	require.NoError(err)

	mgr.EndStopping("ws-1")
	_, err = mgr.Launch(context.Background(), "ws-1", t.TempDir(), "helper")
	require.NoError(err)
}

func TestStopWorkspaceWaitsForInflightLaunches(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	mgr := NewManager(Options{})
	t.Cleanup(mgr.Shutdown)

	// Simulate a Launch that already passed claimInflight but has
	// not yet returned (i.e. still inside startSession). Without
	// the drain, StopWorkspace would snapshot empty sessions and
	// finish; the in-flight launch would then insert a session
	// after the workspace was supposedly stopped.
	mgr.mu.Lock()
	mgr.inflightWS["ws-1"] = 1
	mgr.mu.Unlock()

	stopReturned := make(chan struct{})
	go func() {
		mgr.StopWorkspace(context.Background(), "ws-1")
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
		require.FailNow(
			"StopWorkspace returned before inflight launch drained",
		)
	case <-time.After(75 * time.Millisecond):
	}

	mgr.releaseInflight("ws-1")

	select {
	case <-stopReturned:
	case <-time.After(2 * time.Second):
		require.FailNow(
			"StopWorkspace did not return after inflight drained",
		)
	}

	// And the marker is cleared, so subsequent launches are not
	// permanently rejected.
	mgr.mu.Lock()
	stopping := mgr.stoppingWS["ws-1"]
	mgr.mu.Unlock()
	assert.Equal(0, stopping)
}

func TestManagerStopKillsDescendantProcesses(t *testing.T) {
	t.Setenv("MIDDLEMAN_LOCALRUNTIME_HELPER", "1")
	require := require.New(t)
	assert := Assert.New(t)

	record := filepath.Join(t.TempDir(), "pids")
	ctx := context.Background()
	mgr := NewManager(Options{Targets: []LaunchTarget{{
		Key: "helper", Label: "helper", Kind: LaunchTargetAgent,
		Source: "config", Available: true,
		Command: []string{
			os.Args[0],
			"-test.run=TestHelperProcess",
			"--",
			"spawn-child",
			record,
		},
	}}})
	t.Cleanup(mgr.Shutdown)

	session, err := mgr.Launch(ctx, "ws-1", t.TempDir(), "helper")
	require.NoError(err)

	var parentPID, childPID int
	require.Eventually(func() bool {
		data, err := os.ReadFile(record)
		if err != nil {
			return false
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) < 2 {
			return false
		}
		parentPID, _ = strconv.Atoi(lines[0])
		childPID, _ = strconv.Atoi(lines[1])
		return parentPID > 0 && childPID > 0
	}, 5*time.Second, 25*time.Millisecond, "helper should record both pids")

	require.NoError(syscall.Kill(parentPID, 0), "parent should be alive")
	require.NoError(syscall.Kill(childPID, 0), "child should be alive")

	require.NoError(mgr.Stop(ctx, "ws-1", session.Key))

	assert.Eventually(func() bool {
		return errors.Is(syscall.Kill(parentPID, 0), syscall.ESRCH) &&
			errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH)
	}, 5*time.Second, 25*time.Millisecond,
		"descendant child should die with the session leader")
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

func TestResolveExecutableForcesAbsoluteFromRelativePATH(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(os.MkdirAll(binDir, 0o755))
	exe := filepath.Join(binDir, "fake-runtime-tool")
	require.NoError(os.WriteFile(exe, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	t.Chdir(dir)
	t.Setenv("PATH", "bin")
	// Recent Go versions wrap LookPath results from relative PATH
	// entries with ErrDot. With execerrdot=0 they're returned with
	// no error — that's exactly the case where the worktree-cwd
	// rebinding is dangerous, so verify the absolute fallback runs.
	t.Setenv("GODEBUG", "execerrdot=0")

	got, err := resolveExecutable("fake-runtime-tool")
	require.NoError(err)
	assert.True(
		filepath.IsAbs(got),
		"expected absolute path, got %q (relative would resolve "+
			"inside cmd.Dir = the workspace worktree)",
		got,
	)
	assert.Equal(exe, got)
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
		"MAINTAINER_PERSONAL_GH_PAT=secret-9",
		"NOTSECRET=ok",
	}
	out := sessionEnvironment(in, []string{
		"MAINTAINER_PERSONAL_GH_PAT",
	})

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

func TestSessionEnvironmentStripsConfiguredTokenEnv(t *testing.T) {
	require := require.New(t)
	in := []string{
		"PATH=/usr/bin",
		"WORK_GH_BOT_TOKEN=top-secret",
	}
	out := sessionEnvironment(in, []string{"WORK_GH_BOT_TOKEN"})
	require.Contains(out, "PATH=/usr/bin")
	for _, kv := range out {
		require.NotContains(
			kv, "top-secret",
			"configured token env leaked: %q", kv,
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
		time.Sleep(time.Hour)
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
		time.Sleep(time.Hour)
	case "spawn-child":
		if len(helperArgs) < 2 {
			os.Exit(2)
		}
		child := exec.Command(
			os.Args[0], "-test.run=TestHelperProcess", "--", "sleep",
		)
		if err := child.Start(); err != nil {
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
		_, _ = fmt.Fprintf(
			f, "%d\n%d\n", os.Getpid(), child.Process.Pid,
		)
		_ = f.Close()
		time.Sleep(time.Hour)
	case "exit":
		os.Exit(3)
	default:
		os.Exit(2)
	}
}
