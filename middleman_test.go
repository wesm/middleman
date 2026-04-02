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

	inst, err := New(Options{
		Token:    "test-token",
		DataDir:  t.TempDir(),
		BasePath: "/middleman/",
		Embedded: true,
		AppName:  "TestApp",
		Assets:   frontend,
	})
	require.NoError(t, err)
	defer inst.Close()

	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	inst.Handler().ServeHTTP(rr, req)

	body := rr.Body.String()
	assert := Assert.New(t)
	assert.Contains(body, `window.__MIDDLEMAN_EMBEDDED__=true`)
	assert.Contains(body, `window.__MIDDLEMAN_APP_NAME__="TestApp"`)
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
