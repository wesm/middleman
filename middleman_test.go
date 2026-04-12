package middleman

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReturnsWorkingHandler(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	inst, err := New(Options{
		Token:        "test-token",
		DataDir:      t.TempDir(),
		BasePath:     "/middleman/",
		SyncInterval: time.Minute,
		Assets:       frontend,
	})
	require.NoError(t, err)
	defer inst.Close()

	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	Assert.Contains(t, rr.Body.String(), `window.__BASE_PATH__="/middleman/"`)
}

func TestNewEmbeddedBootstrapGlobals(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	hideSync := true
	inst, err := New(Options{
		Token:    "test-token",
		DataDir:  t.TempDir(),
		BasePath: "/middleman/",
		EmbedConfig: &EmbedConfig{
			Theme: &ThemeConfig{Mode: "dark"},
			UI:    &UIConfig{HideSync: &hideSync},
		},
		Assets: frontend,
	})
	require.NoError(t, err)
	defer inst.Close()

	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)
	assert.Contains(body, `window.__middleman_config=`)
	assert.NotContains(body, `__MIDDLEMAN_EMBEDDED__`)
}

func TestEmbedConfigFullFlow(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(
				`<!DOCTYPE html><html><head></head>` +
					`<body>app</body></html>`,
			),
		},
	}

	hideStar := true
	sidebarCollapsed := false
	inst, err := New(Options{
		Token:    "test-token",
		DataDir:  t.TempDir(),
		BasePath: "/app/",
		EmbedConfig: &EmbedConfig{
			Theme: &ThemeConfig{
				Mode:   "dark",
				Colors: map[string]string{"bgPrimary": "#1a1a2e"},
				Fonts:  map[string]string{"sans": "SF Pro"},
			},
			UI: &UIConfig{
				HideStar:         &hideStar,
				SidebarCollapsed: &sidebarCollapsed,
				Repo: &RepoRef{
					Owner: "apache", Name: "arrow",
				},
			},
		},
		Assets: frontend,
	})
	require.NoError(t, err)
	defer inst.Close()

	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rr := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)

	assert.Contains(body, `window.__middleman_config=`)
	assert.Contains(body, `"mode":"dark"`)
	assert.Contains(body, `"bgPrimary":"#1a1a2e"`)
	assert.Contains(body, `"sans":"SF Pro"`)
	assert.Contains(body, `"hideStar":true`)
	assert.Contains(body, `"sidebarCollapsed":false`)
	assert.Contains(body, `"owner":"apache"`)
	assert.NotContains(body, `__MIDDLEMAN_EMBEDDED__`)
	assert.NotContains(body, `__MIDDLEMAN_APP_NAME__`)
	assert.Contains(body, `window.__BASE_PATH__="/app/"`)
}

func TestNoEmbedConfigStandaloneMode(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(
				`<!DOCTYPE html><html><head></head>` +
					`<body>app</body></html>`,
			),
		},
	}

	inst, err := New(Options{
		Token:   "test-token",
		DataDir: t.TempDir(),
		Assets:  frontend,
	})
	require.NoError(t, err)
	defer inst.Close()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)
	assert.Contains(body, `window.__BASE_PATH__="/"`)
	assert.NotContains(body, `__middleman_config`)
}

func TestNewRequiresToken(t *testing.T) {
	_, err := New(Options{
		DataDir: t.TempDir(),
	})
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "token")
}

func TestNewRequiresDataDir(t *testing.T) {
	_, err := New(Options{
		Token: "test-token",
	})
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "DataDir")
}

func TestNewWithDBPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared.db")
	inst, err := New(Options{
		Token:  "test-token",
		DBPath: dbPath,
		Repos:  []Repo{{Owner: "t", Name: "r"}},
	})
	require.NoError(t, err)
	defer inst.Close()

	_, err = os.Stat(dbPath)
	require.NoError(t, err)
}

func TestNewWithResolveToken(t *testing.T) {
	dir := t.TempDir()
	called := false
	inst, err := New(Options{
		ResolveToken: func(
			_ context.Context, host string,
		) (string, error) {
			called = true
			require.Equal(t, "github.com", host)
			return "resolved-token", nil
		},
		DataDir: dir,
		Repos:   []Repo{{Owner: "t", Name: "r"}},
	})
	require.NoError(t, err)
	defer inst.Close()
	require.True(t, called)
}

func TestNewResolveTokenError(t *testing.T) {
	_, err := New(Options{
		ResolveToken: func(
			_ context.Context, _ string,
		) (string, error) {
			return "", fmt.Errorf("auth failed")
		},
		DataDir: t.TempDir(),
	})
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "auth failed")
}

func TestNewResolveTokenOverridesStatic(t *testing.T) {
	dir := t.TempDir()
	inst, err := New(Options{
		Token: "static-token",
		ResolveToken: func(
			_ context.Context, _ string,
		) (string, error) {
			return "dynamic-token", nil
		},
		DataDir: dir,
		Repos:   []Repo{{Owner: "t", Name: "r"}},
	})
	require.NoError(t, err)
	defer inst.Close()
}

func TestNewDBPathSkipsDataDirCreation(t *testing.T) {
	// When DBPath is set, DataDir should not be required
	// and no directory creation should be attempted.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	inst, err := New(Options{
		Token:  "test-token",
		DBPath: dbPath,
	})
	require.NoError(t, err)
	defer inst.Close()
}

func TestNewMultiHostRequiresResolveToken(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Options{
		Token:   "tok",
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b", PlatformHost: "github.com"},
			{Owner: "c", Name: "d", PlatformHost: "ghe.corp.com"},
		},
	})
	require.Error(t, err)
	Assert.Contains(t, err.Error(), "ResolveToken")
}

func TestNewMultiHostWithResolveToken(t *testing.T) {
	dir := t.TempDir()
	inst, err := New(Options{
		ResolveToken: func(
			_ context.Context, host string,
		) (string, error) {
			return "token-for-" + host, nil
		},
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b", PlatformHost: "github.com"},
			{Owner: "c", Name: "d", PlatformHost: "ghe.corp.com"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inst)
	inst.Close()
}

func TestNewSingleHostWithTokenStillWorks(t *testing.T) {
	dir := t.TempDir()
	inst, err := New(Options{
		Token:   "tok",
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b"},
			{Owner: "c", Name: "d"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inst)
	inst.Close()
}

func TestCloseIsIdempotent(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	inst, err := New(Options{
		Token:   "test-token",
		DataDir: t.TempDir(),
		Assets:  frontend,
	})
	require.NoError(t, err)

	require.NoError(t, inst.Close())
	require.NoError(t, inst.Close()) // must not panic or error
}

// TestStopSyncCancelsStackHook verifies the instance-lifetime stacks
// hook context gets canceled during StopSync. After New() installs the
// hook, we flip to stopped state via StopSync and confirm a subsequent
// Close() works cleanly (no deadlock from a dangling hook goroutine).
// This covers the roborev finding that StopSync must cancel in-flight
// stack-detection work, not only Close.
func TestStopSyncCancelsStackHook(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	inst, err := New(Options{
		Token:   "test-token",
		DataDir: t.TempDir(),
		Assets:  frontend,
	})
	require.NoError(t, err)

	// Never called StartSync — hook must still exist (installed in New)
	// and StopSync must cancel its context without blocking.
	done := make(chan struct{})
	go func() {
		inst.StopSync()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "StopSync did not return within 5s — hook ctx not canceled")
	}

	// Close must still succeed after StopSync.
	require.NoError(t, inst.Close())
}

// TestStartSyncAfterStopSyncIsTerminal pins the documented contract of
// StopSync: once the Syncer is stopped, a subsequent StartSync is a
// silent no-op — it must not resurrect the hook, must not start a new
// background sync goroutine, and must not panic. A previous attempt at
// fixing a hook-lifecycle bug reinstalled the hook on StartSync, which
// only looked like a restart: the underlying Syncer.Start already
// no-ops once stopped, so the reinstalled hook never fired. This test
// prevents that half-working restart path from coming back.
func TestStartSyncAfterStopSyncIsTerminal(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	req := require.New(t)
	inst, err := New(Options{
		Token:   "test-token",
		DataDir: t.TempDir(),
		Assets:  frontend,
	})
	req.NoError(err)
	t.Cleanup(func() { _ = inst.Close() })

	req.NotNil(inst.cancelHook, "hook must be installed by New")
	inst.StopSync()
	req.Nil(inst.cancelHook, "StopSync must cancel and clear the hook")

	// StartSync after StopSync must not reinstall the hook and must
	// not panic. The Syncer underneath is already permanently stopped
	// (Start/TriggerRun both no-op once stopped=true).
	inst.StartSync(t.Context())
	req.Nil(inst.cancelHook, "StartSync must not resurrect the hook after StopSync")

	// Close remains idempotent and safe after a StopSync/StartSync
	// sequence.
	req.NoError(inst.Close())
	req.NoError(inst.Close())
}
