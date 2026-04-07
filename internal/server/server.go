package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	ghclient "github.com/wesm/middleman/internal/github"
)

type EmbedConfig struct {
	Theme *ThemeConfig `json:"theme,omitempty"`
	UI    *UIConfig    `json:"ui,omitempty"`
}

type ThemeConfig struct {
	Mode   string            `json:"mode,omitempty"`
	Colors map[string]string `json:"colors,omitempty"`
	Fonts  map[string]string `json:"fonts,omitempty"`
	Radii  map[string]string `json:"radii,omitempty"`
}

type UIConfig struct {
	HideSync          *bool    `json:"hideSync,omitempty"`
	HideRepoSelector  *bool    `json:"hideRepoSelector,omitempty"`
	HideStar          *bool    `json:"hideStar,omitempty"`
	SidebarCollapsed  *bool    `json:"sidebarCollapsed,omitempty"`
	Repo              *RepoRef `json:"repo,omitempty"`
	ActiveWorktreeKey string   `json:"activeWorktreeKey,omitempty"`
}

type RepoRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type ServerOptions struct {
	EmbedConfig *EmbedConfig
	Clones      *gitclone.Manager // optional clone manager for diff view
}

// Server holds the HTTP mux and its dependencies.
type Server struct {
	db                *db.DB
	syncer            *ghclient.Syncer
	clones            *gitclone.Manager
	cfg               *config.Config
	cfgPath           string
	cfgMu             sync.Mutex
	basePath          string
	options           ServerOptions
	version           string
	handler           http.Handler
	activeWorktreeMu  sync.Mutex
	activeWorktreeKey string
	activeWorktreeSet bool
}

// SetVersion sets the version string returned by GET /api/v1/version.
func (s *Server) SetVersion(v string) { s.version = v }

// SetActiveWorktreeKey sets the key of the currently
// focused worktree. Thread-safe.
func (s *Server) SetActiveWorktreeKey(key string) {
	s.activeWorktreeMu.Lock()
	s.activeWorktreeKey = key
	s.activeWorktreeSet = true
	s.activeWorktreeMu.Unlock()
}

// ActiveWorktreeKey returns the key of the currently
// focused worktree and whether it was explicitly set.
// Thread-safe.
func (s *Server) ActiveWorktreeKey() (string, bool) {
	s.activeWorktreeMu.Lock()
	defer s.activeWorktreeMu.Unlock()
	return s.activeWorktreeKey, s.activeWorktreeSet
}

// New creates a Server without config persistence.
// Pass cfg for repo filtering (can be nil for tests that
// don't need filtering).
func New(
	database *db.DB,
	syncer *ghclient.Syncer,
	frontend fs.FS,
	basePath string,
	cfg *config.Config,
	opts ServerOptions,
) *Server {
	return newServer(
		database, syncer, opts.Clones, frontend,
		basePath, cfg, "", opts,
	)
}

// NewWithConfig creates a Server with config persistence for
// settings/repo endpoints.
func NewWithConfig(
	database *db.DB,
	syncer *ghclient.Syncer,
	clones *gitclone.Manager,
	frontend fs.FS,
	cfg *config.Config,
	cfgPath string,
	opts ServerOptions,
) *Server {
	return newServer(
		database, syncer, clones, frontend,
		cfg.BasePath, cfg, cfgPath, opts,
	)
}

func newServer(
	database *db.DB,
	syncer *ghclient.Syncer,
	clones *gitclone.Manager,
	frontend fs.FS,
	basePath string,
	cfg *config.Config,
	cfgPath string,
	options ServerOptions,
) *Server {
	mux := http.NewServeMux()

	s := &Server{
		db:       database,
		basePath: basePath,
		syncer:   syncer,
		clones:   clones,
		cfg:      cfg,
		cfgPath:  cfgPath,
		options:  options,
	}

	api := humago.NewWithPrefix(mux, "/api/v1", apiConfig(basePath))
	s.registerAPI(api)

	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/v1/repos", s.handleAddRepo)
	mux.HandleFunc("DELETE /api/v1/repos/{owner}/{name}", s.handleDeleteRepo)

	// Roborev proxy
	if cfg != nil {
		roborevTarget := cfg.RoborevEndpoint()
		mux.Handle("/api/roborev/", roborevProxy(roborevTarget))
		mux.HandleFunc(
			"GET /api/v1/roborev/status",
			handleRoborevStatus(cfg),
		)
	}

	if frontend != nil {
		indexBytes, err := fs.ReadFile(frontend, "index.html")
		if err != nil {
			indexBytes = []byte("<!DOCTYPE html><html><body>frontend not found</body></html>")
		}
		indexTemplate := string(indexBytes)
		if basePath != "/" {
			prefix := strings.TrimSuffix(basePath, "/")
			indexTemplate = strings.ReplaceAll(indexTemplate, `src="/assets/`, `src="`+prefix+`/assets/`)
			indexTemplate = strings.ReplaceAll(indexTemplate, `href="/assets/`, `href="`+prefix+`/assets/`)
		}

		serveIndex := func(w http.ResponseWriter) {
			idx := strings.Replace(indexTemplate, "<head>",
				`<head><script>`+s.bootstrapScript()+`</script>`, 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(idx))
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

func (s *Server) bootstrapScript() string {
	safeBase, _ := json.Marshal(s.basePath)
	var builder strings.Builder
	builder.WriteString(`window.__BASE_PATH__=`)
	builder.WriteString(scriptSafe(string(safeBase)))
	builder.WriteString(`;`)
	cfg := s.options.EmbedConfig
	if awKey, set := s.ActiveWorktreeKey(); set {
		if cfg == nil {
			cfg = &EmbedConfig{}
		} else {
			cfgCopy := *cfg
			cfg = &cfgCopy
		}
		if cfg.UI == nil {
			cfg.UI = &UIConfig{}
		} else {
			uiCopy := *cfg.UI
			cfg.UI = &uiCopy
		}
		cfg.UI.ActiveWorktreeKey = awKey
	}
	if cfg != nil {
		configJSON, _ := json.Marshal(cfg)
		builder.WriteString(`window.__middleman_config=`)
		builder.WriteString(scriptSafe(string(configJSON)))
		builder.WriteString(`;`)
	}
	return builder.String()
}

// scriptSafe escapes sequences that could break out of an inline
// <script> block. Replaces "</" with "<\/" so that payloads
// containing "</script>" cannot close the tag early.
func scriptSafe(s string) string {
	return strings.ReplaceAll(s, "</", `<\/`)
}

// ServeHTTP implements http.Handler so Server can be used directly.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && s.isMutatingAPIRequest(r) {
		if !checkCSRF(w, r) {
			return
		}
	}
	s.handler.ServeHTTP(w, r)
}

// isMutatingAPIRequest checks whether the request targets an API route,
// accounting for the configured basePath prefix.
func (s *Server) isMutatingAPIRequest(r *http.Request) bool {
	path := r.URL.Path
	if s.basePath != "/" {
		prefix := strings.TrimSuffix(s.basePath, "/")
		path = strings.TrimPrefix(path, prefix)
	}
	return strings.HasPrefix(path, "/api/")
}

// checkCSRF rejects cross-site mutation requests. Returns true if
// the request is allowed, false if it was rejected (response written).
func checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		if sfs != "same-origin" && sfs != "none" {
			writeError(w, http.StatusForbidden,
				"cross-origin requests are not allowed")
			return false
		}
	}

	// Require Content-Type: application/json on all mutation requests,
	// including zero-body endpoints like POST /sync. This prevents
	// cross-origin form submissions and simple fetches from forging
	// requests even without Sec-Fetch-Site.
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType,
			"Content-Type must be application/json")
		return false
	}

	return true
}

// ListenAndServe starts the HTTP server on addr.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:        addr,
		Handler:     s,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout is 0 (disabled) because the roborev
		// proxy streams SSE/NDJSON responses that are
		// long-lived by design. A non-zero value would kill
		// /api/roborev/api/stream/events and /api/job/log
		// after the deadline.
		IdleTimeout: 60 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) handleVersion(
	w http.ResponseWriter, _ *http.Request,
) {
	writeJSON(w, http.StatusOK, map[string]string{
		"version": s.version,
	})
}

// writeJSON encodes v as JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
