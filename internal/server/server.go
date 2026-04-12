package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
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
	hub               *EventHub
	activeWorktreeMu  sync.Mutex
	activeWorktreeKey string
	activeWorktreeSet bool

	// bg tracks short-lived goroutines that HTTP handlers spawn
	// outside of the Syncer's own wait group (e.g. mergePR's
	// post-failure refresh). Shutdown waits on bg before the
	// caller tears down the DB.
	//
	// bgMu guards shuttingDown, drainDone, and httpSrv, and
	// serializes bg.Add against Shutdown's bg.Wait so the
	// WaitGroup cannot observe Add racing with Wait when the
	// counter transiently hits zero.
	bgMu         sync.Mutex
	bg           sync.WaitGroup
	bgCtx        context.Context
	bgCancel     context.CancelFunc
	shuttingDown bool
	// drainDone is created the first time Shutdown is called and
	// closed when bg.Wait returns. Every caller waits on it
	// subject to its own ctx, so a retry with a longer deadline
	// observes true drain after an earlier caller's ctx expired.
	drainDone chan struct{}
	httpSrv   *http.Server
}

// Hub returns the server's SSE event hub. Callers should never
// retain the returned pointer beyond the server's lifetime.
func (s *Server) Hub() *EventHub { return s.hub }

// SetVersion sets the version string returned by GET /api/v1/version.
func (s *Server) SetVersion(v string) { s.version = v }

// runBackground launches fn as a tracked goroutine. fn receives a
// context cancelled by Shutdown. If Shutdown has already started,
// runBackground drops the task: these goroutines are best-effort
// refreshes and starting one during drain would race with bg.Wait.
func (s *Server) runBackground(fn func(ctx context.Context)) {
	s.bgMu.Lock()
	if s.shuttingDown {
		s.bgMu.Unlock()
		return
	}
	s.bg.Add(1)
	s.bgMu.Unlock()
	go func() {
		defer s.bg.Done()
		fn(s.bgCtx)
	}()
}

// Shutdown stops the HTTP listener (if started via ListenAndServe
// or Serve), closes the SSE event hub so streaming handlers exit,
// cancels background goroutines' context, and blocks until they
// finish or ctx expires. Safe to call concurrently and repeatedly.
// Every caller drives http.Server.Shutdown with its own ctx
// (stdlib polls idle-conn closure per call) and waits on a shared
// drain channel, so a retry with a longer deadline observes true
// drain for both HTTP handlers and the bg group. Only the first
// caller closes the hub and cancels bgCtx.
func (s *Server) Shutdown(ctx context.Context) error {
	s.bgMu.Lock()
	first := !s.shuttingDown
	if first {
		s.shuttingDown = true
		s.drainDone = make(chan struct{})
	}
	drainDone := s.drainDone
	httpSrv := s.httpSrv
	s.bgMu.Unlock()

	// Close the hub first so handleSSE subscribers can exit on
	// their <-done select arm. Otherwise http.Server.Shutdown
	// below would wait on SSE handlers that never return until
	// client disconnect, hanging the shutdown until ctx expires.
	if first && s.hub != nil {
		s.hub.Close()
	}

	var httpErr error
	if httpSrv != nil {
		httpErr = httpSrv.Shutdown(ctx)
	}

	if first {
		s.bgCancel()
		go func() {
			s.bg.Wait()
			close(drainDone)
		}()
	}

	select {
	case <-drainDone:
		return httpErr
	case <-ctx.Done():
		if httpErr != nil {
			return errors.Join(httpErr, ctx.Err())
		}
		return ctx.Err()
	}
}

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

	bgCtx, bgCancel := context.WithCancel(context.Background())
	s := &Server{
		db:       database,
		basePath: basePath,
		syncer:   syncer,
		clones:   clones,
		cfg:      cfg,
		cfgPath:  cfgPath,
		options:  options,
		hub:      NewEventHub(),
		bgCtx:    bgCtx,
		bgCancel: bgCancel,
	}

	api := humago.NewWithPrefix(mux, "/api/v1", apiConfig(basePath))
	s.registerAPI(api)

	mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.handleUpdateSettings)
	mux.HandleFunc("POST /api/v1/repos", s.handleAddRepo)
	mux.HandleFunc("DELETE /api/v1/repos/{owner}/{name}", s.handleDeleteRepo)
	mux.HandleFunc("GET /api/v1/events", s.handleSSE)

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

// ListenAndServe starts the HTTP server on addr. Returns
// http.ErrServerClosed when stopped by Shutdown (matches net/http).
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts HTTP connections on the provided listener. Useful
// for tests and any caller that wants to own the listener lifetime.
// Returns http.ErrServerClosed when stopped by Shutdown.
func (s *Server) Serve(ln net.Listener) error {
	srv := &http.Server{
		Handler:     s,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout is 0 (disabled) because the roborev
		// proxy streams SSE/NDJSON responses that are
		// long-lived by design. A non-zero value would kill
		// /api/roborev/api/stream/events and /api/job/log
		// after the deadline.
		IdleTimeout: 60 * time.Second,
	}

	s.bgMu.Lock()
	if s.shuttingDown {
		s.bgMu.Unlock()
		_ = ln.Close()
		return http.ErrServerClosed
	}
	s.httpSrv = srv
	s.bgMu.Unlock()

	return srv.Serve(ln)
}

// handleSSE streams server events to a client. The handler subscribes
// to the EventHub and forwards each broadcast as an SSE frame. It exits
// when the client disconnects, when the hub closes, when the subscriber
// is evicted (slow consumer), or when context is canceled.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	rc := http.NewResponseController(w)
	// Clear server-wide WriteTimeout for this SSE response
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		return
	}

	// Subscribe BEFORE the first flush so any broadcast issued between
	// the headers landing on the wire and the subscriber being registered
	// is delivered to this client instead of dropped.
	ch, done := s.hub.Subscribe(r.Context())

	if err := rc.Flush(); err != nil {
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		// Non-blocking done check
		select {
		case <-done:
			return
		default:
		}

		select {
		case <-done:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				slog.Error("sse: marshal event", "type", ev.Type, "err", err)
				continue
			}
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				return
			}
		case <-ticker.C:
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				return
			}
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
			if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
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
