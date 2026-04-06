# Per-Repo Platform Host Support

Enable middleman's standalone CLI to monitor repos across github.com and
GitHub Enterprise instances in a single config.

## Constraints and Scope

- **No duplicate owner/name across hosts.** Config validation rejects two repos
  with the same owner+name on different hosts. This keeps the API routes, DB
  lookup helpers, and clone paths unambiguous without requiring host-qualified
  identifiers throughout the stack. The DB schema allows it
  (`UNIQUE(platform, platform_host, owner, name)`) but the config layer
  forbids it.
- **Settings UI does not support `platform_host` or `token_env` yet.** Repos
  added via the settings UI default to github.com with the global token. GHE
  repos must be added by editing the TOML config file.
- **No GHE URL parsing.** `parseGitHubRef` stays github.com-only. Repos with
  `platform_host` must use explicit `owner` and `name` fields.

## Config

Add two optional fields to `config.Repo`:

```toml
[[repos]]
owner = "acme"
name = "widget"
platform_host = "github.mycompany.com"  # default: "github.com"
token_env = "GHE_TOKEN"                 # default: global github_token_env
```

`PlatformHost` defaults to `"github.com"` when empty. `TokenEnv` defaults to
the global `github_token_env` when empty.

**`token_env` is effectively per-host, not per-repo.** All repos on the same
host must resolve to the same token. Config validation rejects two repos on
the same host with different `token_env` values. This matches reality: a
GitHub token is scoped to a host, not a repo.

### Validation

- Fail fast during `Config.Validate()` if any repo's resolved token is empty
- Reject duplicate owner+name pairs across different hosts
- Reject conflicting `token_env` values for repos on the same host
- `parseGitHubRef` stays github.com-only; repos with `platform_host` must use
  explicit owner/name fields (no pasted URLs)

### Save/Load round-trip

`configFile` and `Config.Save()` must preserve `platform_host` and `token_env`
through TOML round-trips. Fields are omitted from TOML when empty (default
values).

## Multi-Client Syncer

### Current design

`Syncer` holds one `Client` and one `platformHost` string. All repos share the
same API client and token.

### New design

`Syncer` holds a `clients map[string]Client` keyed by host. The single
`client` field and `platformHost` field are removed.

```go
type Syncer struct {
    clients      map[string]Client       // host -> client
    db           *db.DB
    clones       *gitclone.Manager
    rateTrackers map[string]*RateTracker // host -> tracker
    repos        []RepoRef
    // ... (rest unchanged)
}
```

`RepoRef` gains a `PlatformHost` field so the syncer knows which client to use
per repo:

```go
type RepoRef struct {
    Owner        string
    Name         string
    PlatformHost string // "github.com" or GHE hostname
}
```

### Constructor

`NewSyncer` accepts the client map and rate tracker map directly. Callers
build both before calling it.

```go
func NewSyncer(
    clients map[string]Client,
    database *db.DB,
    clones *gitclone.Manager,
    repos []RepoRef,
    interval time.Duration,
    rateTrackers map[string]*RateTracker,
) *Syncer
```

The single-client `client Client` parameter, `platformHost string` parameter,
and single `rateTracker *RateTracker` parameter are all replaced by host-keyed
maps.

### Client lookup

A `clientFor(repo RepoRef) Client` helper returns the client for a repo's
host, defaulting to the `"github.com"` entry.

### Sync loop

`RunOnce` groups repos by host, then iterates groups. Within each group, the
same client is reused. Rate backoff checks use the group host's `RateTracker`
from the `rateTrackers` map.

### On-demand sync

`SyncMR` and `SyncItemByNumber` look up the repo in `s.repos` to determine its
host, then use the correct client. If the repo isn't tracked, return an error
(existing behavior). Since config validation forbids duplicate owner/name
across hosts, owner+name is sufficient for lookup.

### Clone host

`cloneHost()` is replaced by per-repo host lookup from `RepoRef.PlatformHost`.

## Clone Manager (`internal/gitclone/`)

### Current design

`Manager` holds one `baseDir` and one `token`. Clones are stored under
`{baseDir}/{owner}/{name}.git`. All git operations authenticate with the
single token.

### Problems with multi-host

1. **Path collision.** Same owner/name on different hosts would share a clone
   directory. Config validation forbids this (see Constraints), but the clone
   paths should still be host-partitioned for correctness.
2. **Wrong credentials.** A GHE repo would authenticate with the github.com
   token, which will fail for private repos.

### New design

Partition clone storage by host and use per-host tokens:

```go
type Manager struct {
    baseDir string
    tokens  map[string]string // host -> token
}

func New(baseDir string, tokens map[string]string) *Manager
```

**Path layout:** `{baseDir}/{host}/{owner}/{name}.git`

`ClonePath(host, owner, name)` gains a `host` parameter. All callers
(`EnsureClone`, `RevParse`, `MergeBase`, `Diff`, `Show`) gain a `host`
parameter or derive it from context.

**Auth:** `git()` takes the host and looks up the token from the `tokens` map.
If no token exists for a host, operations proceed without auth (public repos).

**`SetToken` removal.** The current `SetToken(token)` method is replaced by
the constructor accepting the full token map. The syncer builds the map at
startup alongside the client map.

**Backward compat:** The constructor accepts `map[string]string` -- callers
that only have github.com pass `map[string]string{"github.com": token}`.

## Server Impact

The server currently takes a single `github.Client` for on-demand operations
(merge, comment, review, close, etc.). This needs to become host-aware.

### Approach

Add two lookup methods to `Syncer`:

- `ClientForRepo(owner, name string) (Client, error)` -- returns the client
  for a tracked repo. Used by handlers that operate on a specific repo (merge,
  comment, review, close, mark-ready, sync).
- `ClientForHost(host string) (Client, error)` -- returns the client for a
  host. Used by `handleAddRepo` to validate an untracked repo against the
  GitHub API before adding it to config. The settings UI only adds repos on
  the default host (github.com), so this always uses the `"github.com"` client.

`server.New` and `server.NewWithConfig` drop the `ghClient Client` parameter;
they get clients from the syncer.

### Handlers affected

All handlers that operate on a specific tracked repo call
`s.syncer.ClientForRepo(owner, name)` instead of `s.ghClient`.

`handleAddRepo` calls `s.syncer.ClientForHost("github.com")` to validate the
repo exists before adding it. When the settings UI gains GHE support in a
future change, this would accept the host from the request body.

## CLI Entrypoint (`cmd/middleman/main.go`)

```go
// Collect unique hosts and their tokens
hostTokens := map[string]string{} // host -> token
for _, r := range cfg.Repos {
    host := r.PlatformHostOrDefault()
    if _, seen := hostTokens[host]; seen {
        continue
    }
    hostTokens[host] = r.ResolveToken(cfg.GitHubTokenEnv)
}

// Build per-host rate trackers, clients, and clone token map
rateTrackers := map[string]*RateTracker{}
clients := map[string]Client{}
cloneTokens := map[string]string{}
for host, token := range hostTokens {
    rateTrackers[host] = ghclient.NewRateTracker(database, host)
    c, err := ghclient.NewClient(token, host, rateTrackers[host])
    clients[host] = c
    cloneTokens[host] = token
}

cloneMgr := gitclone.New(cloneDir, cloneTokens)
syncer := ghclient.NewSyncer(
    clients, database, cloneMgr, repos,
    cfg.SyncDuration(), rateTrackers,
)
```

## Embedded Library (`middleman.go`)

### Token resolution rules

- **Single-host configs** (all repos on the same host, or no repos): `Token`
  is sufficient. The library uses it for the sole host.
- **Multi-host configs** (repos on different hosts): `ResolveToken` is
  required. `New()` returns an error if repos span multiple hosts and only
  `Token` is provided, since a single token cannot authenticate against
  multiple hosts.
- When `ResolveToken` is set, it is called once per unique host. `Token` is
  ignored.

### Client map construction

`New()` collects unique hosts from `opts.Repos`, resolves tokens per host
(via `ResolveToken` or single `Token`), and builds the `clients`,
`rateTrackers`, and `cloneTokens` maps the same way the CLI does. The
resulting maps are passed to `NewSyncer` and `gitclone.New`.

## Rate Tracking

`RateTracker` is already bound to a single host (keyed by `platformHost` in
`middleman_rate_limits`). No schema changes needed.

The Syncer holds `map[string]*RateTracker` keyed by host. Each client gets its
own tracker at construction time. Multiple hosts create separate rate limit
rows in the DB and track backoff independently.

The CLI and embedded library both build the tracker map alongside the client
map:

```go
rateTrackers := map[string]*RateTracker{}
for host := range hostTokens {
    rateTrackers[host] = ghclient.NewRateTracker(database, host)
}
```

## Test Plan

### Config tests (`internal/config/`)
- Parse repos with `platform_host` and `token_env` fields
- Parse repos without those fields (defaults to github.com / global token)
- Round-trip save/load preserves platform_host and token_env
- Validation: repo with platform_host but no resolvable token fails
- Validation: duplicate owner+name across different hosts is rejected
- Validation: conflicting token_env for same host is rejected

### Clone manager tests (`internal/gitclone/`)
- `ClonePath` includes host in path (`{baseDir}/{host}/{owner}/{name}.git`)
- `git()` uses correct per-host token for auth
- Missing host token results in unauthenticated git operations
- Single-host backward compat with `map[string]string{"github.com": token}`

### Syncer tests (`internal/github/`)
- Multi-host client dispatch: two hosts, verify correct client called per repo
- Single-host backward compat: single-entry maps work
- `clientFor` returns correct client; defaults to github.com for empty host
- `ClientForRepo` returns error for untracked repo
- On-demand SyncMR dispatches to correct host's client
- Rate backoff uses the correct host's tracker

### Server tests (`internal/server/`)
- Handlers use `ClientForRepo` instead of direct client
- On-demand operations route to correct client
- `handleAddRepo` validates via `ClientForHost("github.com")`

### Embedded library tests (`middleman_test.go`)
- Multi-host config with `ResolveToken` builds correct client map
- Multi-host config with only `Token` (no `ResolveToken`) returns error
- Single-host config with `Token` works (backward compat)

### Rate tracker tests (`internal/github/`)
- Multiple hosts tracked independently
- Hour rollover boundary test
- Concurrent access (race detector)

### Existing tests
- All pass with single-entry client/tracker maps (backward compatible)
