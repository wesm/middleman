package middleman

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/web"
)

// Type aliases so external callers don't need to import
// the internal server package.
type (
	EmbedConfig = server.EmbedConfig
	ThemeConfig = server.ThemeConfig
	UIConfig    = server.UIConfig
	RepoRef     = server.RepoRef
)

// Repo identifies a GitHub repository to monitor.
type Repo struct {
	Owner        string
	Name         string
	PlatformHost string // e.g. "github.com" or GHE hostname
}

// defaultPlatformHost returns the platform host from the first
// repo that has one set, falling back to "github.com".
func defaultPlatformHost(repos []Repo) string {
	for _, r := range repos {
		if r.PlatformHost != "" {
			return r.PlatformHost
		}
	}
	return "github.com"
}

// Activity configures the activity view defaults.
type Activity struct {
	ViewMode   string
	TimeRange  string
	HideClosed bool
	HideBots   bool
}

// Options configures a middleman Instance for embedding.
type Options struct {
	// Token is a static GitHub token. Used when ResolveToken
	// is nil.
	Token string
	// ResolveToken returns a GitHub token for the given platform
	// host (e.g. "github.com"). Preferred over Token for
	// embedded use cases that need per-host auth.
	ResolveToken func(ctx context.Context, host string) (string, error)
	// DataDir is the directory for middleman state. Required if
	// DBPath is not set.
	DataDir string
	// DBPath overrides the DataDir-derived database path. When
	// set, the host owns the SQLite file and DataDir may be
	// omitted.
	DBPath       string
	BasePath     string
	SyncInterval time.Duration
	Repos        []Repo
	Activity     Activity
	Assets       fs.FS
	EmbedConfig  *server.EmbedConfig
}

// Instance holds a running middleman server and its resources.
type Instance struct {
	db         *db.DB
	server     *server.Server
	syncer     *ghclient.Syncer
	cancelSync context.CancelFunc
	stopMu     sync.Mutex
	closed     bool
}

// New creates a middleman Instance from the given options.
// Either Token or ResolveToken must yield a non-empty token.
// Either DBPath or DataDir must be provided.
func New(opts Options) (*Instance, error) {
	// DB path: prefer explicit DBPath over DataDir-derived.
	dbPath := opts.DBPath
	if dbPath == "" {
		if opts.DataDir == "" {
			return nil, fmt.Errorf(
				"middleman: either DBPath or DataDir " +
					"is required",
			)
		}
		if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
			return nil, fmt.Errorf(
				"middleman: create data directory %s: %w",
				opts.DataDir, err,
			)
		}
		dbPath = filepath.Join(opts.DataDir, "middleman.db")
	}

	// Token: prefer ResolveToken over static Token.
	token := opts.Token
	if opts.ResolveToken != nil {
		host := defaultPlatformHost(opts.Repos)
		resolved, err := opts.ResolveToken(
			context.Background(), host,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"middleman: resolve token: %w", err,
			)
		}
		token = resolved
	}
	if token == "" {
		return nil, fmt.Errorf(
			"middleman: token is required",
		)
	}

	cfg := buildConfig(opts)

	frontend, err := resolveAssets(opts)
	if err != nil {
		return nil, fmt.Errorf(
			"middleman: resolving assets: %w", err,
		)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf(
			"middleman: opening database: %w", err,
		)
	}

	host := defaultPlatformHost(opts.Repos)
	rt := ghclient.NewRateTracker(database, host)

	gh := ghclient.NewClient(token, rt)

	var refs []ghclient.RepoRef
	for _, r := range opts.Repos {
		refs = append(refs, ghclient.RepoRef{
			Owner: r.Owner,
			Name:  r.Name,
		})
	}

	syncer := ghclient.NewSyncer(
		gh, database, nil, refs, cfg.SyncDuration(), rt,
	)

	srv := server.New(
		database, gh, syncer, frontend,
		cfg.BasePath, cfg,
		server.ServerOptions{
			EmbedConfig: opts.EmbedConfig,
		},
	)

	return &Instance{
		db:     database,
		server: srv,
		syncer: syncer,
	}, nil
}

// Handler returns the HTTP handler for this instance.
func (i *Instance) Handler() http.Handler {
	return i.server
}

// StartSync begins periodic GitHub sync in the background.
// The context is used for cancellation during Close.
func (i *Instance) StartSync(ctx context.Context) {
	ctx, i.cancelSync = context.WithCancel(ctx)
	i.syncer.Start(ctx)
}

// StopSync stops the periodic GitHub sync.
func (i *Instance) StopSync() {
	i.syncer.Stop()
}

// Close stops sync and closes the database. Safe to call
// multiple times.
func (i *Instance) Close() error {
	i.stopMu.Lock()
	defer i.stopMu.Unlock()

	if i.closed {
		return nil
	}
	i.closed = true

	if i.cancelSync != nil {
		i.cancelSync()
	}
	i.StopSync()
	return i.db.Close()
}

func buildConfig(opts Options) *config.Config {
	interval := opts.SyncInterval
	if interval == 0 {
		interval = 5 * time.Minute
	}

	basePath := opts.BasePath
	if basePath == "" {
		basePath = "/"
	} else {
		basePath = "/" + strings.Trim(basePath, "/")
		if basePath != "/" {
			basePath += "/"
		}
	}

	viewMode := opts.Activity.ViewMode
	if viewMode == "" {
		viewMode = "threaded"
	}

	timeRange := opts.Activity.TimeRange
	if timeRange == "" {
		timeRange = "7d"
	}

	var repos []config.Repo
	for _, r := range opts.Repos {
		repos = append(repos, config.Repo{
			Owner: r.Owner,
			Name:  r.Name,
		})
	}
	if repos == nil {
		repos = []config.Repo{}
	}

	return &config.Config{
		SyncInterval:   interval.String(),
		GitHubTokenEnv: "UNUSED",
		Host:           "127.0.0.1",
		Port:           8090,
		BasePath:       basePath,
		DataDir:        opts.DataDir,
		Repos:          repos,
		Activity: config.Activity{
			ViewMode:   viewMode,
			TimeRange:  timeRange,
			HideClosed: opts.Activity.HideClosed,
			HideBots:   opts.Activity.HideBots,
		},
	}
}

func resolveAssets(opts Options) (fs.FS, error) {
	if opts.Assets != nil {
		return opts.Assets, nil
	}
	return web.Assets()
}
