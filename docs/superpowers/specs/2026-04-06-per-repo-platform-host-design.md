# Per-Repo Platform Host Support

Enable middleman's standalone CLI to monitor repos across github.com and
GitHub Enterprise instances in a single config.

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
the global `github_token_env` when empty. No URL parsing for GHE hosts --
users must specify explicit `owner` and `name`.

### Validation

- Fail fast during `Config.Validate()` if any repo's resolved token is empty
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
    clients      map[string]Client  // host -> client
    db           *db.DB
    clones       *gitclone.Manager
    rateTracker  *RateTracker
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

`NewSyncer` accepts the client map directly. Callers build clients before
calling it.

```go
func NewSyncer(
    clients map[string]Client,
    database *db.DB,
    clones *gitclone.Manager,
    repos []RepoRef,
    interval time.Duration,
    rateTracker *RateTracker,
) *Syncer
```

The single-client `client Client` parameter and `platformHost string` parameter
are replaced by `clients map[string]Client`.

### Client lookup

A `clientFor(repo RepoRef) Client` helper returns the client for a repo's
host, defaulting to the `"github.com"` entry.

### Sync loop

`RunOnce` groups repos by host, then iterates groups. Within each group, the
same client is reused. Rate backoff checks use the group's host.

### On-demand sync

`SyncMR` and `SyncItemByNumber` look up the repo in `s.repos` to determine its
host, then use the correct client. If the repo isn't tracked, return an error
(existing behavior).

### Clone host

`cloneHost()` is replaced by per-repo host lookup from `RepoRef.PlatformHost`.

## Server Impact

The server currently takes a single `github.Client` for on-demand operations
(merge, comment, review, close, etc.). This needs to become host-aware.

### Approach

Add a `ClientForRepo(owner, name string) Client` method to `Syncer` that
returns the appropriate client. The server calls this instead of holding its
own client reference.

`server.New` and `server.NewWithConfig` drop the `ghClient Client` parameter;
they get clients from the syncer.

### Handlers affected

All handlers that use the client directly (merge, comment, review, close,
mark-ready, sync) call `s.syncer.ClientForRepo(owner, name)` instead of
`s.ghClient`.

## CLI Entrypoint (`cmd/middleman/main.go`)

```go
// Build per-host clients
hostTokens := map[string]string{} // host -> token
for _, r := range cfg.Repos {
    host := r.PlatformHostOrDefault()
    if _, seen := hostTokens[host]; seen {
        continue
    }
    hostTokens[host] = r.ResolveToken(cfg.GitHubTokenEnv)
}

clients := map[string]Client{}
for host, token := range hostTokens {
    c, err := ghclient.NewClient(token, host, rt)
    clients[host] = c
}

syncer := ghclient.NewSyncer(clients, database, cloneMgr, repos, ...)
```

## Embedded Library (`middleman.go`)

The `Options` struct already supports `ResolveToken func(host string) string`.
Adapt to build the client map using this function, grouping repos by host.

## Rate Tracking

Already per-host (keyed by `platformHost` in `middleman_rate_limits`). No
schema changes needed. Multiple hosts create separate rate limit rows.

## Test Plan

### Config tests (`internal/config/`)
- Parse repos with `platform_host` and `token_env` fields
- Parse repos without those fields (defaults to github.com / global token)
- Round-trip save/load preserves platform_host and token_env
- Validation: repo with platform_host but no resolvable token fails

### Syncer tests (`internal/github/`)
- Multi-host client dispatch: two hosts, verify correct client called per repo
- Single-host backward compat: nil or single-entry map works
- `clientFor` returns correct client; defaults to github.com for empty host
- `ClientForRepo` returns error for untracked repo
- On-demand SyncMR dispatches to correct host's client

### Server tests (`internal/server/`)
- Handlers use `ClientForRepo` instead of direct client
- On-demand operations route to correct client

### Rate tracker tests (`internal/github/`)
- Multiple hosts tracked independently
- Hour rollover boundary test
- Concurrent access (race detector)

### Existing tests
- All pass with single-entry client map (backward compatible)
