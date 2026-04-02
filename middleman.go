package middleman

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/web"
)

// Repo identifies a GitHub repository to monitor.
type Repo struct {
	Owner string
	Name  string
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
	Token        string
	DataDir      string
	BasePath     string
	SyncInterval time.Duration
	Repos        []Repo
	Activity     Activity
	Assets       fs.FS
	Embedded     bool
	AppName      string
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
// Token and DataDir are required.
func New(opts Options) (*Instance, error) {
	if opts.Token == "" {
		return nil, fmt.Errorf(
			"middleman: token is required",
		)
	}
	if opts.DataDir == "" {
		return nil, fmt.Errorf(
			"middleman: DataDir is required",
		)
	}

	cfg := buildConfig(opts)

	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf(
			"middleman: create data directory %s: %w",
			opts.DataDir, err,
		)
	}

	frontend, err := resolveAssets(opts)
	if err != nil {
		return nil, fmt.Errorf(
			"middleman: resolving assets: %w", err,
		)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf(
			"middleman: opening database: %w", err,
		)
	}

	gh := ghclient.NewClient(opts.Token)

	var refs []ghclient.RepoRef
	for _, r := range opts.Repos {
		refs = append(refs, ghclient.RepoRef{
			Owner: r.Owner,
			Name:  r.Name,
		})
	}

	syncer := ghclient.NewSyncer(
		gh, database, refs, cfg.SyncDuration(),
	)

	srv := server.New(
		database, gh, syncer, frontend,
		cfg.BasePath, cfg,
		server.ServerOptions{
			Embedded: opts.Embedded,
			AppName:  opts.AppName,
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
