package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
)

// Server holds the HTTP mux and its dependencies.
type Server struct {
	db     *db.DB
	gh     ghclient.Client
	syncer *ghclient.Syncer
	mux    *http.ServeMux
}

// New creates a Server wiring up all API routes and optional SPA serving.
func New(database *db.DB, gh ghclient.Client, syncer *ghclient.Syncer, frontend fs.FS) *Server {
	s := &Server{
		db:     database,
		gh:     gh,
		syncer: syncer,
		mux:    http.NewServeMux(),
	}

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
	s.mux.HandleFunc("POST /api/v1/sync", s.handleTriggerSync)
	s.mux.HandleFunc("GET /api/v1/sync/status", s.handleSyncStatus)

	if frontend != nil {
		fileServer := http.FileServerFS(frontend)
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Strip leading slash: fs.FS paths must not start with '/'.
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" {
				name = "index.html"
			}
			// Try serving the exact file; fall back to index.html for SPA routing.
			f, err := frontend.Open(name)
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			// File not found — serve index.html so the SPA router handles the path.
			http.ServeFileFS(w, r, frontend, "index.html")
		})
	}

	return s
}

// ServeHTTP implements http.Handler so Server can be used directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
