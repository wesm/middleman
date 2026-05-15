package ptyowner

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOwnerAttachInputAndReplay(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	if runtime.GOOS == "windows" {
		t.Skip("in-process PTY owner test requires a host PTY")
	}

	root := t.TempDir()
	ctx := t.Context()

	done := make(chan error, 1)
	go func() {
		done <- RunOwner(ctx, Options{
			Root:    root,
			Session: "middleman-test",
			Cwd:     t.TempDir(),
			Command: []string{"sh", "-c", "printf ready; while IFS= read -r line; do echo got:$line; done"},
		})
	}()

	client := Client{Root: root}
	require.Eventually(func() bool {
		return client.Ping(context.Background(), "middleman-test") == nil
	}, 2*time.Second, 20*time.Millisecond)

	first, err := client.Attach(context.Background(), "middleman-test", 120, 30)
	require.NoError(err)
	defer first.Close()

	require.Contains(readUntil(t, first.Output, "ready"), "ready")
	require.NoError(first.Write([]byte("hello\n")))
	require.Contains(readUntil(t, first.Output, "got:hello"), "got:hello")
	first.Close()

	second, err := client.Attach(context.Background(), "middleman-test", 100, 20)
	require.NoError(err)
	defer second.Close()

	assert.Contains(readUntil(t, second.Output, "got:hello"), "got:hello")
	require.NoError(second.Resize(90, 25))
	require.NoError(client.Stop(context.Background(), "middleman-test"))

	select {
	case err := <-done:
		require.NoError(err)
	case <-time.After(2 * time.Second):
		require.Fail("owner did not stop")
	}
}

func TestOwnerStopWhileRunOwnerReturns(t *testing.T) {
	require := require.New(t)
	if runtime.GOOS == "windows" {
		t.Skip("in-process PTY owner test requires a host PTY")
	}

	root := t.TempDir()
	paths, err := NewSessionPaths(root, "middleman-stop-race")
	require.NoError(err)
	done := make(chan error, 1)
	go func() {
		done <- RunOwner(t.Context(), Options{
			Root:    root,
			Session: "middleman-stop-race",
			Cwd:     t.TempDir(),
			Command: []string{"sh", "-c", "while :; do sleep 0.05; done"},
		})
	}()

	client := Client{Root: root}
	require.Eventually(func() bool {
		return client.Ping(context.Background(), "middleman-stop-race") == nil
	}, 2*time.Second, 20*time.Millisecond)

	require.NoError(client.Stop(context.Background(), "middleman-stop-race"))
	_, err = os.Stat(paths.Dir)
	require.True(os.IsNotExist(err))
	select {
	case err := <-done:
		require.NoError(err)
	case <-time.After(2 * time.Second):
		require.Fail("owner did not stop")
	}
}

func TestOwnerRejectsOversizedUnauthenticatedRequest(t *testing.T) {
	require := require.New(t)
	if runtime.GOOS == "windows" {
		t.Skip("in-process PTY owner test requires a host PTY")
	}

	root := t.TempDir()
	session := "middleman-oversized-request"
	done := make(chan error, 1)
	go func() {
		done <- RunOwner(t.Context(), Options{
			Root:    root,
			Session: session,
			Cwd:     t.TempDir(),
			Command: []string{"sh", "-c", "while :; do sleep 0.05; done"},
		})
	}()

	client := Client{Root: root}
	require.Eventually(func() bool {
		return client.Ping(context.Background(), session) == nil
	}, 2*time.Second, 20*time.Millisecond)

	paths, err := NewSessionPaths(root, session)
	require.NoError(err)
	state, err := readState(paths)
	require.NoError(err)
	conn, err := net.Dial("tcp", state.Addr)
	require.NoError(err)
	_, err = conn.Write([]byte(
		`{"type":"status","token":"wrong","data":"` +
			strings.Repeat("A", maxOwnerFirstRequestSize) +
			`"}` + "\n",
	))
	require.NoError(err)
	require.NoError(conn.SetReadDeadline(time.Now().Add(time.Second)))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	require.Error(err)
	require.NoError(conn.Close())

	require.NoError(client.Ping(context.Background(), session))
	require.NoError(client.Stop(context.Background(), session))
	select {
	case err := <-done:
		require.NoError(err)
	case <-time.After(2 * time.Second):
		require.Fail("owner did not stop")
	}
}

func TestOwnerHelperEnvironmentStripsCredentials(t *testing.T) {
	out := ownerHelperEnvironment([]string{
		"PATH=/usr/bin",
		"MIDDLEMAN_GITHUB_TOKEN=secret-1",
		"GITHUB_TOKEN=secret-2",
		"GH_TOKEN_WORK=secret-3",
		"KEEP=value",
	})

	require.ElementsMatch(t, []string{
		"PATH=/usr/bin",
		"KEEP=value",
	}, out)
}

func TestClientStopTreatsStaleOwnerStateAsAbsent(t *testing.T) {
	require := require.New(t)

	root := t.TempDir()
	paths, err := NewSessionPaths(root, "middleman-stale")
	require.NoError(err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	addr := listener.Addr().String()
	require.NoError(listener.Close())
	require.NoError(writeState(paths, ownerState{
		Session: "middleman-stale",
		Addr:    addr,
		Token:   "token",
		Cwd:     t.TempDir(),
	}))

	err = (&Client{Root: root}).Stop(context.Background(), "middleman-stale")

	require.NoError(err)
	_, err = os.Stat(paths.Dir)
	require.True(os.IsNotExist(err))
}

func TestClientEnsurePreservesStateOnContextCancellation(t *testing.T) {
	require := require.New(t)

	root := t.TempDir()
	paths, err := NewSessionPaths(root, "middleman-canceled-ensure")
	require.NoError(err)
	require.NoError(writeState(paths, ownerState{
		Session: "middleman-canceled-ensure",
		Addr:    "127.0.0.1:1",
		Token:   "token",
		Cwd:     t.TempDir(),
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = (&Client{Root: root}).Ensure(ctx, "middleman-canceled-ensure", t.TempDir())

	require.Error(err)
	_, err = os.Stat(paths.Dir)
	require.NoError(err)
}

func TestClientEnsureSerializesConcurrentStarts(t *testing.T) {
	require := require.New(t)
	assert := Assert.New(t)
	if runtime.GOOS == "windows" {
		t.Skip("in-process PTY owner test requires a host PTY")
	}

	root := t.TempDir()
	dir := t.TempDir()
	record := filepath.Join(dir, "starts")
	stop := filepath.Join(dir, "stop")
	client := &Client{
		Root:      root,
		InProcess: true,
		Command: []string{
			"sh", "-c",
			"printf start >> \"$MIDDLEMAN_START_RECORD\"; " +
				"while [ ! -f \"$MIDDLEMAN_STOP_FILE\" ]; do sleep 0.05; done",
		},
	}
	t.Setenv("MIDDLEMAN_START_RECORD", record)
	t.Setenv("MIDDLEMAN_STOP_FILE", stop)
	t.Cleanup(func() {
		_ = os.WriteFile(stop, []byte("stop"), 0o644)
		_ = client.Stop(context.Background(), "middleman-concurrent-ensure")
	})

	const callers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			<-start
			errs <- client.Ensure(
				context.Background(),
				"middleman-concurrent-ensure",
				dir,
			)
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(err)
	}

	data, err := os.ReadFile(record)
	require.NoError(err)
	assert.Equal("start", string(data))
}

func TestClientStopPreservesStateOnContextCancellation(t *testing.T) {
	require := require.New(t)

	root := t.TempDir()
	paths, err := NewSessionPaths(root, "middleman-canceled-stop")
	require.NoError(err)
	require.NoError(writeState(paths, ownerState{
		Session: "middleman-canceled-stop",
		Addr:    "127.0.0.1:1",
		Token:   "token",
		Cwd:     t.TempDir(),
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = (&Client{Root: root}).Stop(ctx, "middleman-canceled-stop")

	require.Error(err)
	_, err = os.Stat(paths.Dir)
	require.NoError(err)
}

func TestClientPingHonorsContextAfterConnect(t *testing.T) {
	require := require.New(t)

	root := t.TempDir()
	paths, err := NewSessionPaths(root, "middleman-silent-owner")
	require.NoError(err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	require.NoError(writeState(paths, ownerState{
		Session: "middleman-silent-owner",
		Addr:    listener.Addr().String(),
		Token:   "token",
		Cwd:     t.TempDir(),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = (&Client{Root: root}).Ping(ctx, "middleman-silent-owner")

	require.Error(err)
	select {
	case conn := <-accepted:
		_ = conn.Close()
	default:
	}
}

func readUntil(t *testing.T, output <-chan []byte, needle string) string {
	t.Helper()

	deadline := time.After(2 * time.Second)
	var builder strings.Builder
	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				return builder.String()
			}
			builder.Write(chunk)
			if strings.Contains(builder.String(), needle) {
				return builder.String()
			}
		case <-deadline:
			require.New(t).Failf(
				"timed out waiting for output",
				"wanted %q in %q", needle, builder.String(),
			)
		}
	}
}
