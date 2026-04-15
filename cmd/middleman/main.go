package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/stacks"
	"github.com/wesm/middleman/internal/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf(
			"middleman %s (%s) built %s\n",
			version, commit, buildDate,
		)
		os.Exit(0)
	}

	configPath := flag.String(
		"config", config.DefaultConfigPath(),
		"path to config file",
	)
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	if err := config.EnsureDefault(configPath); err != nil {
		return fmt.Errorf("ensure config: %w", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	globalToken := cfg.GitHubToken()
	if globalToken == "" {
		return fmt.Errorf(
			"GitHub token not set: env var %q is empty",
			cfg.GitHubTokenEnv,
		)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf(
			"create data directory %s: %w", cfg.DataDir, err,
		)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	// Build per-host tokens from repo config first, so explicit
	// token_env overrides are honored (including for github.com).
	hostTokens := make(map[string]string, len(cfg.Repos)+1)
	for _, r := range cfg.Repos {
		host := r.PlatformHostOrDefault()
		if _, seen := hostTokens[host]; seen {
			continue
		}
		token := r.ResolveToken(globalToken)
		if token == "" {
			return fmt.Errorf(
				"no token for host %s (repo %s/%s)",
				host, r.Owner, r.Name,
			)
		}
		hostTokens[host] = token
	}
	// Seed github.com from the global token if no repo already
	// provided one, so the settings UI can validate repos even
	// with empty or GHE-only configs.
	if _, ok := hostTokens["github.com"]; !ok {
		hostTokens["github.com"] = globalToken
	}

	rateTrackers := make(
		map[string]*ghclient.RateTracker, len(hostTokens),
	)
	budgetPerHour := cfg.BudgetPerHour()
	budgets := make(
		map[string]*ghclient.SyncBudget, len(hostTokens),
	)
	clients := make(
		map[string]ghclient.Client, len(hostTokens),
	)
	cloneTokens := make(
		map[string]string, len(hostTokens),
	)
	for host, token := range hostTokens {
		rateTrackers[host] = ghclient.NewRateTracker(
			database, host, "rest",
		)
		if budgetPerHour > 0 {
			budgets[host] = ghclient.NewSyncBudget(
				budgetPerHour,
			)
		}
		c, err := ghclient.NewClient(
			token, host, rateTrackers[host], budgets[host],
		)
		if err != nil {
			return fmt.Errorf(
				"create client for %s: %w", host, err,
			)
		}
		clients[host] = c
		cloneTokens[host] = token
	}

	repos := resolveStartupRepos(
		context.Background(), cfg, clients, database,
	)

	cloneMgr := gitclone.New(
		filepath.Join(cfg.DataDir, "clones"), cloneTokens,
	)

	syncer := ghclient.NewSyncer(
		clients, database, cloneMgr, repos,
		cfg.SyncDuration(), rateTrackers, budgets,
	)

	fetchers := make(
		map[string]*ghclient.GraphQLFetcher, len(hostTokens),
	)
	for host, token := range hostTokens {
		gqlRT := ghclient.NewRateTracker(database, host, "graphql")
		fetchers[host] = ghclient.NewGraphQLFetcher(
			token, host, gqlRT, budgets[host],
		)
	}
	syncer.SetFetchers(fetchers)

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.NewWithConfig(
		database, syncer, cloneMgr, assets,
		cfg, configPath, server.ServerOptions{
			WorktreeDir: filepath.Join(cfg.DataDir, "worktrees"),
		},
	)

	// Wire status callback and prime the SSE event hub so clients
	// can show live sync state without polling.
	syncer.SetOnStatusChange(func(status *ghclient.SyncStatus) {
		srv.Hub().Broadcast(server.Event{
			Type: "sync_status",
			Data: status,
		})
		if !status.Running {
			srv.Hub().Broadcast(server.Event{
				Type: "data_changed",
				Data: struct{}{},
			})
		}
	})
	srv.Hub().Broadcast(server.Event{
		Type: "sync_status",
		Data: syncer.Status(),
	})

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	syncer.SetOnSyncCompleted(stacks.SyncCompletedHook(ctx, database, nil))
	syncer.Start(ctx)
	defer syncer.Stop()
	defer stop()

	// srv.Shutdown MUST be the last-registered defer so LIFO runs
	// it FIRST on return: close the HTTP listener (and SSE hub)
	// before syncer.Stop blocks for up to 30 s, otherwise the
	// process keeps serving requests against a syncer that is
	// already winding down.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server shutdown", "err", err)
		}
	}()

	displayVersion := version
	if version == "dev" && commit != "unknown" {
		displayVersion = "dev-" + commit
	}
	srv.SetVersion(displayVersion)

	addr := cfg.ListenAddr()
	slog.Info(fmt.Sprintf("starting server at http://%s", addr))

	errCh := make(chan error, 1)
	go func() {
		if listenErr := srv.ListenAndServe(addr); !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- listenErr
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		return nil
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}
}

func resolveStartupRepos(
	ctx context.Context,
	cfg *config.Config,
	clients map[string]ghclient.Client,
	database *db.DB,
) []ghclient.RepoRef {
	seen := make(map[string]struct{})
	repos := make([]ghclient.RepoRef, 0, len(cfg.Repos))
	for _, raw := range cfg.Repos {
		_, expanded, err := ghclient.ResolveConfiguredRepo(
			ctx, clients, raw,
		)
		if err != nil {
			slog.Warn("resolve configured repo", "err", err)
			if raw.HasNameGlob() {
				expanded = fallbackGlobFromDB(
					ctx, database, raw,
				)
			} else {
				expanded = []ghclient.RepoRef{{
					Owner:        raw.Owner,
					Name:         raw.Name,
					PlatformHost: raw.PlatformHostOrDefault(),
				}}
			}
		}
		for _, repo := range expanded {
			key := strings.ToLower(repo.PlatformHost) + "\x00" +
				strings.ToLower(repo.Owner) + "\x00" +
				strings.ToLower(repo.Name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			repos = append(repos, repo)
		}
	}
	return repos
}

// fallbackGlobFromDB returns repos from the database that match
// the glob config entry, preserving previously tracked matches
// when GitHub is unreachable at startup.
func fallbackGlobFromDB(
	ctx context.Context,
	database *db.DB,
	raw config.Repo,
) []ghclient.RepoRef {
	if database == nil {
		return nil
	}
	dbRepos, err := database.ListRepos(ctx)
	if err != nil {
		slog.Warn("fallback glob from db", "err", err)
		return nil
	}
	host := raw.PlatformHostOrDefault()
	var matches []ghclient.RepoRef
	for _, r := range dbRepos {
		dbHost := r.PlatformHost
		if dbHost == "" {
			dbHost = "github.com"
		}
		if !strings.EqualFold(dbHost, host) ||
			!strings.EqualFold(r.Owner, raw.Owner) {
			continue
		}
		matched, _ := path.Match(
			strings.ToLower(raw.Name),
			strings.ToLower(r.Name),
		)
		if matched {
			matches = append(matches, ghclient.RepoRef{
				Owner:        r.Owner,
				Name:         r.Name,
				PlatformHost: dbHost,
			})
		}
	}
	if len(matches) > 0 {
		slog.Info(
			"using DB-persisted repos for offline glob",
			"pattern", raw.Owner+"/"+raw.Name,
			"count", len(matches),
		)
	}
	return matches
}
