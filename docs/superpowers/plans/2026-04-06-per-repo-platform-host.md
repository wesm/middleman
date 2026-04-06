# Per-Repo Platform Host Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable middleman to monitor repos across github.com and GitHub Enterprise instances via per-repo `platform_host` and `token_env` config fields, with host-partitioned clones, per-host clients, and per-host rate tracking.

**Architecture:** Config gains per-repo `platform_host` and `token_env` fields (effectively per-host). The Syncer holds `map[string]Client` and `map[string]*RateTracker` keyed by host. The clone manager stores clones under `{host}/{owner}/{name}.git` with per-host tokens. The server drops its direct `ghClient` and uses `syncer.ClientForRepo()`/`ClientForHost()`.

**Tech Stack:** Go, SQLite, go-github/v84, TOML config

**Spec:** `docs/superpowers/specs/2026-04-06-per-repo-platform-host-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/config/config.go` | Modify | Add `PlatformHost`, `TokenEnv` to `Repo`; validation for duplicates and conflicting tokens |
| `internal/config/config_test.go` | Modify | Tests for new fields, round-trip, validation |
| `internal/gitclone/clone.go` | Modify | Per-host token map, host-partitioned paths, remove `SetToken` |
| `internal/gitclone/clone_test.go` | Modify | Update `ClonePath` tests for host param |
| `internal/gitclone/diff.go` | Modify | Add `host` param to `Diff` |
| `internal/gitclone/diff_test.go` | Modify | Update callers |
| `internal/github/sync.go` | Modify | Multi-client Syncer, `ClientForRepo`, `ClientForHost` |
| `internal/github/sync_test.go` | Modify | Multi-host dispatch tests, update all `NewSyncer` callers |
| `internal/github/merged_diff_test.go` | Modify | Update `NewSyncer` callers |
| `internal/server/server.go` | Modify | Drop `gh` field and parameter from `New`/`NewWithConfig` |
| `internal/server/huma_routes.go` | Modify | Replace `s.gh.*` with `s.syncer.ClientForRepo()` |
| `internal/server/settings_handlers.go` | Modify | Use `s.syncer.ClientForHost("github.com")` |
| `internal/server/api_test.go` | Modify | Update `New`/`NewWithConfig` callers, mock setup |
| `internal/server/basepath_test.go` | Modify | Update callers |
| `internal/server/embedded_test.go` | Modify | Update callers |
| `internal/server/settings_test.go` | Modify | Update callers |
| `cmd/middleman/main.go` | Modify | Build per-host client/tracker/token maps |
| `middleman.go` | Modify | Multi-host token resolution, client map construction |
| `middleman_test.go` | Modify | Multi-host tests, Token-only rejection for multi-host |

---

### Task 1: Config — Add PlatformHost and TokenEnv to Repo

**Files:**
- Modify: `internal/config/config.go:29-32` (Repo struct)
- Modify: `internal/config/config.go:303-353` (Validate)
- Modify: `internal/config/config.go:397-406` (configFile)
- Modify: `internal/config/config.go:409-431` (Save)
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for new config fields**

Add to `internal/config/config_test.go`:

```go
func TestLoadRepoPlatformHost(t *testing.T) {
	assert := assert.New(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "acme"
name = "widget"
platform_host = "github.mycompany.com"
token_env = "GHE_TOKEN"

[[repos]]
owner = "acme"
name = "lib"
`), 0o644))

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	assert.Equal("github.mycompany.com", cfg.Repos[0].PlatformHost)
	assert.Equal("GHE_TOKEN", cfg.Repos[0].TokenEnv)
	assert.Equal("", cfg.Repos[1].PlatformHost)
	assert.Equal("", cfg.Repos[1].TokenEnv)
}

func TestRepoPlatformHostOrDefault(t *testing.T) {
	assert := assert.New(t)
	r := Repo{Owner: "a", Name: "b"}
	assert.Equal("github.com", r.PlatformHostOrDefault())

	r.PlatformHost = "ghe.corp.com"
	assert.Equal("ghe.corp.com", r.PlatformHostOrDefault())
}

func TestRepoResolveToken(t *testing.T) {
	assert := assert.New(t)

	r := Repo{Owner: "a", Name: "b"}
	assert.Equal("fallback-val", r.ResolveToken("fallback-val"))

	r.TokenEnv = "MY_GHE_TOKEN"
	t.Setenv("MY_GHE_TOKEN", "ghe-secret")
	assert.Equal("ghe-secret", r.ResolveToken("fallback-val"))
}

func TestValidateRejectsDuplicateOwnerName(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "acme"
name = "widget"

[[repos]]
owner = "acme"
name = "widget"
platform_host = "ghe.corp.com"
`), 0o644))

	_, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValidateRejectsConflictingTokenEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "acme"
name = "widget"
platform_host = "ghe.corp.com"
token_env = "GHE_TOKEN_A"

[[repos]]
owner = "acme"
name = "lib"
platform_host = "ghe.corp.com"
token_env = "GHE_TOKEN_B"
`), 0o644))

	_, err := Load(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicting")
}

func TestSaveRoundTripPlatformHost(t *testing.T) {
	assert := assert.New(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")

	cfg := &Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "MIDDLEMAN_GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8090,
		BasePath:       "/",
		DataDir:        dir,
		Activity:       Activity{ViewMode: "threaded", TimeRange: "7d"},
		Repos: []Repo{
			{Owner: "acme", Name: "widget",
				PlatformHost: "ghe.corp.com", TokenEnv: "GHE_TOKEN"},
			{Owner: "acme", Name: "lib"},
		},
	}
	require.NoError(t, cfg.Save(cfgPath))

	loaded, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal("ghe.corp.com", loaded.Repos[0].PlatformHost)
	assert.Equal("GHE_TOKEN", loaded.Repos[0].TokenEnv)
	assert.Equal("", loaded.Repos[1].PlatformHost)
	assert.Equal("", loaded.Repos[1].TokenEnv)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run 'TestLoadRepoPlatformHost|TestRepoPlatformHostOrDefault|TestRepoResolveToken|TestValidateRejectsDuplicate|TestValidateRejectsConflicting|TestSaveRoundTripPlatformHost' -v`
Expected: FAIL — fields don't exist yet

- [ ] **Step 3: Implement config changes**

In `internal/config/config.go`, update the `Repo` struct (line 29):

```go
type Repo struct {
	Owner        string `toml:"owner" json:"owner"`
	Name         string `toml:"name" json:"name"`
	PlatformHost string `toml:"platform_host,omitempty" json:"platform_host,omitempty"`
	TokenEnv     string `toml:"token_env,omitempty" json:"token_env,omitempty"`
}
```

Add helper methods after `FullName()` (after line 36):

```go
// PlatformHostOrDefault returns the platform host, defaulting to
// "github.com" when empty.
func (r Repo) PlatformHostOrDefault() string {
	if r.PlatformHost == "" {
		return "github.com"
	}
	return r.PlatformHost
}

// ResolveToken returns the token for this repo. If TokenEnv is set,
// reads that env var. Otherwise returns globalToken.
func (r Repo) ResolveToken(globalToken string) string {
	if r.TokenEnv != "" {
		return os.Getenv(r.TokenEnv)
	}
	return globalToken
}
```

In `Validate()` (after the repos normalize loop at line 308), add:

```go
	// Reject duplicate owner+name across different hosts.
	seen := map[string]bool{}
	for _, r := range c.Repos {
		key := r.Owner + "/" + r.Name
		if seen[key] {
			return fmt.Errorf(
				"config: duplicate repo %s", key,
			)
		}
		seen[key] = true
	}

	// Reject conflicting token_env for repos on the same host.
	hostTokenEnv := map[string]string{}
	for _, r := range c.Repos {
		host := r.PlatformHostOrDefault()
		if prev, ok := hostTokenEnv[host]; ok {
			if prev != r.TokenEnv {
				return fmt.Errorf(
					"config: conflicting token_env for host %s: %q vs %q",
					host, prev, r.TokenEnv,
				)
			}
		} else {
			hostTokenEnv[host] = r.TokenEnv
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add platform_host and token_env to config.Repo with validation"
```

---

### Task 2: Clone Manager — Per-Host Tokens and Host-Partitioned Paths

**Files:**
- Modify: `internal/gitclone/clone.go:20-28` (Manager struct, New)
- Modify: `internal/gitclone/clone.go:31-33` (remove SetToken)
- Modify: `internal/gitclone/clone.go:36-38` (ClonePath — add host param)
- Modify: `internal/gitclone/clone.go:43-51` (EnsureClone — add host param)
- Modify: `internal/gitclone/clone.go:103-120` (RevParse, MergeBase — add host param)
- Modify: `internal/gitclone/clone.go:123-152` (git — accept host for token lookup)
- Modify: `internal/gitclone/diff.go:10` (Diff — add host param)
- Test: `internal/gitclone/clone_test.go`
- Test: `internal/gitclone/diff_test.go`

- [ ] **Step 1: Write failing test for host-partitioned ClonePath**

Add to `internal/gitclone/clone_test.go`:

```go
func TestClonePathIncludesHost(t *testing.T) {
	m := New("/base", map[string]string{"github.com": "tok"})
	got := m.ClonePath("github.com", "owner", "repo")
	assert.Equal(t, "/base/github.com/owner/repo.git", got)

	got = m.ClonePath("ghe.corp.com", "org", "lib")
	assert.Equal(t, "/base/ghe.corp.com/org/lib.git", got)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitclone/... -run TestClonePathIncludesHost -v`
Expected: FAIL — signature mismatch

- [ ] **Step 3: Update Manager struct, New, and ClonePath**

In `internal/gitclone/clone.go`, replace Manager struct and New (lines 20-28):

```go
// Manager manages bare git clones for diff computation.
type Manager struct {
	baseDir string            // directory to store clones
	tokens  map[string]string // host -> token
}

// New creates a Manager that stores bare clones under baseDir.
// tokens maps platform host to auth token (may be empty for
// public repos).
func New(baseDir string, tokens map[string]string) *Manager {
	return &Manager{baseDir: baseDir, tokens: tokens}
}
```

Remove `SetToken` (lines 31-33). Update `ClonePath` (line 36):

```go
// ClonePath returns the filesystem path for a repo's bare clone.
func (m *Manager) ClonePath(host, owner, name string) string {
	return filepath.Join(m.baseDir, host, owner, name+".git")
}
```

- [ ] **Step 4: Update git() to use per-host token**

Replace the `git()` method (line 123) to accept a host parameter:

```go
func (m *Manager) git(ctx context.Context, host, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
	)
	token := m.tokens[host]
	if token != "" {
		cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		cmd.Env = append(cmd.Env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+cred,
		)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := stderr.String()
		if isNotFoundError(msg) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, msg)
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	return out, nil
}
```

- [ ] **Step 5: Update all Manager methods to pass host through**

Update `EnsureClone` (line 43):

```go
func (m *Manager) EnsureClone(ctx context.Context, host, owner, name, remoteURL string) error {
	clonePath := m.ClonePath(host, owner, name)
	if _, err := os.Stat(filepath.Join(clonePath, "HEAD")); os.IsNotExist(err) {
		return m.cloneBare(ctx, host, clonePath, remoteURL)
	}
	m.ensurePullRefspec(ctx, host, clonePath)
	return m.fetch(ctx, host, clonePath)
}
```

Update `ensurePullRefspec` (line 57):

```go
func (m *Manager) ensurePullRefspec(ctx context.Context, host, clonePath string) {
	out, err := m.git(ctx, host, clonePath, "config", "--get-all", "remote.origin.fetch")
```

Update `cloneBare` (line 70):

```go
func (m *Manager) cloneBare(ctx context.Context, host, clonePath, remoteURL string) error {
	// ... (mkdir unchanged)
	_, err := m.git(ctx, host, "", "clone", "--bare", remoteURL, clonePath)
	// ...
	_, err = m.git(ctx, host, clonePath, "config", "--add", "remote.origin.fetch", pullRefspec)
	// ...
	return m.fetch(ctx, host, clonePath)
}
```

Update `fetch` (line 93):

```go
func (m *Manager) fetch(ctx context.Context, host, clonePath string) error {
	_, err := m.git(ctx, host, clonePath, "fetch", "--prune", "origin")
```

Update `RevParse` (line 103):

```go
func (m *Manager) RevParse(ctx context.Context, host, owner, name, ref string) (string, error) {
	clonePath := m.ClonePath(host, owner, name)
	out, err := m.git(ctx, host, clonePath, "rev-parse", "--verify", ref)
```

Update `MergeBase` (line 113):

```go
func (m *Manager) MergeBase(ctx context.Context, host, owner, name, sha1, sha2 string) (string, error) {
	clonePath := m.ClonePath(host, owner, name)
	out, err := m.git(ctx, host, clonePath, "merge-base", sha1, sha2)
```

Update `Diff` in `internal/gitclone/diff.go` (line 10):

```go
func (m *Manager) Diff(ctx context.Context, host, owner, name, mergeBase, headSHA string, hideWhitespace bool) (*DiffResult, error) {
	clonePath := m.ClonePath(host, owner, name)
	// update internal m.git calls to pass host
```

Update `computeWhitespaceOnlyCount` and `getWhitespaceOnlyFiles` similarly to pass `host` through their `m.git()` calls.

- [ ] **Step 6: Fix all existing clone_test.go and diff_test.go callers**

Update every `New(dir, "token")` to `New(dir, map[string]string{"github.com": "token"})` and every `ClonePath(owner, name)` to `ClonePath("github.com", owner, name)`, etc. Use find-and-replace for the pattern. Update `EnsureClone`, `RevParse`, `MergeBase`, `Diff` calls to include `"github.com"` as the first context arg.

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/gitclone/... -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```
git add internal/gitclone/
git commit -m "Make clone manager host-aware with per-host tokens and paths"
```

---

### Task 3: Syncer — Multi-Client and Multi-RateTracker

**Files:**
- Modify: `internal/github/sync.go:26-73` (RepoRef, Syncer struct, NewSyncer)
- Modify: `internal/github/sync.go:76-81` (remove cloneHost, add clientFor)
- Modify: `internal/github/sync.go:124+` (RunOnce — use clientFor)
- Modify: `internal/github/sync.go:182+` (syncRepo — use clientFor)
- Modify: `internal/github/sync.go:232+` (doSyncRepo — use clientFor)
- Modify: `internal/github/sync.go:690+` (SyncMR — use clientFor)
- Modify: `internal/github/sync.go:791+` (SyncItemByNumber — use clientFor)
- Modify: `internal/github/sync.go:825+` (fetchAndUpdateClosed — use clientFor)
- Test: `internal/github/sync_test.go`

- [ ] **Step 1: Write failing test for multi-client dispatch**

Add to `internal/github/sync_test.go`:

```go
func TestSyncerMultiHostClientDispatch(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	ghMock := &mockClient{
		openPRs: []*gh.PullRequest{},
	}
	gheMock := &mockClient{
		openPRs: []*gh.PullRequest{},
	}

	clients := map[string]Client{
		"github.com":    ghMock,
		"ghe.corp.com":  gheMock,
	}

	repos := []RepoRef{
		{Owner: "pub", Name: "repo", PlatformHost: "github.com"},
		{Owner: "priv", Name: "repo", PlatformHost: "ghe.corp.com"},
	}

	syncer := NewSyncer(clients, d, nil, repos, time.Minute, nil)
	syncer.RunOnce(ctx)

	// Both mocks should have been called for their respective repos.
	require.True(ghMock.listOpenPRsCalled, "github.com client should be called")
	require.True(gheMock.listOpenPRsCalled, "ghe.corp.com client should be called")
}
```

(The `mockClient` will need a `listOpenPRsCalled bool` field — add it to the existing mock struct.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github/... -run TestSyncerMultiHostClientDispatch -v`
Expected: FAIL — NewSyncer signature mismatch

- [ ] **Step 3: Update RepoRef, Syncer struct, and NewSyncer**

In `internal/github/sync.go`, update `RepoRef` (line 26):

```go
type RepoRef struct {
	Owner        string
	Name         string
	PlatformHost string // "github.com" or GHE hostname
}
```

Update `Syncer` struct (line 32):

```go
type Syncer struct {
	clients      map[string]Client
	db           *db.DB
	clones       *gitclone.Manager
	rateTrackers map[string]*RateTracker
	repos        []RepoRef
	reposMu      sync.Mutex
	interval     time.Duration
	running      atomic.Bool
	status       atomic.Value
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup
	displayNames map[string]string
}
```

Update `NewSyncer` (line 52):

```go
func NewSyncer(
	clients map[string]Client,
	database *db.DB,
	clones *gitclone.Manager,
	repos []RepoRef,
	interval time.Duration,
	rateTrackers map[string]*RateTracker,
) *Syncer {
	s := &Syncer{
		clients:      clients,
		db:           database,
		clones:       clones,
		rateTrackers: rateTrackers,
		repos:        repos,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
	s.status.Store(&SyncStatus{})
	return s
}
```

- [ ] **Step 4: Add clientFor, ClientForRepo, ClientForHost, and hostFor helpers**

Replace `cloneHost()` with:

```go
// clientFor returns the API client for a repo's host.
func (s *Syncer) clientFor(repo RepoRef) Client {
	host := repo.PlatformHost
	if host == "" {
		host = "github.com"
	}
	if c, ok := s.clients[host]; ok {
		return c
	}
	return s.clients["github.com"]
}

// ClientForRepo returns the API client for a tracked repo.
func (s *Syncer) ClientForRepo(
	owner, name string,
) (Client, error) {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()
	for _, r := range s.repos {
		if r.Owner == owner && r.Name == name {
			return s.clientFor(r), nil
		}
	}
	return nil, fmt.Errorf(
		"repo %s/%s is not tracked", owner, name,
	)
}

// ClientForHost returns the API client for a platform host.
func (s *Syncer) ClientForHost(
	host string,
) (Client, error) {
	if c, ok := s.clients[host]; ok {
		return c, nil
	}
	return nil, fmt.Errorf(
		"no client configured for host %s", host,
	)
}

// hostFor returns the platform host for a tracked repo.
func (s *Syncer) hostFor(owner, name string) string {
	for _, r := range s.repos {
		if r.Owner == owner && r.Name == name {
			if r.PlatformHost != "" {
				return r.PlatformHost
			}
			return "github.com"
		}
	}
	return "github.com"
}
```

- [ ] **Step 5: Update all s.client references to use clientFor**

In `syncRepo` (line 188): replace `s.client.GetRepository(ctx, repo.Owner, repo.Name)` with `s.clientFor(repo).GetRepository(ctx, repo.Owner, repo.Name)`

In `syncRepo` (line 208-209): replace `s.cloneHost()` with `repo.PlatformHost` (or helper), and update `s.clones.EnsureClone(ctx, repo.Owner, repo.Name, remoteURL)` to `s.clones.EnsureClone(ctx, repo.PlatformHost, repo.Owner, repo.Name, remoteURL)`.

In `doSyncRepo` (line 233): replace `s.client.ListOpenPullRequests` with `s.clientFor(repo).ListOpenPullRequests`.

Apply the same pattern to every `s.client.*` call in `syncOpenMR`, `refreshTimeline`, `refreshCIStatus`, `fetchAndUpdateClosed`, `SyncMR`, `SyncIssue`, `SyncItemByNumber`, and `computeMergedMRDiffSHAs`.

For `SyncMR` and `SyncItemByNumber` which take `owner, name` not a `RepoRef`, construct the ref: `repo := RepoRef{Owner: owner, Name: name, PlatformHost: s.hostFor(owner, name)}` and use `s.clientFor(repo)`.

Update all `s.clones.*` calls to pass the host: `s.clones.EnsureClone(ctx, host, owner, name, ...)`, `s.clones.MergeBase(ctx, host, owner, name, ...)`, `s.clones.RevParse(ctx, host, owner, name, ...)`.

Update clone URL construction from `s.cloneHost()` to use the repo's `PlatformHost` (defaulting to `"github.com"`).

- [ ] **Step 6: Update RunOnce rate backoff to be per-host**

In `RunOnce` (around line 126), replace the single `s.rateTracker` backoff check with a per-host check using the current repo's host:

```go
	for i, repo := range repos {
		host := repo.PlatformHost
		if host == "" {
			host = "github.com"
		}
		if rt := s.rateTrackers[host]; rt != nil {
			if backoff, wait := rt.ShouldBackoff(); backoff {
				// ... existing backoff logic
			}
		}
```

- [ ] **Step 7: Fix all NewSyncer callers in sync_test.go and merged_diff_test.go**

Every `NewSyncer(mc, d, nil, repos, time.Minute, nil, "")` becomes `NewSyncer(map[string]Client{"github.com": mc}, d, nil, repos, time.Minute, nil)`.

Every `RepoRef{Owner: "o", Name: "r"}` becomes `RepoRef{Owner: "o", Name: "r", PlatformHost: "github.com"}`.

Update `setupSyncer` in `merged_diff_test.go` to pass `map[string]Client{"github.com": mc}` and the host in `EnsureClone`/`MergeBase`/`RevParse` calls.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/github/... -count=1 -v`
Expected: All PASS

- [ ] **Step 9: Commit**

```
git add internal/github/sync.go internal/github/sync_test.go internal/github/merged_diff_test.go
git commit -m "Make Syncer multi-client with per-host rate tracking"
```

---

### Task 4: Server — Drop ghClient, Use Syncer Client Lookup

**Files:**
- Modify: `internal/server/server.go:48-60` (Server struct — remove `gh` field)
- Modify: `internal/server/server.go:68-117` (New, NewWithConfig, newServer — remove `gh` param)
- Modify: `internal/server/huma_routes.go` (replace all `s.gh.*` with `s.syncer.ClientForRepo()`)
- Modify: `internal/server/settings_handlers.go:122` (use `s.syncer.ClientForHost`)
- Test: `internal/server/api_test.go`
- Test: `internal/server/basepath_test.go`
- Test: `internal/server/embedded_test.go`
- Test: `internal/server/settings_test.go`

- [ ] **Step 1: Remove `gh` field from Server and update constructors**

In `internal/server/server.go`, remove `gh ghclient.Client` from the `Server` struct (line 50), from `New()` params (line 68), from `NewWithConfig()` params (line 85), and from `newServer()` params (line 103). Remove the `gh: gh` assignment in `newServer` (line 117).

- [ ] **Step 2: Replace all s.gh.* calls in huma_routes.go**

For each `s.gh.*` call, extract owner/name from the handler's input, then:

```go
client, err := s.syncer.ClientForRepo(input.Owner, input.Name)
if err != nil {
    return nil, huma.Error404NotFound(err.Error())
}
client.CreateIssueComment(ctx, ...)
```

Apply to all 9 `s.gh.*` calls identified in huma_routes.go (lines 423, 524, 574, 590, 612, 678, 690, 762, 775).

- [ ] **Step 3: Update handleAddRepo to use ClientForHost**

In `internal/server/settings_handlers.go` (line 122), replace:

```go
if _, err := s.gh.GetRepository(
    r.Context(), body.Owner, body.Name,
); err != nil {
```

with:

```go
ghClient, clientErr := s.syncer.ClientForHost("github.com")
if clientErr != nil {
    writeError(w, http.StatusServiceUnavailable,
        "no GitHub client available")
    return
}
if _, err := ghClient.GetRepository(
    r.Context(), body.Owner, body.Name,
); err != nil {
```

- [ ] **Step 4: Update s.clones.Diff call in getDiff to pass host**

In `huma_routes.go` `getDiff` (line 1105), the Diff call needs a host for the clone path. Look it up from the syncer:

```go
host := s.syncer.HostForRepo(input.Owner, input.Name)
result, err := s.clones.Diff(ctx, host, input.Owner, input.Name, ...)
```

`HostForRepo` is a public wrapper around `hostFor` (added in Task 3 Step 4). Add it alongside `ClientForRepo`:

```go
// HostForRepo returns the platform host for a tracked repo,
// defaulting to "github.com" if not found.
func (s *Syncer) HostForRepo(owner, name string) string {
	s.reposMu.Lock()
	defer s.reposMu.Unlock()
	return s.hostFor(owner, name)
}
```

- [ ] **Step 5: Fix all server test constructors**

In `api_test.go`, `basepath_test.go`, `embedded_test.go`, `settings_test.go`:
- Remove the `mock` (mockGH client) parameter from `New()` and `NewWithConfig()` calls
- Instead, set up the mock client in the syncer's client map:

```go
clients := map[string]ghclient.Client{"github.com": mock}
syncer := ghclient.NewSyncer(clients, database, nil, nil, time.Minute, nil)
srv := New(database, syncer, frontend, basePath, cfg, opts)
```

Also update `mockGH` references — the server no longer holds a client directly, so mocks must be in the syncer's client map.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/server/... -count=1 -v`
Expected: All PASS

- [ ] **Step 7: Commit**

```
git add internal/server/
git commit -m "Remove direct ghClient from server, use syncer client lookup"
```

---

### Task 5: CLI Entrypoint — Per-Host Client Map Construction

**Files:**
- Modify: `cmd/middleman/main.go`

- [ ] **Step 1: Update run() to build per-host maps**

Replace the single-client construction in `cmd/middleman/main.go` (lines 81-100) with:

```go
	globalToken := cfg.GitHubToken()
	if globalToken == "" {
		return fmt.Errorf(
			"GitHub token not set: env var %q is empty",
			cfg.GitHubTokenEnv,
		)
	}

	// ... (database open unchanged) ...

	// Always seed github.com from the global token.
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

	rateTrackers := make(map[string]*ghclient.RateTracker, len(hostTokens))
	clients := make(map[string]ghclient.Client, len(hostTokens))
	cloneTokens := make(map[string]string, len(hostTokens))
	for host, token := range hostTokens {
		rateTrackers[host] = ghclient.NewRateTracker(database, host)
		c, err := ghclient.NewClient(token, host, rateTrackers[host])
		if err != nil {
			return fmt.Errorf("create client for %s: %w", host, err)
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
```

Update `server.NewWithConfig` call to remove the `ghClient` parameter.

- [ ] **Step 2: Build and verify**

Run: `go build ./cmd/middleman/...`
Expected: Compiles

- [ ] **Step 3: Run full test suite**

Run: `go test ./... -short -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```
git add cmd/middleman/main.go
git commit -m "Build per-host client and rate tracker maps in CLI entrypoint"
```

---

### Task 6: Embedded Library — Multi-Host Token Resolution

**Files:**
- Modify: `middleman.go:93-184` (New function)
- Test: `middleman_test.go`

- [ ] **Step 1: Write failing tests**

Add to `middleman_test.go`:

```go
func TestNewMultiHostRequiresResolveToken(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Options{
		Token:   "tok",
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b", PlatformHost: "github.com"},
			{Owner: "c", Name: "d", PlatformHost: "ghe.corp.com"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ResolveToken")
}

func TestNewMultiHostWithResolveToken(t *testing.T) {
	dir := t.TempDir()
	inst, err := New(Options{
		ResolveToken: func(_ context.Context, host string) (string, error) {
			return "token-for-" + host, nil
		},
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b", PlatformHost: "github.com"},
			{Owner: "c", Name: "d", PlatformHost: "ghe.corp.com"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inst)
	inst.Close()
}

func TestNewSingleHostWithTokenStillWorks(t *testing.T) {
	dir := t.TempDir()
	inst, err := New(Options{
		Token:   "tok",
		DataDir: dir,
		Repos: []Repo{
			{Owner: "a", Name: "b"},
			{Owner: "c", Name: "d"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inst)
	inst.Close()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestNewMultiHost|TestNewSingleHostWithToken' -v`
Expected: FAIL

- [ ] **Step 3: Rewrite New() for multi-host support**

In `middleman.go`, rewrite the token resolution and client construction in `New()` to:

1. Collect unique hosts from `opts.Repos`
2. If multiple hosts and no `ResolveToken`, return error
3. Build `hostTokens` map (single token for single-host, ResolveToken for multi-host)
4. Build `clients`, `rateTrackers`, `cloneTokens` maps
5. Pass maps to `NewSyncer` and `gitclone.New`
6. Build `RepoRef` slice with `PlatformHost`
7. Pass syncer (not client) to `server.New`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -short -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```
git add middleman.go middleman_test.go
git commit -m "Support multi-host token resolution in embedded library"
```

---

### Task 7: Update e2e-server and Final Integration

**Files:**
- Modify: `cmd/e2e-server/main.go`

- [ ] **Step 1: Update e2e-server for new signatures**

Update `cmd/e2e-server/main.go` to use the new `NewSyncer(clients map, ...)` and `gitclone.New(dir, tokens map)` signatures, and remove the client parameter from `server.New`/`server.NewWithConfig`.

- [ ] **Step 2: Run full test suite with race detector**

Run: `go test -race ./... -short -count=1`
Expected: All PASS, no races

- [ ] **Step 3: Build all binaries**

Run: `go build ./...`
Expected: Clean build

- [ ] **Step 4: Commit**

```
git add cmd/e2e-server/main.go
git commit -m "Update e2e-server for multi-client syncer signatures"
```

---

### Task 8: Rate Tracker — Additional Test Coverage

**Files:**
- Modify: `internal/github/rate_test.go`

- [ ] **Step 1: Add hour-rollover boundary test**

```go
func TestRateTrackerHourRollover(t *testing.T) {
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com")

	// Record requests, then advance past the hour boundary.
	for i := 0; i < 5; i++ {
		rt.RecordRequest()
	}
	assert.Equal(t, 5, rt.RequestsThisHour())

	// Simulate hour rollover by manipulating internal state.
	rt.mu.Lock()
	rt.hourStart = time.Now().Add(-2 * time.Hour)
	rt.mu.Unlock()

	rt.RecordRequest()
	assert.Equal(t, 1, rt.RequestsThisHour(),
		"counter should reset after hour boundary")
}
```

- [ ] **Step 2: Add concurrent access test**

```go
func TestRateTrackerConcurrentAccess(t *testing.T) {
	d := openTestDB(t)
	rt := NewRateTracker(d, "github.com")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt.RecordRequest()
			rt.RequestsThisHour()
			rt.Remaining()
			rt.ShouldBackoff()
		}()
	}
	wg.Wait()
	assert.GreaterOrEqual(t, rt.RequestsThisHour(), 1)
}
```

- [ ] **Step 3: Run with race detector**

Run: `go test -race ./internal/github/... -run 'TestRateTracker' -v`
Expected: PASS, no races

- [ ] **Step 4: Commit**

```
git add internal/github/rate_test.go
git commit -m "Add rate tracker hour-rollover and concurrent access tests"
```
