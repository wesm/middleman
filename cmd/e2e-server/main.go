package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/stacks"
	"github.com/wesm/middleman/internal/testutil"
	"github.com/wesm/middleman/internal/web"
)

func main() {
	port := flag.Int("port", 0, "port to listen on (0 selects a random free port)")
	roborev := flag.String(
		"roborev", "",
		"roborev daemon endpoint (enables proxy)",
	)
	serverInfoFile := flag.String(
		"server-info-file", "",
		"path to write discovered server port info as JSON",
	)
	flag.Parse()

	if err := run(*port, *roborev, *serverInfoFile); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type e2eServerInfo struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	BaseURL string `json:"base_url"`
	PID     int    `json:"pid"`
}

func run(port int, roborevEndpoint, serverInfoFile string) error {
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

	// Run stack detection so seeded stacked chains are discoverable
	// via /api/v1/stacks and the PR detail sidebar.
	for _, rp := range []struct{ owner, name string }{
		{"acme", "widgets"},
		{"acme", "tools"},
	} {
		repo, err := database.GetRepoByOwnerName(ctx, rp.owner, rp.name)
		if err != nil || repo == nil {
			continue
		}
		if err := stacks.RunDetection(ctx, database, repo.ID); err != nil {
			return fmt.Errorf("stack detection %s/%s: %w", rp.owner, rp.name, err)
		}
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
	patchFixturePRSHAs(fc, "acme", "widgets", 1, diffRepo.HeadSHA, diffRepo.BaseSHA)

	repos := make([]ghclient.RepoRef, len(cfg.Repos))
	for i, r := range cfg.Repos {
		repos[i] = ghclient.RepoRef{Owner: r.Owner, Name: r.Name, PlatformHost: "github.com"}
	}

	syncer := ghclient.NewSyncer(map[string]ghclient.Client{"github.com": fc}, database, diffRepo.Manager, repos, time.Hour, nil, nil)

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

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("unexpected listener addr type %T", listener.Addr())
	}

	info := e2eServerInfo{
		Host:    "127.0.0.1",
		Port:    tcpAddr.Port,
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", tcpAddr.Port),
		PID:     os.Getpid(),
	}
	if err := writeServerInfoFile(serverInfoFile, info); err != nil {
		return fmt.Errorf("write server info file: %w", err)
	}
	defer cleanupServerInfoFile(serverInfoFile)

	slog.Info(fmt.Sprintf("starting e2e server at %s", info.BaseURL))

	httpServer := &http.Server{
		Handler:     srv,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(listener); !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
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

func cleanupServerInfoFile(path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("cleanup server info file failed", "path", path, "err", err)
	}
}

func writeServerInfoFile(path string, info e2eServerInfo) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir server info dir: %w", err)
	}

	content, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal server info: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(content, '\n'), 0o644); err != nil {
		return fmt.Errorf("write temp server info file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename server info file: %w", err)
	}
	return nil
}

func patchFixturePRSHAs(fc *testutil.FixtureClient, owner, repo string, number int, headSHA, baseSHA string) {
	if fc == nil {
		return
	}

	repoKey := fmt.Sprintf("%s/%s", owner, repo)
	patch := func(prs []*gh.PullRequest) {
		for _, pr := range prs {
			if pr.GetNumber() != number {
				continue
			}
			if pr.Head == nil {
				pr.Head = &gh.PullRequestBranch{}
			}
			if pr.Base == nil {
				pr.Base = &gh.PullRequestBranch{}
			}
			pr.Head.SHA = &headSHA
			pr.Base.SHA = &baseSHA
		}
	}

	patch(fc.OpenPRs[repoKey])
	patch(fc.PRs[repoKey])
}
