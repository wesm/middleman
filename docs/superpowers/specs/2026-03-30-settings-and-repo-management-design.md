# Settings Page, Repo Management, and Activity Defaults

## Overview

Add a settings page for managing repositories and activity feed preferences through the UI, with changes persisted to `config.toml`. Fix activity feed state loss when navigating between views.

## Config Changes

New TOML structure in `config.toml`:

```toml
# Existing fields remain unchanged
sync_interval = "5m"
host = "127.0.0.1"
port = 8090

[[repos]]
owner = "apache"
name = "arrow"

# New section
[activity]
view_mode = "flat"         # "flat" or "threaded"
time_range = "7d"          # "24h", "7d", "30d", "90d"
hide_closed = false        # hide merged/closed items by default
hide_bots = false          # hide bot activity by default
```

### Config package (`internal/config/`)

- Add `Activity` struct with the four fields above, embedded in `Config`
- Add `Save(path string) error` method that writes the full config back to TOML
- Add a `ConfigPath` field (not serialized) so the server knows where to write
- Validate activity fields in `Validate()`: view_mode must be "flat"/"threaded", time_range must be one of the four values

## API

### Repo management

- `POST /api/v1/repos` -- body: `{"owner": "x", "name": "y"}`
  - Validates repo exists on GitHub via the existing `client.GetRepository()`
  - Returns 400 if repo is already configured
  - Returns 502 if GitHub API fails (repo doesn't exist or token lacks access)
  - On success: adds to config, saves config.toml, updates Syncer repo list, triggers sync, returns the new repo record
- `DELETE /api/v1/repos/{owner}/{name}`
  - Returns 404 if repo not in config
  - Removes from config, saves config.toml, updates Syncer repo list
  - Does not delete DB data (orphaned data is acceptable)

### Settings (activity preferences)

- `GET /api/v1/settings` -- returns activity preferences from config
- `PUT /api/v1/settings` -- body: `{"activity": {"view_mode": "threaded", ...}}`
  - Validates fields, updates config, saves config.toml
  - Returns updated settings

### Server changes

- Thread `configPath` into `Server` struct so handlers can load/save config
- Add a `sync.Mutex` on the server to serialize config reads/writes
- The `POST /api/v1/repos` handler needs access to the GitHub client (already available as `s.gh`) and the Syncer

### Syncer changes

- Add `SetRepos(repos []RepoRef)` method that updates the repo list under a mutex
- The existing `RunOnce` reads `s.repos` -- protect that read with the same mutex

## Frontend

### Settings page

New route `settings` in the router. Centered layout (max-width 640px) following the agentsview pattern.

**Repositories section:**
- List of current repos with `owner/name` display and a remove button (x) on each row
- Confirmation before removing (inline "are you sure?" or similar)
- "Add repository" form: single text input accepting `owner/name` format, Add button
- Inline validation feedback: shows error if repo not found / no access, success if added
- After adding: repo appears in list, sync triggers automatically

**Activity defaults section:**
- View mode: segmented control (Flat / Threaded)
- Time range: segmented control (24h / 7d / 30d / 90d)
- Hide closed/merged: toggle
- Hide bots: toggle
- Changes save immediately on interaction (no explicit save button)

### Header

Add a gear icon next to the existing theme toggle that navigates to the settings page.

### Sidebar

Add an "Add repo" link at the bottom of the PullList and IssueList sidebars that navigates to the settings page.

### API client

Add to `frontend/src/lib/api/client.ts`:
- `addRepo(owner: string, name: string): Promise<Repo>`
- `removeRepo(owner: string, name: string): Promise<void>`
- `getSettings(): Promise<Settings>`
- `updateSettings(settings: Settings): Promise<Settings>`

### Activity state persistence (bug fix)

The activity feed currently resets all filters when navigating away and back because:
1. `ActivityFeed.svelte` calls `syncFromURL()` on every `onMount`, which reads from URL params that were cleared by navigation
2. `hideClosedMerged`, `hideBots`, `enabledEvents`, and `itemFilter` are component-local `$state` that gets destroyed on unmount

Fix:
- Move `hideClosedMerged`, `hideBots`, `enabledEvents`, and `itemFilter` into the activity store as module-level state (same pattern as existing `filterTypes`, `viewMode`, etc.)
- Add an `initialized` flag to the activity store. On first mount, sync from URL. On subsequent mounts, restore from store state (skip `syncFromURL`)
- On mount: if not initialized, call `syncFromURL()` and set initialized. If already initialized, skip URL sync
- Continue syncing store-to-URL when filters change so bookmarkable URLs still work

### Settings applied to activity

On app startup, fetch settings from the API and apply activity defaults to the store (only if the store hasn't been initialized yet -- URL params take precedence over config defaults, config defaults take precedence over hardcoded defaults).

Initialization priority: URL params > config.toml defaults > hardcoded defaults.

## File inventory

### New files
- `frontend/src/lib/components/settings/SettingsPage.svelte`
- `frontend/src/lib/components/settings/SettingsSection.svelte`
- `frontend/src/lib/components/settings/RepoSettings.svelte`
- `frontend/src/lib/components/settings/ActivitySettings.svelte`

### Modified files
- `internal/config/config.go` -- Activity struct, Save method, ConfigPath field
- `internal/server/server.go` -- new routes, configPath field, config mutex
- `internal/server/handlers.go` -- new handler functions
- `internal/github/sync.go` -- SetRepos method, mutex on repos
- `cmd/middleman/main.go` -- pass configPath to server
- `frontend/src/lib/api/client.ts` -- new API functions
- `frontend/src/lib/stores/activity.svelte.ts` -- move local state to store, initialized flag
- `frontend/src/lib/stores/router.svelte.ts` -- add "settings" route
- `frontend/src/lib/components/layout/AppHeader.svelte` -- gear icon
- `frontend/src/lib/components/sidebar/PullList.svelte` -- add repo link
- `frontend/src/lib/components/sidebar/IssueList.svelte` -- add repo link
- `frontend/src/lib/components/ActivityFeed.svelte` -- use store state instead of local
- `frontend/src/App.svelte` -- settings route, load settings on startup
