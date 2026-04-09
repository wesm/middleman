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
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/testutil"
	"github.com/wesm/middleman/internal/web"
)

func main() {
	port := flag.Int("port", 4174, "port to listen on")
	roborev := flag.String(
		"roborev", "",
		"roborev daemon endpoint (enables proxy)",
	)
	flag.Parse()

	if err := run(*port, *roborev); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(port int, roborevEndpoint string) error {
	tmpDir, err := os.MkdirTemp("", "middleman-e2e-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(tmpDir + "/e2e.db")
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	result, err := testutil.SeedFixtures(ctx, database)
	if err != nil {
		return fmt.Errorf("seed fixtures: %w", err)
	}

	diffRepo, err := testutil.SetupDiffRepo(ctx, tmpDir, database)
	if err != nil {
		return fmt.Errorf("setup diff repo: %w", err)
	}

	cfg := &config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "widgets"},
			{Owner: "acme", Name: "tools"},
			{Owner: "acme", Name: "archived"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}

	if roborevEndpoint != "" {
		cfg.Roborev.Endpoint = roborevEndpoint
	}

	fc := result.FixtureClient()

	// Patch the fixture client's PR for acme/widgets#1 with the real
	// SHAs from the test repo. Without this, the detail store's
	// background sync (POST /repos/.../pulls/1/sync) would upsert the
	// PR with empty platform SHAs, overwriting what SetupDiffRepo set.
	for _, pr := range fc.OpenPRs["acme/widgets"] {
		if pr.GetNumber() == 1 {
			pr.Head.SHA = &diffRepo.HeadSHA
			pr.Base.SHA = &diffRepo.BaseSHA
			break
		}
	}

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{Owner: r.Owner, Name: r.Name, PlatformHost: "github.com"}
	}

	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": fc}, database, diffRepo.Manager, repos, time.Hour, nil, 0)

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.New(database, syncer, assets, "/", cfg, server.ServerOptions{
		Clones: diffRepo.Manager,
	})

	// Do not start the syncer's background loop. The seeded DB is the
	// ground truth for E2E tests; RunOnce would overwrite it with
	// incomplete fixture client data. The syncer only needs to exist
	// for Status() and IsTrackedRepo() calls.

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	slog.Info(fmt.Sprintf("starting e2e server at http://%s", addr))

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
