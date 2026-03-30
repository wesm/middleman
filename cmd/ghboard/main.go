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

	"github.com/wesm/ghboard/internal/config"
	"github.com/wesm/ghboard/internal/db"
	ghclient "github.com/wesm/ghboard/internal/github"
	"github.com/wesm/ghboard/internal/server"
	"github.com/wesm/ghboard/internal/web"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("ghboard %s (%s) built %s\n", version, commit, buildDate)
		os.Exit(0)
	}

	configPath := flag.String("config", config.DefaultConfigPath(), "path to config file")
	flag.Parse()

	if err := run(*configPath); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	token := cfg.GitHubToken()
	if token == "" {
		return fmt.Errorf("GitHub token not set: env var %q is empty", cfg.GitHubTokenEnv)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data directory %s: %w", cfg.DataDir, err)
	}

	database, err := db.Open(cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	ghClient := ghclient.NewClient(token)

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{Owner: r.Owner, Name: r.Name}
	}

	syncer := ghclient.NewSyncer(ghClient, database, repos, cfg.SyncDuration())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	syncer.Start(ctx)
	defer syncer.Stop()

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.New(database, ghClient, syncer, assets)

	addr := cfg.ListenAddr()
	slog.Info(fmt.Sprintf("starting server at http://%s", addr))

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(addr); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
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
