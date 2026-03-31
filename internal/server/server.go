package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// Server holds the HTTP mux and its dependencies.
type Server struct {
	db       *db.DB
	gh       ghclient.Client
	syncer   *ghclient.Syncer
	basePath string
	mux      *http.ServeMux
}

// New creates a Server wiring up all API routes and optional SPA serving.
// basePath should be "/" or "/prefix/" (with trailing slash).
func New(database *db.DB, gh ghclient.Client, syncer *ghclient.Syncer, frontend fs.FS, basePath string) *Server {
	s := &Server{
		db:       database,
		basePath: basePath,
		gh:       gh,
		syncer:   syncer,
		mux:      http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /api/v1/activity", s.handleListActivity)
	s.mux.HandleFunc("GET /api/v1/pulls", s.handleListPulls)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{name}/pulls/{number}", s.handleGetPull)
	s.mux.HandleFunc("PUT /api/v1/repos/{owner}/{name}/pulls/{number}/state", s.handleSetKanbanState)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{name}/pulls/{number}/comments", s.handlePostComment)
	s.mux.HandleFunc("GET /api/v1/issues", s.handleListIssues)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{name}/issues/{number}", s.handleGetIssue)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{name}/issues/{number}/comments", s.handlePostIssueComment)

	s.mux.HandleFunc("PUT /api/v1/starred", s.handleSetStarred)
	s.mux.HandleFunc("DELETE /api/v1/starred", s.handleUnsetStarred)

	s.mux.HandleFunc("GET /api/v1/repos", s.handleListRepos)
	s.mux.HandleFunc("GET /api/v1/repos/{owner}/{name}", s.handleGetRepo)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{name}/pulls/{number}/approve", s.handleApprovePR)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{name}/pulls/{number}/ready-for-review", s.handleReadyForReview)
	s.mux.HandleFunc("POST /api/v1/repos/{owner}/{name}/pulls/{number}/merge", s.handleMergePR)
	s.mux.HandleFunc("POST /api/v1/sync", s.handleTriggerSync)
	s.mux.HandleFunc("GET /api/v1/sync/status", s.handleSyncStatus)

	if frontend != nil {
		// Read index.html once and inject the runtime base path.
		indexBytes, err := fs.ReadFile(frontend, "index.html")
		if err != nil {
			indexBytes = []byte("<!DOCTYPE html><html><body>frontend not found</body></html>")
		}
		idx := string(indexBytes)
		// Inject runtime base path for the SPA.
		idx = strings.Replace(idx, "<head>",
			`<head><script>window.__BASE_PATH__="`+basePath+`";</script>`, 1)
		// Rewrite asset URLs to include the base path prefix.
		if basePath != "/" {
			prefix := strings.TrimSuffix(basePath, "/")
			idx = strings.ReplaceAll(idx, `src="/assets/`, `src="`+prefix+`/assets/`)
			idx = strings.ReplaceAll(idx, `href="/assets/`, `href="`+prefix+`/assets/`)
		}
		indexHTML := idx

		fileServer := http.FileServerFS(frontend)
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			// Serve index.html with injected base path for SPA routes.
			if name == "index.html" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(indexHTML))
				return
			}
			// Try serving the exact file; fall back to index.html for SPA routing.
			f, err := frontend.Open(name)
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(indexHTML))
		})
	}

	return s
}

// ServeHTTP implements http.Handler so Server can be used directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.basePath != "/" {
		http.StripPrefix(strings.TrimSuffix(s.basePath, "/"), s.mux).ServeHTTP(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return srv.ListenAndServe()
}

// writeJSON encodes v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already written; nothing useful we can do.
		return
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
