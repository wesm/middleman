package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// Server holds the HTTP mux and its dependencies.
type Server struct {
	db       *db.DB
	gh       ghclient.Client
	syncer   *ghclient.Syncer
	basePath string
	handler  http.Handler
}

// New creates a Server wiring up all API routes and optional SPA serving.
// basePath should be "/" or "/prefix/" (with trailing slash).
func New(database *db.DB, gh ghclient.Client, syncer *ghclient.Syncer, frontend fs.FS, basePath string) *Server {
	mux := http.NewServeMux()

	s := &Server{
		db:       database,
		basePath: basePath,
		gh:       gh,
		syncer:   syncer,
	}

	api := humago.NewWithPrefix(mux, "/api/v1", apiConfig())
	s.registerAPI(api)

	if frontend != nil {
		indexBytes, err := fs.ReadFile(frontend, "index.html")
		if err != nil {
			indexBytes = []byte("<!DOCTYPE html><html><body>frontend not found</body></html>")
		}
		idx := string(indexBytes)
		safeBase, _ := json.Marshal(basePath)
		idx = strings.Replace(idx, "<head>",
			`<head><script>window.__BASE_PATH__=`+string(safeBase)+`;</script>`, 1)
		if basePath != "/" {
			prefix := strings.TrimSuffix(basePath, "/")
			idx = strings.ReplaceAll(idx, `src="/assets/`, `src="`+prefix+`/assets/`)
			idx = strings.ReplaceAll(idx, `href="/assets/`, `href="`+prefix+`/assets/`)
		}
		indexHTML := []byte(idx)

		serveIndex := func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(indexHTML)
		}

		fileServer := http.FileServerFS(frontend)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			name := strings.TrimPrefix(r.URL.Path, "/")
			if name == "" || name == "index.html" {
				serveIndex(w)
				return
			}
			f, err := frontend.Open(name)
			if err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
			serveIndex(w)
		})
	}

	// When serving under a base path, use an outer mux with
	// StripPrefix so the inner mux sees clean paths like /api/v1/...
	if basePath != "/" {
		outer := http.NewServeMux()
		prefix := strings.TrimSuffix(basePath, "/")
		outer.Handle(basePath, http.StripPrefix(prefix, mux))
		s.handler = outer
	} else {
		s.handler = mux
	}

	return s
}

// ServeHTTP implements http.Handler so Server can be used directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s,
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
		return
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
