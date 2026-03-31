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
- Validate activity fields in `Validate()`: view_mode must be "flat"/"threaded" (empty = "flat"), time_range must be one of the four values (empty = "7d")

## API

### Repo management

- `POST /api/v1/repos` -- body: `{"owner": "x", "name": "y"}`
  - Validates repo exists on GitHub via the existing `client.GetRepository()`
  - Returns 400 if repo is already configured
  - Returns 502 if GitHub API fails (repo doesn't exist or token lacks access)
  - On success: adds to config, saves config.toml, updates Syncer repo list, triggers sync, returns the new repo record
- `DELETE /api/v1/repos/{owner}/{name}`
  - Returns 400 if removing the repo would leave zero configured repos
  - Returns 404 if repo not in config
  - Removes from config, saves config.toml, updates Syncer repo list
  - Does not delete DB data (orphaned data is acceptable)

### Settings (activity preferences + configured repos)

- `GET /api/v1/settings` -- returns the full settings blob: configured repos list (from config, not DB) and activity preferences
- `PUT /api/v1/settings` -- body: `{"activity": {"view_mode": "threaded", ...}}`
  - Validates fields, updates config, saves config.toml
  - Returns updated settings
  - Only updates the `activity` section; repos are managed via their own endpoints

The existing `GET /api/v1/repos` remains DB-backed and unchanged. It returns all repos that have data in the DB (including orphaned repos that were removed from config). This is correct for PR/issue list grouping and filtering. The settings page uses `GET /api/v1/settings` to show the configured repo list.

### Server changes

- Thread `configPath` and a `*config.Config` into `Server` struct
- Add a `sync.Mutex` on the server to serialize config reads/writes
- The `POST /api/v1/repos` handler needs access to the GitHub client (already available as `s.gh`) and the Syncer

### Syncer changes

- Add `SetRepos(repos []RepoRef)` method that swaps the repo list under a `sync.Mutex`
- `RunOnce` copies `s.repos` to a local snapshot under the mutex, then iterates the snapshot without the lock held. This ensures `SetRepos` is never blocked for the duration of a full sync pass.

## Frontend

### Settings page

New route `settings` in the router. Centered layout (max-width 640px) following the agentsview pattern.

**Repositories section:**
- List of current repos (from `GET /api/v1/settings`, not DB endpoint) with `owner/name` display and a remove button (x) on each row
- Remove button disabled when only one repo remains (cannot delete the last repo)
- Confirmation before removing (inline "are you sure?" prompt)
- "Add repository" form: single text input accepting `owner/name` format, Add button
- Inline validation feedback: shows error if repo not found / no access / already configured, success if added
- After adding: repo appears in list, sync triggers automatically

**Activity defaults section:**
- View mode: segmented control (Flat / Threaded)
- Time range: segmented control (24h / 7d / 30d / 90d)
- Hide closed/merged: toggle
- Hide bots: toggle
- Changes save immediately on interaction via `PUT /api/v1/settings` (no explicit save button)

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

### Settings hydration

On app startup in `App.svelte`, fetch `GET /api/v1/settings` and apply activity defaults to the store before rendering any views. Gate rendering behind an `appReady` flag so the activity store is hydrated with config defaults before `ActivityFeed` mounts and calls `loadActivity()`.

Initialization sequence:
1. App mounts, sets `appReady = false`
2. Fetches `GET /api/v1/settings`
3. Calls `hydrateActivityDefaults(settings.activity)` on the activity store -- this sets `viewMode`, `timeRange`, `hideClosedMerged`, `hideBots` from config values
4. Sets `appReady = true`, views render
5. When `ActivityFeed` mounts: if URL has query params, those override the hydrated defaults (URL params > config > hardcoded). If URL has no params, the config defaults stick.

This ensures the initialization priority is: URL params > config.toml defaults > hardcoded defaults.

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
- `internal/github/sync.go` -- SetRepos method, mutex on repos field
- `cmd/middleman/main.go` -- pass configPath to server
- `frontend/src/lib/api/client.ts` -- new API functions
- `frontend/src/lib/stores/activity.svelte.ts` -- move local state to store, initialized flag, hydrateActivityDefaults
- `frontend/src/lib/stores/router.svelte.ts` -- add "settings" route
- `frontend/src/lib/components/layout/AppHeader.svelte` -- gear icon
- `frontend/src/lib/components/sidebar/PullList.svelte` -- add repo link
- `frontend/src/lib/components/sidebar/IssueList.svelte` -- add repo link
- `frontend/src/lib/components/ActivityFeed.svelte` -- use store state instead of local
- `frontend/src/App.svelte` -- settings route, appReady gate, settings hydration
