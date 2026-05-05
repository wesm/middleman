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
	"slices"
	"strings"
	"syscall"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/platform"
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
		ctx,
		*port,
		*roborev,
		*serverInfoFile,
		*defaultPlatformHost,
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

type e2eStaticProvider struct {
	kind        platform.Kind
	host        string
	caps        platform.Capabilities
	issue       platform.Issue
	issueEvents []platform.IssueEvent
}

func (p e2eStaticProvider) Platform() platform.Kind {
	return p.kind
}

func (p e2eStaticProvider) Host() string {
	return p.host
}

func (p e2eStaticProvider) Capabilities() platform.Capabilities {
	return p.caps
}

func (p e2eStaticProvider) ListOpenIssues(
	_ context.Context,
	ref platform.RepoRef,
) ([]platform.Issue, error) {
	if ref.RepoPath != p.issue.Repo.RepoPath {
		return nil, nil
	}
	if p.issue.State != "open" {
		return nil, nil
	}
	return []platform.Issue{p.issue}, nil
}

func (p e2eStaticProvider) GetIssue(
	_ context.Context,
	ref platform.RepoRef,
	number int,
) (platform.Issue, error) {
	if ref.RepoPath == p.issue.Repo.RepoPath && number == p.issue.Number {
		return p.issue, nil
	}
	return platform.Issue{}, fmt.Errorf("e2e static provider: issue %s#%d not found", ref.RepoPath, number)
}

func (p e2eStaticProvider) ListIssueEvents(
	_ context.Context,
	ref platform.RepoRef,
	number int,
) ([]platform.IssueEvent, error) {
	if ref.RepoPath == p.issue.Repo.RepoPath && number == p.issue.Number {
		return slices.Clone(p.issueEvents), nil
	}
	return nil, nil
}

type globRefreshContextKey struct{}

func gitLabReadOnlyRepoRef() platform.RepoRef {
	return platform.RepoRef{
		Platform:      platform.KindGitLab,
		Host:          "gitlab.example.com",
		Owner:         "group",
		Name:          "project",
		RepoPath:      "group/project",
		WebURL:        "https://gitlab.example.com/group/project",
		CloneURL:      "https://gitlab.example.com/group/project.git",
		DefaultBranch: "main",
	}
}

func gitLabReadOnlyIssueFixture(now time.Time) (platform.Issue, []platform.IssueEvent) {
	ref := gitLabReadOnlyRepoRef()
	issue := platform.Issue{
		Repo:         ref,
		PlatformID:   7101,
		Number:       11,
		URL:          "https://gitlab.example.com/group/project/-/issues/11",
		Title:        "GitLab read-only issue",
		Author:       "ada",
		State:        "open",
		Body:         "GitLab issue body",
		CommentCount: 1,
		CreatedAt:    now.Add(-48 * time.Hour),
		UpdatedAt:    now,
	}
	events := []platform.IssueEvent{
		{
			Repo:        ref,
			PlatformID:  7201,
			IssueNumber: 11,
			EventType:   "issue_comment",
			Author:      "ada",
			Body:        "GitLab read-only timeline comment",
			CreatedAt:   now,
			DedupeKey:   "gitlab-read-only-issue-comment",
		},
	}
	return issue, events
}

func seedGitLabReadOnlyCapabilityFixture(ctx context.Context, database *db.DB) error {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	issue, events := gitLabReadOnlyIssueFixture(now)
	repoID, err := database.UpsertRepo(ctx, db.RepoIdentity{
		Platform:       "gitlab",
		PlatformHost:   "gitlab.example.com",
		PlatformRepoID: "7001",
		Owner:          issue.Repo.Owner,
		Name:           issue.Repo.Name,
		RepoPath:       issue.Repo.RepoPath,
	})
	if err != nil {
		return fmt.Errorf("upsert gitlab repo: %w", err)
	}
	issueID, err := database.UpsertIssue(ctx, &db.Issue{
		RepoID:          repoID,
		PlatformID:      issue.PlatformID,
		Number:          issue.Number,
		URL:             issue.URL,
		Title:           issue.Title,
		Author:          issue.Author,
		State:           issue.State,
		Body:            issue.Body,
		CommentCount:    issue.CommentCount,
		CreatedAt:       issue.CreatedAt,
		UpdatedAt:       issue.UpdatedAt,
		LastActivityAt:  now,
		DetailFetchedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("upsert gitlab issue: %w", err)
	}
	commentID := events[0].PlatformID
	if err := database.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:    issueID,
			PlatformID: &commentID,
			EventType:  events[0].EventType,
			Author:     events[0].Author,
			Body:       events[0].Body,
			CreatedAt:  events[0].CreatedAt,
			DedupeKey:  events[0].DedupeKey,
		},
	}); err != nil {
		return fmt.Errorf("upsert gitlab issue event: %w", err)
	}
	return nil
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
	if err := seedGitLabReadOnlyCapabilityFixture(ctx, database); err != nil {
		return fmt.Errorf("seed gitlab capability fixture: %w", err)
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

	repos := []config.Repo{
		{Owner: "acme", Name: "widgets"},
		{Owner: "acme", Name: "tools"},
		{Owner: "acme", Name: "archived"},
		{Owner: "roborev-dev", Name: "*"},
	}
	if !strings.EqualFold(defaultPlatformHost, "github.com") {
		repos = []config.Repo{
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
	fc.ListRepositoriesByOwnerFn = func(
		ctx context.Context, owner string,
	) ([]*gh.Repository, error) {
		pushedMiddleman := gh.Timestamp{Time: time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)}
		pushedWorker := gh.Timestamp{Time: time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)}
		pushedBot := gh.Timestamp{Time: time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)}
		privateFalse := false
		if owner == "import-lab" {
			return []*gh.Repository{
				{
					Name:        new("api"),
					Owner:       &gh.User{Login: new(owner)},
					Description: new("Import API"),
					Private:     &privateFalse,
					Archived:    new(false),
					PushedAt:    &pushedMiddleman,
				},
				{
					Name:        new("worker"),
					Owner:       &gh.User{Login: new(owner)},
					Description: new("Import worker"),
					Private:     &privateFalse,
					Archived:    new(false),
					PushedAt:    &pushedWorker,
				},
				{
					Name:        new("archived"),
					Owner:       &gh.User{Login: new(owner)},
					Description: new("Archived import fixture"),
					Private:     &privateFalse,
					Archived:    new(true),
					PushedAt:    &pushedBot,
				},
			}, nil
		}
		if owner != "roborev-dev" {
			return fc.ReposByOwner[owner], nil
		}

		repos := []*gh.Repository{
			{
				Name:        new("middleman"),
				Owner:       &gh.User{Login: new(owner)},
				Description: new("Main dashboard"),
				Private:     &privateFalse,
				Archived:    new(false),
				PushedAt:    &pushedMiddleman,
			},
			{
				Name:        new("worker"),
				Owner:       &gh.User{Login: new(owner)},
				Description: new("Background jobs"),
				Private:     &privateFalse,
				Archived:    new(false),
				PushedAt:    &pushedWorker,
			},
			{
				Name:        new("archived"),
				Owner:       &gh.User{Login: new(owner)},
				Description: new("Archived service"),
				Private:     new(false),
				Archived:    new(true),
				PushedAt:    &pushedBot,
			},
		}
		if includeRefreshRepo, _ := ctx.Value(globRefreshContextKey{}).(bool); includeRefreshRepo {
			repos = append(repos, &gh.Repository{
				Name:        new("review-bot"),
				Owner:       &gh.User{Login: new(owner)},
				Description: new("Review automation"),
				Private:     &privateFalse,
				Archived:    new(false),
				PushedAt:    &pushedBot,
			})
		}
		return repos, nil
	}
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
	for _, repo := range startupResolved.Expanded {
		if _, err := database.UpsertRepo(
			ctx, repo.PlatformHost, repo.Owner, repo.Name,
		); err != nil {
			return fmt.Errorf("seed startup repo %s/%s: %w", repo.Owner, repo.Name, err)
		}
	}
	if !strings.EqualFold(defaultPlatformHost, "github.com") {
		if _, err := database.UpsertRepo(
			ctx, defaultPlatformHost, "enterprise", "service",
		); err != nil {
			return fmt.Errorf("seed default-host repo: %w", err)
		}
	}

	rt := ghclient.NewRateTracker(database, "github.com", "rest")
	// Seed with known values so the budget bars render.
	rt.UpdateFromRate(ghclient.Rate{
		Limit:     5000,
		Remaining: 4200,
		Reset:     time.Now().Add(45 * time.Minute),
	})

	gqlRT := ghclient.NewRateTracker(database, "github.com", "graphql")
	gqlRT.UpdateFromRate(ghclient.Rate{
		Limit:     5000,
		Remaining: 4800,
		Reset:     time.Now().Add(40 * time.Minute),
	})

	budget := ghclient.NewSyncBudget(500)
	budget.Spend(75)

	gitLabIssue, gitLabIssueEvents := gitLabReadOnlyIssueFixture(
		time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
	)
	registry, err := ghclient.NewProviderRegistry(
		fixtureClients,
		e2eStaticProvider{
			kind:        platform.KindGitLab,
			host:        "gitlab.example.com",
			issue:       gitLabIssue,
			issueEvents: gitLabIssueEvents,
			caps: platform.Capabilities{
				ReadIssues:   true,
				ReadComments: true,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("create e2e provider registry: %w", err)
	}
	trackedRepos := append(
		slices.Clone(startupResolved.Expanded),
		ghclient.RepoRef{
			Platform:      platform.KindGitLab,
			PlatformHost:  gitLabIssue.Repo.Host,
			Owner:         gitLabIssue.Repo.Owner,
			Name:          gitLabIssue.Repo.Name,
			RepoPath:      gitLabIssue.Repo.RepoPath,
			WebURL:        gitLabIssue.Repo.WebURL,
			CloneURL:      gitLabIssue.Repo.CloneURL,
			DefaultBranch: gitLabIssue.Repo.DefaultBranch,
		},
	)
	syncer := ghclient.NewSyncerWithRegistry(
		registry,
		database, diffRepo.Manager, trackedRepos, time.Hour,
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
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost &&
			r.URL.Path == "/__e2e/pr-diff-summary/advance-head" {
			repo, err := database.GetRepoByOwnerName(
				r.Context(), "acme", "widgets",
			)
			if err != nil || repo == nil {
				http.Error(w, "repo not found", http.StatusNotFound)
				return
			}
			if err := database.UpdateDiffSHAs(
				r.Context(), repo.ID, 1,
				diffRepo.AltHeadSHA, diffRepo.BaseSHA, diffRepo.BaseSHA,
			); err != nil {
				http.Error(w, "update diff shas", http.StatusInternalServerError)
				return
			}
			if err := database.UpdatePlatformSHAs(
				r.Context(), repo.ID, 1,
				diffRepo.AltHeadSHA, diffRepo.BaseSHA,
			); err != nil {
				http.Error(w, "update platform shas", http.StatusInternalServerError)
				return
			}
			patchFixturePRSHAs(
				fc, "acme", "widgets", 1,
				diffRepo.AltHeadSHA, diffRepo.BaseSHA,
			)
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]string{
				"head_sha": diffRepo.AltHeadSHA,
			}); err != nil {
				slog.Warn("write e2e response", "err", err)
			}
			return
		}
		if r.Method == http.MethodPost &&
			strings.Contains(r.URL.Path, "/api/v1/repos/roborev-dev/") &&
			strings.HasSuffix(r.URL.Path, "/refresh") {
			r = r.WithContext(
				context.WithValue(r.Context(), globRefreshContextKey{}, true),
			)
		}
		srv.ServeHTTP(w, r)
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
		Handler:     rootHandler,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 60 * time.Second,
	}

	// Drain HTTP handlers and bg goroutines before DB close.
	// LIFO ordering: this runs after stop() but before the
	// deferred database.Close above. srv.Shutdown closes the
	// hub so SSE handlers exit, then drains bg goroutines;
	// httpServer.Shutdown drains in-flight HTTP handlers.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server shutdown", "err", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http shutdown", "err", err)
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		if serveErr := httpServer.Serve(listener); !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		// Trigger Shutdown so Serve unblocks (the defer is a
		// safety net for other exit paths and is idempotent).
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), 10*time.Second,
		)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("server shutdown", "err", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http shutdown", "err", err)
		}
		// Drain errCh so a real Serve failure (not
		// ErrServerClosed) is surfaced instead of swallowed.
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
