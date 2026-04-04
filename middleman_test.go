package middleman

import (
	"net/http"
	"net/http/httptest"
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
