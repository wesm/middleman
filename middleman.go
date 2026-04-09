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
	"github.com/wesm/middleman/internal/gitclone"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/web"
)

// Type aliases so external callers don't need to import
// internal packages.
type (
	EmbedConfig  = server.EmbedConfig
	ThemeConfig  = server.ThemeConfig
	UIConfig     = server.UIConfig
	RepoRef      = server.RepoRef
	WorktreeLink = db.WorktreeLink
	WatchedMR    = ghclient.WatchedMR
)

// Repo identifies a GitHub repository to monitor.
type Repo struct {
	Owner        string
	Name         string
	PlatformHost string // e.g. "github.com" or GHE hostname
}

// uniqueHosts returns the deduplicated list of platform hosts
// from the given repos. Repos without PlatformHost default to
// "github.com". If repos is empty, returns ["github.com"].
func uniqueHosts(repos []Repo) []string {
	seen := make(map[string]bool)
	var hosts []string
	for _, r := range repos {
		h := r.PlatformHost
		if h == "" {
			h = "github.com"
		}
		if !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return []string{"github.com"}
	}
	return hosts
}

// resolveHostTokens builds a map of host -> token. When
// resolveToken is set it is called once per host; otherwise
// staticToken is used for all hosts.
func resolveHostTokens(
	hosts []string,
	staticToken string,
	resolveToken func(ctx context.Context, host string) (string, error),
) (map[string]string, error) {
	tokens := make(map[string]string, len(hosts))
	for _, host := range hosts {
		if resolveToken != nil {
			tok, err := resolveToken(
				context.Background(), host,
			)
			if err != nil {
				return nil, fmt.Errorf(
					"middleman: resolve token for %s: %w",
					host, err,
				)
			}
			tokens[host] = tok
		} else {
			tokens[host] = staticToken
		}
		if tokens[host] == "" {
			return nil, fmt.Errorf(
				"middleman: token is required (empty for host %s)",
				host,
			)
		}
	}
	return tokens, nil
}

// EmbedHooks provides lifecycle callbacks for embedded consumers.
//
// Concurrency:
//   - OnMRSynced fires after each merge request is synced. Sync
//     processes repos in parallel, so this callback may be
//     invoked from multiple goroutines concurrently (one per
//     in-flight repo sync). Implementations must be safe for
//     concurrent use and must not block indefinitely or they
//     will stall sync progress.
//   - OnSyncCompleted fires once at the end of each sync pass on
//     the goroutine that drives the sync, so it is not invoked
//     concurrently with itself.
//
// Hooks should be set before calling StartSync. Mutating the
// hook fields while a sync is in flight is not safe.
type EmbedHooks struct {
	OnMRSynced      func(MergeRequestSummary)
	OnSyncCompleted func(results []RepoSyncResult)
}

// MergeRequestSummary is a lightweight snapshot of a synced MR,
// passed to EmbedHooks callbacks.
type MergeRequestSummary struct {
	MergeRequestID int64
	RepoOwner      string
	RepoName       string
	Number         int
	State          string
	Title          string
	IsDraft        bool
	CIStatus       string
	ReviewDecision string
	PlatformHost   string
	CIChecksJSON   string
	UpdatedAt      time.Time
}

// RepoSyncResult holds the outcome of syncing a single repo.
type RepoSyncResult struct {
	Owner        string
	Name         string
	PlatformHost string
	Error        string // empty on success
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
	DBPath        string
	BasePath      string
	SyncInterval  time.Duration
	WatchInterval time.Duration
	Repos         []Repo
	Activity      Activity
	Assets        fs.FS
	EmbedConfig   *server.EmbedConfig
	EmbedHooks    *EmbedHooks
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

	// Collect unique hosts from repos.
	hosts := uniqueHosts(opts.Repos)

	// Multi-host requires ResolveToken.
	if len(hosts) > 1 && opts.ResolveToken == nil {
		return nil, fmt.Errorf(
			"middleman: multi-host config requires " +
				"ResolveToken (repos span multiple hosts)",
		)
	}

	// Build per-host tokens.
	hostTokens, err := resolveHostTokens(
		hosts, opts.Token, opts.ResolveToken,
	)
	if err != nil {
		return nil, err
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

	// Build per-host budgets, clients, and rate trackers.
	budgetPerHour := cfg.BudgetPerHour()
	budgets := make(
		map[string]*ghclient.SyncBudget, len(hosts),
	)
	clients := make(map[string]ghclient.Client, len(hosts))
	rateTrackers := make(
		map[string]*ghclient.RateTracker, len(hosts),
	)
	for _, host := range hosts {
		rt := ghclient.NewRateTracker(database, host)
		rateTrackers[host] = rt
		if budgetPerHour > 0 {
			budgets[host] = ghclient.NewSyncBudget(
				budgetPerHour,
			)
		}
		gh, cErr := ghclient.NewClient(
			hostTokens[host], host, rt, budgets[host],
		)
		if cErr != nil {
			database.Close()
			return nil, fmt.Errorf(
				"middleman: creating GitHub client for %s: %w",
				host, cErr,
			)
		}
		clients[host] = gh
	}

	// Build repo refs with resolved PlatformHost.
	var refs []ghclient.RepoRef
	for _, r := range opts.Repos {
		h := r.PlatformHost
		if h == "" {
			h = "github.com"
		}
		refs = append(refs, ghclient.RepoRef{
			Owner:        r.Owner,
			Name:         r.Name,
			PlatformHost: h,
		})
	}

	// Clone manager (needs DataDir for clone storage).
	var cloneMgr *gitclone.Manager
	if opts.DataDir != "" {
		cloneDir := filepath.Join(opts.DataDir, "clones")
		cloneMgr = gitclone.New(cloneDir, hostTokens)
	}

	syncer := ghclient.NewSyncer(
		clients, database, cloneMgr, refs,
		cfg.SyncDuration(), rateTrackers, budgets,
	)

	if opts.WatchInterval > 0 {
		syncer.SetWatchInterval(opts.WatchInterval)
	}

	if opts.EmbedHooks != nil {
		if opts.EmbedHooks.OnMRSynced != nil {
			cb := opts.EmbedHooks.OnMRSynced
			syncer.SetOnMRSynced(
				func(owner, name string, mr *db.MergeRequest) {
					host := "github.com"
					for _, r := range opts.Repos {
						if r.Owner == owner && r.Name == name && r.PlatformHost != "" {
							host = r.PlatformHost
							break
						}
					}
					cb(MergeRequestSummary{
						MergeRequestID: mr.ID,
						RepoOwner:      owner,
						RepoName:       name,
						Number:         mr.Number,
						State:          mr.State,
						Title:          mr.Title,
						IsDraft:        mr.IsDraft,
						CIStatus:       mr.CIStatus,
						ReviewDecision: mr.ReviewDecision,
						PlatformHost:   host,
						CIChecksJSON:   mr.CIChecksJSON,
						UpdatedAt:      mr.UpdatedAt,
					})
				},
			)
		}
		if opts.EmbedHooks.OnSyncCompleted != nil {
			cb := opts.EmbedHooks.OnSyncCompleted
			syncer.SetOnSyncCompleted(
				func(results []ghclient.RepoSyncResult) {
					out := make(
						[]RepoSyncResult, len(results),
					)
					for i, r := range results {
						out[i] = RepoSyncResult{
							Owner:        r.Owner,
							Name:         r.Name,
							PlatformHost: r.PlatformHost,
							Error:        r.Error,
						}
					}
					cb(out)
				},
			)
		}
	}

	srv := server.New(
		database, syncer, frontend,
		cfg.BasePath, cfg,
		server.ServerOptions{
			EmbedConfig: opts.EmbedConfig,
			Clones:      cloneMgr,
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

// PurgeOtherHosts deletes all data for platform hosts other
// than keepHost.
func (inst *Instance) PurgeOtherHosts(keepHost string) error {
	return inst.db.PurgeOtherHosts(keepHost)
}

// SetWatchedMRs sets the list of merge requests to sync on a
// fast interval. Replaces any previous watch list.
func (inst *Instance) SetWatchedMRs(mrs []WatchedMR) {
	inst.syncer.SetWatchedMRs(mrs)
}

// SetWorktreeLinks replaces all worktree links atomically.
func (inst *Instance) SetWorktreeLinks(
	links []WorktreeLink,
) error {
	return inst.db.SetWorktreeLinks(links)
}

// SetActiveWorktree sets the key of the currently focused worktree.
// This is bootstrap-only state: it is injected into the initial HTML
// page load. An already-loaded SPA will not see the change until the
// next full page load. For live updates, mutate
// window.__middleman_config.ui.activeWorktreeKey in the browser and
// call window.__middleman_notify_config_changed().
func (inst *Instance) SetActiveWorktree(key string) {
	inst.server.SetActiveWorktreeKey(key)
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
