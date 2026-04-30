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
	"strings"
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

// defaultRoborevEndpoint is the address the e2e server points the
// roborev proxy at when -roborev is not provided. It is deliberately
// an unbindable loopback port so direct playwright runs fail closed
// (the proxy returns 502) instead of silently forwarding test
// traffic to a real local roborev daemon (typically at
// 127.0.0.1:7373). The runner script (scripts/run-roborev-e2e.sh)
// always passes -roborev explicitly to the dockerized seeded daemon.
const defaultRoborevEndpoint = "http://127.0.0.1:1"

func main() {
	port := flag.Int("port", 0, "port to listen on (0 selects a random free port)")
	roborev := flag.String(
		"roborev", defaultRoborevEndpoint,
		"roborev daemon endpoint",
	)
	defaultPlatformHost := flag.String(
		"default-platform-host", "github.com",
		"default platform host for seeded config",
	)
	serverInfoFile := flag.String(
		"server-info-file", "",
		"path to write discovered server port info as JSON",
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	if err := run(
		ctx, *port, *roborev, *serverInfoFile, *defaultPlatformHost,
	); err != nil {
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

type globRefreshContextKey struct{}

func e2eConfigRepos(defaultPlatformHost string) []config.Repo {
	if strings.EqualFold(defaultPlatformHost, "github.com") {
		return []config.Repo{
			{Owner: "acme", Name: "widgets"},
			{Owner: "acme", Name: "tools"},
			{Owner: "acme", Name: "archived"},
			{Owner: "roborev-dev", Name: "*"},
		}
	}
	return []config.Repo{
		{
			Owner:        "enterprise",
			Name:         "service",
			PlatformHost: defaultPlatformHost,
		},
		{
			Owner:        "acme",
			Name:         "widgets",
			PlatformHost: "github.com",
		},
	}
}

func e2eListRepositoriesByOwner(
	fc *testutil.FixtureClient,
) func(context.Context, string) ([]*gh.Repository, error) {
	return func(ctx context.Context, owner string) ([]*gh.Repository, error) {
		switch owner {
		case "import-lab":
			return e2eImportLabRepos(owner), nil
		case "roborev-dev":
			return e2eRoborevRepos(ctx, owner), nil
		default:
			return fc.ReposByOwner[owner], nil
		}
	}
}

func e2eImportLabRepos(owner string) []*gh.Repository {
	return []*gh.Repository{
		e2eRepo(owner, "api", "Import API", false, time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)),
		e2eRepo(owner, "worker", "Import worker", false, time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)),
		e2eRepo(owner, "archived", "Archived import fixture", true, time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)),
	}
}

func e2eRoborevRepos(ctx context.Context, owner string) []*gh.Repository {
	repos := []*gh.Repository{
		e2eRepo(owner, "middleman", "Main dashboard", false, time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)),
		e2eRepo(owner, "worker", "Background jobs", false, time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)),
		e2eRepo(owner, "archived", "Archived service", true, time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)),
	}
	if includeRefreshRepo, _ := ctx.Value(globRefreshContextKey{}).(bool); includeRefreshRepo {
		repos = append(repos, e2eRepo(owner, "review-bot", "Review automation", false, time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)))
	}
	return repos
}

func e2eRepo(owner, name, description string, archived bool, pushedAt time.Time) *gh.Repository {
	privateFalse := false
	return &gh.Repository{
		Name:        &name,
		Owner:       &gh.User{Login: &owner},
		Description: &description,
		Private:     &privateFalse,
		Archived:    &archived,
		PushedAt:    &gh.Timestamp{Time: pushedAt},
	}
}

func runSeededStackDetection(ctx context.Context, database *db.DB) error {
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
	return nil
}

func seedStartupRepos(
	ctx context.Context,
	database *db.DB,
	repos []ghclient.RepoRef,
	defaultPlatformHost string,
) error {
	for _, repo := range repos {
		if _, err := database.UpsertRepo(ctx, repo.PlatformHost, repo.Owner, repo.Name); err != nil {
			return fmt.Errorf("seed startup repo %s/%s: %w", repo.Owner, repo.Name, err)
		}
	}
	if strings.EqualFold(defaultPlatformHost, "github.com") {
		return nil
	}
	if _, err := database.UpsertRepo(ctx, defaultPlatformHost, "enterprise", "service"); err != nil {
		return fmt.Errorf("seed default-host repo: %w", err)
	}
	return nil
}

func e2eRootHandler(
	srv http.Handler,
	database *db.DB,
	diffRepo *testutil.DiffRepoResult,
	fc *testutil.FixtureClient,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/__e2e/pr-diff-summary/advance-head" {
			handleAdvanceHead(w, r, database, diffRepo, fc)
			return
		}
		if r.Method == http.MethodPost &&
			strings.Contains(r.URL.Path, "/api/v1/repos/roborev-dev/") &&
			strings.HasSuffix(r.URL.Path, "/refresh") {
			r = r.WithContext(context.WithValue(r.Context(), globRefreshContextKey{}, true))
		}
		srv.ServeHTTP(w, r)
	})
}

func handleAdvanceHead(
	w http.ResponseWriter,
	r *http.Request,
	database *db.DB,
	diffRepo *testutil.DiffRepoResult,
	fc *testutil.FixtureClient,
) {
	repo, err := database.GetRepoByOwnerName(r.Context(), "acme", "widgets")
	if err != nil || repo == nil {
		http.Error(w, "repo not found", http.StatusNotFound)
		return
	}
	if err := database.UpdateDiffSHAs(r.Context(), repo.ID, 1, diffRepo.AltHeadSHA, diffRepo.BaseSHA, diffRepo.BaseSHA); err != nil {
		http.Error(w, "update diff shas", http.StatusInternalServerError)
		return
	}
	if err := database.UpdatePlatformSHAs(r.Context(), repo.ID, 1, diffRepo.AltHeadSHA, diffRepo.BaseSHA); err != nil {
		http.Error(w, "update platform shas", http.StatusInternalServerError)
		return
	}
	patchFixturePRSHAs(fc, "acme", "widgets", 1, diffRepo.AltHeadSHA, diffRepo.BaseSHA)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"head_sha": diffRepo.AltHeadSHA}); err != nil {
		slog.Warn("write e2e response", "err", err)
	}
}

// run starts the e2e server and blocks until ctx is canceled or the
// HTTP server errors out. Tests call it directly with a cancellable
// context; main() wires it to SIGINT/SIGTERM.
func run(
	ctx context.Context,
	port int,
	roborevEndpoint, serverInfoFile, defaultPlatformHost string,
) error {
	defaultPlatformHost = strings.TrimSpace(defaultPlatformHost)
	if defaultPlatformHost == "" {
		defaultPlatformHost = "github.com"
	}
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

	result, err := testutil.SeedFixtures(ctx, database)
	if err != nil {
		return fmt.Errorf("seed fixtures: %w", err)
	}

	if err := runSeededStackDetection(ctx, database); err != nil {
		return err
	}

	diffRepo, err := testutil.SetupDiffRepo(ctx, tmpDir, database)
	if err != nil {
		return fmt.Errorf("setup diff repo: %w", err)
	}

	repos := e2eConfigRepos(defaultPlatformHost)
	cfg := &config.Config{
		SyncInterval:        "5m",
		GitHubTokenEnv:      "MIDDLEMAN_GITHUB_TOKEN",
		DefaultPlatformHost: defaultPlatformHost,
		Host:                "127.0.0.1",
		Port:                8091,
		BasePath:            "/",
		Repos:               repos,
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}

	cfg.Roborev.Endpoint = roborevEndpoint
	cfgPath := filepath.Join(tmpDir, "config.toml")
	if err := cfg.Save(cfgPath); err != nil {
		return fmt.Errorf("save e2e config: %w", err)
	}

	fc := result.FixtureClient()
	fc.ListRepositoriesByOwnerFn = e2eListRepositoriesByOwner(fc)
	patchFixturePRSHAs(fc, "acme", "widgets", 1, diffRepo.HeadSHA, diffRepo.BaseSHA)

	fixtureClients := map[string]ghclient.Client{
		"github.com":        fc,
		defaultPlatformHost: fc,
	}
	startupResolved := ghclient.ResolveConfiguredRepos(
		ctx,
		fixtureClients,
		cfg.Repos,
	)
	if err := seedStartupRepos(ctx, database, startupResolved.Expanded, defaultPlatformHost); err != nil {
		return err
	}

	rt := ghclient.NewRateTracker(database, "github.com", "rest")
	// Seed with known values so the budget bars render.
	rt.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 4200,
		Reset:     gh.Timestamp{Time: time.Now().Add(45 * time.Minute)},
	})

	gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")
	gqlRT.UpdateFromRate(gh.Rate{
		Limit:     5000,
		Remaining: 4800,
		Reset:     gh.Timestamp{Time: time.Now().Add(40 * time.Minute)},
	})

	budget := ghclient.NewSyncBudget(500)
	budget.Spend(75)

	syncer := ghclient.NewSyncer(
		fixtureClients,
		database, diffRepo.Manager, startupResolved.Expanded, time.Hour,
		map[string]*ghclient.RateTracker{
			"github.com":        rt,
			defaultPlatformHost: rt,
		},
		map[string]*ghclient.SyncBudget{
			"github.com":        budget,
			defaultPlatformHost: budget,
		},
	)

	// Wire GraphQL fetcher so GQL rate data appears in the endpoint.
	gqlFetcher := ghclient.NewGraphQLFetcher("fake-token", "github.com", gqlRT, budget)
	syncer.SetFetchers(map[string]*ghclient.GraphQLFetcher{
		"github.com":        gqlFetcher,
		defaultPlatformHost: gqlFetcher,
	})

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.NewWithConfig(
		database, syncer, diffRepo.Manager, assets, cfg, cfgPath,
		server.ServerOptions{
			Clones:      diffRepo.Manager,
			WorktreeDir: filepath.Join(tmpDir, "worktrees"),
		},
	)
	rootHandler := e2eRootHandler(srv, database, diffRepo, fc)

	return serveE2E(ctx, port, serverInfoFile, rootHandler, srv)
}

func serveE2E(
	ctx context.Context,
	port int,
	serverInfoFile string,
	rootHandler http.Handler,
	srv *server.Server,
) error {
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
		Handler:     rootHandler,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}
	defer shutdownE2EServer(srv, httpServer)

	errCh := make(chan error, 1)
	go serveHTTP(listener, httpServer, errCh)
	return waitForE2EServer(ctx, srv, httpServer, errCh)
}

func shutdownE2EServer(srv *server.Server, httpServer *http.Server) func() {
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server shutdown", "err", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http shutdown", "err", err)
		}
	}
}

func serveHTTP(listener net.Listener, httpServer *http.Server, errCh chan<- error) {
	if serveErr := httpServer.Serve(listener); !errors.Is(serveErr, http.ErrServerClosed) {
		errCh <- serveErr
	}
	close(errCh)
}

func waitForE2EServer(
	ctx context.Context,
	srv *server.Server,
	httpServer *http.Server,
	errCh <-chan error,
) error {
	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownE2EServer(srv, httpServer)()
		if serveErr, ok := <-errCh; ok {
			return fmt.Errorf("server: %w", serveErr)
		}
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
	oldHeadSHA := ""
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
			if oldHeadSHA == "" {
				oldHeadSHA = pr.Head.GetSHA()
			}
			pr.Head.SHA = &headSHA
			pr.Base.SHA = &baseSHA
		}
	}

	patch(fc.OpenPRs[repoKey])
	patch(fc.PRs[repoKey])

	if oldHeadSHA == "" || oldHeadSHA == headSHA {
		return
	}
	oldRefKey := fmt.Sprintf("%s/%s@%s", owner, repo, oldHeadSHA)
	newRefKey := fmt.Sprintf("%s/%s@%s", owner, repo, headSHA)
	if combined, ok := fc.CombinedStatuses[oldRefKey]; ok {
		fc.CombinedStatuses[newRefKey] = combined
	}
	if runs, ok := fc.CheckRuns[oldRefKey]; ok {
		fc.CheckRuns[newRefKey] = runs
	}
}
