package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

func setupWithBasePath(t *testing.T, basePath string, frontend fs.FS) *Server {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	mock := &mockGH{}
	syncer := ghclient.NewSyncer(mock, database, nil, time.Minute)
	return New(database, mock, syncer, frontend, basePath)
}

func TestBasePathAPIRouting(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	tests := []struct {
		name       string
		basePath   string
		reqPath    string
		wantStatus int
		wantJSON   bool
	}{
		{"root: API returns JSON", "/", "/api/v1/sync/status", 200, true},
		{"root: SPA returns HTML", "/", "/pulls", 200, false},
		{"prefix: API returns JSON", "/middleman/", "/middleman/api/v1/sync/status", 200, true},
		{"prefix: SPA returns HTML", "/middleman/", "/middleman/pulls", 200, false},
		{"prefix: bare API 404s", "/middleman/", "/api/v1/sync/status", 404, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := setupWithBasePath(t, tt.basePath, frontend)
			req := httptest.NewRequest(http.MethodGet, tt.reqPath, nil)
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("expected %d, got %d: %s", tt.wantStatus, rr.Code, rr.Body.String())
			}
			ct := rr.Header().Get("Content-Type")
			isJSON := strings.HasPrefix(ct, "application/json")
			if tt.wantJSON && !isJSON {
				t.Fatalf("expected JSON, got Content-Type %q: %s", ct, rr.Body.String())
			}
		})
	}
}

func TestBasePathInjectsScript(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head></head><body>app</body></html>`),
		},
	}

	srv := setupWithBasePath(t, "/middleman/", frontend)
	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, `window.__BASE_PATH__="/middleman/"`) {
		t.Fatalf("expected injected base path script, got: %s", body)
	}
}

func TestBasePathRewritesAssetURLs(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head><link href="/assets/index.css"></head><body><script src="/assets/index.js"></script></body></html>`),
		},
	}

	srv := setupWithBasePath(t, "/middleman/", frontend)
	req := httptest.NewRequest(http.MethodGet, "/middleman/", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, `href="/assets/`) {
		t.Fatalf("expected rewritten href, got: %s", body)
	}
	if !strings.Contains(body, `href="/middleman/assets/`) {
		t.Fatalf("expected /middleman/assets/ in href, got: %s", body)
	}
	if !strings.Contains(body, `src="/middleman/assets/`) {
		t.Fatalf("expected /middleman/assets/ in src, got: %s", body)
	}
}
