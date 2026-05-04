package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/wesm/middleman/internal/platform"
	gitlabclient "github.com/wesm/middleman/internal/platform/gitlab"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/stacks"
	"github.com/wesm/middleman/internal/web"
)

type splitLogHandler struct {
	handlers []slog.Handler
}

func (h splitLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h splitLogHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, handler := range h.handlers {
		if !handler.Enabled(ctx, r.Level) {
			continue
		}
		if err := handler.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (h splitLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithAttrs(attrs))
	}
	return splitLogHandler{handlers: handlers}
}

func (h splitLogHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, 0, len(h.handlers))
	for _, handler := range h.handlers {
		handlers = append(handlers, handler.WithGroup(name))
	}
	return splitLogHandler{handlers: handlers}
}

var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	closeLog, err := configureLogging(os.Stderr)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "configure logging: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := closeLog(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "close log file: %v\n", err)
		}
	}()

	if err := runCLI(os.Args[1:], os.Stdout); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func configureLogging(stderr io.Writer) (func() error, error) {
	level, err := parseLogLevel(os.Getenv("MIDDLEMAN_LOG_LEVEL"))
	if err != nil {
		return nil, err
	}

	var file *os.File
	logFile := strings.TrimSpace(os.Getenv("MIDDLEMAN_LOG_FILE"))
	stderrLevel := level
	if logFile != "" {
		stderrLevel = slog.LevelInfo
	}
	if raw := os.Getenv("MIDDLEMAN_LOG_STDERR_LEVEL"); strings.TrimSpace(raw) != "" {
		stderrLevel, err = parseLogLevel(raw)
		if err != nil {
			return nil, err
		}
	}

	handlers := []slog.Handler{
		slog.NewTextHandler(
			stderr,
			&slog.HandlerOptions{Level: stderrLevel},
		),
	}
	if logFile != "" {
		if err := os.MkdirAll(filepath.Dir(logFile), 0o700); err != nil {
			return nil, fmt.Errorf("create log directory: %w", err)
		}
		file, err = os.OpenFile(
			logFile,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0o600,
		)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		handlers = append(
			handlers,
			slog.NewTextHandler(
				file,
				&slog.HandlerOptions{Level: level},
			),
		)
	}

	slog.SetDefault(slog.New(splitLogHandler{handlers: handlers}))
	slog.Debug(
		"logging configured",
		"level", level.String(),
		"stderr_level", stderrLevel.String(),
		"file", logFile,
	)

	return func() error {
		if file == nil {
			return nil
		}
		return file.Close()
	}, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf(
			"unsupported MIDDLEMAN_LOG_LEVEL %q", raw,
		)
	}
}

func runCLI(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			_, err := fmt.Fprintf(
				stdout,
				"middleman %s (%s) built %s\n",
				version, commit, buildDate,
			)
			return err
		case "config":
			return runConfigCLI(args[1:], stdout)
		}
	}

	fs := flag.NewFlagSet("middleman", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String(
		"config", config.DefaultConfigPath(),
		"path to config file",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return run(*configPath)
}

func runConfigCLI(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("config command requires subcommand")
	}

	switch args[0] {
	case "read":
		return runConfigRead(args[1:], stdout)
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runConfigRead(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("middleman config read", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String(
		"config", config.DefaultConfigPath(),
		"path to config file",
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("config read requires exactly one key")
	}

	if err := config.EnsureDefault(*configPath); err != nil {
		return fmt.Errorf("ensure config: %w", err)
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	switch fs.Arg(0) {
	case "port":
		_, err := fmt.Fprintf(stdout, "%d\n", cfg.Port)
		return err
	default:
		return fmt.Errorf("unsupported config key %q", fs.Arg(0))
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
	slog.Debug(
		"config loaded",
		"config_path", configPath,
		"data_dir", cfg.DataDir,
		"db_path", cfg.DBPath(),
		"listen_addr", cfg.ListenAddr(),
		"repo_count", len(cfg.Repos),
	)

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

	// Build provider/host tokens from repo config first, so
	// explicit token_env overrides are honored per provider.
	providerTokens := make(map[string]string, len(cfg.Repos)+len(cfg.Platforms)+1)
	for _, r := range cfg.Repos {
		platformName := r.PlatformOrDefault()
		host := r.PlatformHostOrDefault()
		key := providerHostKey(platformName, host)
		if _, seen := providerTokens[key]; seen {
			continue
		}
		token := cfg.ResolveRepoToken(r)
		if token == "" {
			return fmt.Errorf(
				"no token for %s host %s (repo %s/%s)",
				platformName, host, r.Owner, r.Name,
			)
		}
		providerTokens[key] = token
	}
	for _, p := range cfg.Platforms {
		key := providerHostKey(p.Type, p.Host)
		if _, seen := providerTokens[key]; seen {
			continue
		}
		token := cfg.TokenForPlatformHost(p.Type, p.Host, "")
		if token != "" {
			providerTokens[key] = token
		}
	}
	// Seed github.com from the global token if available and no
	// repo/platform already provided one, so the settings UI can
	// validate GitHub repos even with empty or GHE-only configs.
	globalGitHubToken := cfg.GitHubToken()
	defaultGitHubKey := providerHostKey("github", "github.com")
	if globalGitHubToken != "" {
		if _, ok := providerTokens[defaultGitHubKey]; !ok {
			providerTokens[defaultGitHubKey] = globalGitHubToken
		}
	}
	if err := validateProviderHostKeys(providerTokens); err != nil {
		return err
	}

	rateTrackers := make(
		map[string]*ghclient.RateTracker, len(providerTokens),
	)
	budgetPerHour := cfg.BudgetPerHour()
	budgets := make(
		map[string]*ghclient.SyncBudget, len(providerTokens),
	)
	clients := make(
		map[string]ghclient.Client, len(providerTokens),
	)
	providers := make(
		[]platform.Provider, 0, len(providerTokens),
	)
	cloneTokens := make(
		map[string]string, len(providerTokens),
	)
	githubTokens := make(map[string]string, len(providerTokens))
	for key, token := range providerTokens {
		platformName, host := splitProviderHostKey(key)
		if _, ok := rateTrackers[host]; !ok {
			rateTrackers[host] = ghclient.NewRateTracker(
				database, host, "rest",
			)
		}
		if budgetPerHour > 0 {
			if _, ok := budgets[host]; !ok {
				budgets[host] = ghclient.NewSyncBudget(
					budgetPerHour,
				)
			}
		}
		switch platformName {
		case "github":
			c, err := ghclient.NewClient(
				token, host, rateTrackers[host], budgets[host],
			)
			if err != nil {
				return fmt.Errorf(
					"create GitHub client for %s: %w", host, err,
				)
			}
			clients[host] = c
			githubTokens[host] = token
		case "gitlab":
			c, err := gitlabclient.NewClient(host, token)
			if err != nil {
				return fmt.Errorf(
					"create GitLab client for %s: %w", host, err,
				)
			}
			providers = append(providers, c)
		default:
			return fmt.Errorf("unsupported platform %q", platformName)
		}
		if _, ok := cloneTokens[host]; !ok {
			cloneTokens[host] = token
		}
	}

	registry, err := ghclient.NewProviderRegistry(clients, providers...)
	if err != nil {
		return fmt.Errorf("create provider registry: %w", err)
	}

	repos := resolveStartupRepos(
		context.Background(), cfg, registry, database,
	)
	slog.Debug("startup repos resolved", "count", len(repos))

	cloneMgr := gitclone.New(
		filepath.Join(cfg.DataDir, "clones"), cloneTokens,
	)

	syncer := ghclient.NewSyncerWithRegistry(
		registry, database, cloneMgr, repos,
		cfg.SyncDuration(), rateTrackers, budgets,
	)

	fetchers := make(
		map[string]*ghclient.GraphQLFetcher, len(githubTokens),
	)
	for host, token := range githubTokens {
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
	slog.Debug(
		"server initialized",
		"base_path", cfg.BasePath,
		"worktree_dir", filepath.Join(cfg.DataDir, "worktrees"),
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
	registry *platform.Registry,
	database *db.DB,
) []ghclient.RepoRef {
	seen := make(map[string]struct{})
	repos := make([]ghclient.RepoRef, 0, len(cfg.Repos))
	for _, raw := range cfg.Repos {
		_, expanded, err := ghclient.ResolveConfiguredRepoWithRegistry(
			ctx, registry, raw,
		)
		if err != nil {
			slog.Warn("resolve configured repo", "err", err)
			if raw.HasNameGlob() {
				expanded = fallbackGlobFromDB(
					ctx, database, raw,
				)
			} else {
				expanded = ghclient.FallbackConfiguredRepoRefs(nil, raw)
			}
		}
		for _, repo := range expanded {
			key := string(repoPlatform(repo)) + "\x00" +
				strings.ToLower(repoHost(repo)) + "\x00" +
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

func providerHostKey(platformName, host string) string {
	return strings.ToLower(platformName) + "\x00" + strings.ToLower(host)
}

func splitProviderHostKey(key string) (string, string) {
	platformName, host, ok := strings.Cut(key, "\x00")
	if !ok {
		return key, ""
	}
	return platformName, host
}

func validateProviderHostKeys(providerTokens map[string]string) error {
	hostPlatforms := make(map[string]string, len(providerTokens))
	for key := range providerTokens {
		platformName, host := splitProviderHostKey(key)
		if existing, ok := hostPlatforms[host]; ok && existing != platformName {
			return fmt.Errorf(
				"host %s is configured for both %s and %s; use one provider type per host",
				host, existing, platformName,
			)
		}
		hostPlatforms[host] = platformName
	}
	return nil
}

func repoPlatform(repo ghclient.RepoRef) platform.Kind {
	if repo.Platform != "" {
		return repo.Platform
	}
	return platform.KindGitHub
}

func repoHost(repo ghclient.RepoRef) string {
	if repo.PlatformHost != "" {
		return strings.ToLower(repo.PlatformHost)
	}
	if repoPlatform(repo) == platform.KindGitLab {
		return "gitlab.com"
	}
	return "github.com"
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
	rawPlatform := platform.Kind(raw.PlatformOrDefault())
	host := raw.PlatformHostOrDefault()
	var matches []ghclient.RepoRef
	for _, r := range dbRepos {
		dbPlatform := platform.Kind(r.Platform)
		if dbPlatform == "" {
			dbPlatform = platform.KindGitHub
		}
		dbHost := r.PlatformHost
		if dbHost == "" {
			dbHost = "github.com"
		}
		if dbPlatform != rawPlatform ||
			!strings.EqualFold(dbHost, host) ||
			!strings.EqualFold(r.Owner, raw.Owner) {
			continue
		}
		matched, _ := path.Match(
			strings.ToLower(raw.Name),
			strings.ToLower(r.Name),
		)
		if matched {
			repo := ghclient.RepoRef{
				Owner:        r.Owner,
				Name:         r.Name,
				PlatformHost: dbHost,
			}
			if rawPlatform != platform.KindGitHub {
				repo.Platform = rawPlatform
			}
			matches = append(matches, repo)
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
