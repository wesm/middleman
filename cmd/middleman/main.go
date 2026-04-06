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
	"syscall"

	"path/filepath"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitclone"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
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

	// Always seed github.com from the global token so the settings
	// UI can validate repos even with empty or GHE-only configs.
	hostTokens := map[string]string{"github.com": globalToken}
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

	rateTrackers := make(
		map[string]*ghclient.RateTracker, len(hostTokens),
	)
	clients := make(
		map[string]ghclient.Client, len(hostTokens),
	)
	cloneTokens := make(
		map[string]string, len(hostTokens),
	)
	for host, token := range hostTokens {
		rateTrackers[host] = ghclient.NewRateTracker(
			database, host,
		)
		c, err := ghclient.NewClient(
			token, host, rateTrackers[host],
		)
		if err != nil {
			return fmt.Errorf(
				"create client for %s: %w", host, err,
			)
		}
		clients[host] = c
		cloneTokens[host] = token
	}

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{
			Owner:        r.Owner,
			Name:         r.Name,
			PlatformHost: r.PlatformHostOrDefault(),
		}
	}

	cloneMgr := gitclone.New(
		filepath.Join(cfg.DataDir, "clones"), cloneTokens,
	)

	syncer := ghclient.NewSyncer(
		clients, database, cloneMgr, repos,
		cfg.SyncDuration(), rateTrackers,
	)

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.NewWithConfig(
		database, syncer, cloneMgr, assets,
		cfg, configPath, server.ServerOptions{},
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	syncer.Start(ctx)
	defer syncer.Stop()
	defer stop()

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
