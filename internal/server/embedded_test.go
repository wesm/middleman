package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

func setupEmbeddedServer(
	t *testing.T,
	basePath string,
	frontend fs.FS,
	options ServerOptions,
) *Server {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, nil, nil, time.Minute, nil, "")
	return New(
		database,
		mock,
		syncer,
		frontend,
		basePath,
		nil,
		options,
	)
}

func TestBootstrapInjectsEmbedConfig(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	hideSync := true
	srv := setupEmbeddedServer(t, "/middleman/", frontend, ServerOptions{
		EmbedConfig: &EmbedConfig{
			Theme: &ThemeConfig{Mode: "dark"},
			UI:    &UIConfig{HideSync: &hideSync},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)
	assert.Contains(body, `window.__middleman_config=`)
	assert.Contains(body, `"mode":"dark"`)
	assert.NotContains(body, `__MIDDLEMAN_EMBEDDED__`)
}

func TestBootstrapNoEmbedConfig(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	srv := setupEmbeddedServer(t, "/app/", frontend, ServerOptions{})
	req := httptest.NewRequest(http.MethodGet, "/app/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)
	assert.NotContains(body, `__middleman_config`)
	assert.Contains(body, `window.__BASE_PATH__="/app/"`)
}
