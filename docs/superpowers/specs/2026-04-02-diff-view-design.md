# Diff View Design

A full-width PR diff viewer for middleman, powered by local bare git clones.

## Architecture

Three layers, one small schema addition (head/base SHAs on the pull_requests table):

1. **Bare clone manager** (`internal/gitclone/`) — maintains bare clones of tracked repos on disk
2. **Diff API endpoint** — computes diffs on demand from the bare clones and returns structured JSON
3. **Diff view** — Svelte 5 full-width view with file tree, syntax-highlighted unified diffs, and toolbar controls

Data flow:

```
Sync Engine -> git fetch on bare clone -> clone stays current
User opens diff view -> API calls git diff -> parses to JSON -> Svelte renders
```

No diffs stored in the database. The bare clone is the source of truth for file content.

### Schema Addition

Five new columns on `pull_requests` (added via `ALTER TABLE ADD COLUMN` migration):
- `github_head_sha TEXT NOT NULL DEFAULT ''` — the current head SHA as reported by GitHub, updated on every metadata sync regardless of fetch success
- `github_base_sha TEXT NOT NULL DEFAULT ''` — the current base branch tip SHA as reported by GitHub (`PullRequest.Base.SHA`), updated on every metadata sync regardless of fetch success
- `diff_head_sha TEXT NOT NULL DEFAULT ''` — the head SHA for which the local diff was last verified (only updated after a successful clone fetch)
- `diff_base_sha TEXT NOT NULL DEFAULT ''` — the base branch tip SHA at the time `merge_base_sha` was computed (snapshot of `github_base_sha` at fetch time, only updated after a successful clone fetch)
- `merge_base_sha TEXT NOT NULL DEFAULT ''` — the merge base SHA, computed locally via `git merge-base` after a successful clone fetch for any non-merged PR (open or closed-but-unmerged)

Storing the merge base (not the base branch tip) ensures correct diffs even after merge. Once a PR is merged, the head becomes reachable from the base branch, which would cause `git diff base_tip...head` to collapse to an empty diff. By capturing the merge base while the PR is still open, the diff remains stable: `git diff merge_base_sha diff_head_sha` (two-dot, not three-dot) produces the correct result regardless of merge state.

For PRs that are still open at the time of the first sync with this feature, the merge base is computed and stored normally. For already-merged PRs that were synced before this feature was added, the merge base cannot be reliably recovered post-merge (the compare API would return the head itself as the common ancestor). These PRs return a 404 from the diff endpoint with a message indicating the diff is not available. This is an acceptable degradation — it only affects historical PRs merged before the feature is deployed, and users can still view those diffs on GitHub directly.

These are populated during sync from the GitHub API. Using immutable SHAs instead of branch names ensures diffs work correctly for:
- Forked PRs (where the head branch lives in a different repo)
- Deleted branches (merged/closed PRs whose head was cleaned up)
- Same-named branches across different PRs

The sync engine also fetches GitHub's pull refs (`refs/pull/*/head`) during `git fetch`, ensuring head SHAs for forked PRs are available in the bare clone even though the fork's branches are not.

## Backend

### Bare Clone Manager

New package: `internal/gitclone/`

Responsibilities:
- Maintain bare clones (no working tree) in `{data_dir}/clones/{owner}/{name}.git`
- `git clone --bare` on first encounter during sync
- `git fetch --prune` on subsequent syncs
- Authenticate git subprocess commands via environment variables, not command-line arguments (which are visible in `ps` and `/proc`). The clone manager sets `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=http.extraHeader`, and `GIT_CONFIG_VALUE_0=Authorization: Bearer <token>` in the subprocess environment. These env vars are inherited by the git child process only, not visible in process listings, and never written to disk. Uses the same token resolution chain as the rest of middleman (`MIDDLEMAN_GITHUB_TOKEN` env var, falling back to `gh auth token`). No user configuration or prompts required.
- Execute `git diff {merge_base_sha} {diff_head_sha}` and parse the unified diff output into structured data
- All git operations run as subprocesses (no Go git library needed)

Key properties:
- Bare clones use ~30-50% less disk than full clones (no working tree)
- No checkout conflicts or working tree state to manage
- Clone/fetch happens during the sync cycle, not on user request

### Sync Integration

The sync engine (`internal/github/sync.go`) gains a reference to the clone manager. During `syncRepo`, the clone manager fetch runs **before** PR data is upserted, ensuring that `refs/pull/*/head` and branch refs are available locally before `merge_base_sha` is computed for any PR. This is a single `git fetch` call per repo per sync cycle (not per PR).

The fetch refspec includes `+refs/pull/*/head:refs/pull/*/head` to retrieve GitHub's pull request refs. This ensures that head SHAs for forked PRs (where the head branch lives in the fork, not the base repo) are available in the bare clone.

Fetch failures are logged but do not block PR metadata sync. `diff_head_sha`, `diff_base_sha`, and `merge_base_sha` are only computed and persisted when the fetch succeeds. If the fetch fails, these are preserved while `github_head_sha` and `github_base_sha` are still updated from the GitHub API on every metadata sync. The diff endpoint compares the diff SHAs against the GitHub SHAs to detect staleness: for open and closed-but-unmerged PRs, `stale = (diff_head_sha != github_head_sha) || (diff_base_sha != github_base_sha)`. For merged PRs, `stale = (diff_head_sha != github_head_sha)` (base-side movement is irrelevant post-merge, but the head must match). When `stale` is true, the frontend displays a warning banner: "Diff may be outdated — showing changes as of an earlier version of this PR." The diff SHAs will be refreshed on the next successful fetch for any non-merged PR (open or closed-but-unmerged). For merged PRs, diff SHAs are kept only when the stored merge base was computed against the current head SHA. If the head changed since the merge base was computed (e.g., force-push before merge during a fetch outage), the diff remains stale — advancing `diff_head_sha` without recomputing the merge base would produce an incorrect diff.

### Diff API Endpoint

```
GET /api/v1/repos/{owner}/{name}/pulls/{number}/diff
```

Flow:
1. Look up the PR's `diff_head_sha`, `merge_base_sha`, `github_head_sha`, `github_base_sha`, and `diff_base_sha` from the database
2. Run `git diff {merge_base_sha} {diff_head_sha}` (two-dot) on the bare clone; set `stale` based on PR state: for open and closed-but-unmerged PRs, `stale = (diff_head_sha != github_head_sha) || (diff_base_sha != github_base_sha)`; for merged PRs, `stale = (diff_head_sha != github_head_sha)` (base-side movement is expected post-merge since the merge itself advances the base branch; the stored merge base is correct as long as the head matches)
3. Parse the unified diff output into structured JSON
4. Return the response

Response shape:
```json
{
  "stale": false,
  "whitespace_only_count": 3,
  "files": [
    {
      "path": "internal/auth/token.go",
      "old_path": "internal/auth/token.go",
      "status": "modified",
      "is_binary": false,
      "additions": 32,
      "deletions": 8,
      "hunks": [
        {
          "header": "@@ -28,12 +28,16 @@ func (t *TokenStore) Refresh(...)",
          "old_start": 28,
          "old_count": 12,
          "new_start": 28,
          "new_count": 16,
          "lines": [
            { "type": "context", "content": "\tt.mu.Lock()", "old_num": 28, "new_num": 28 },
            { "type": "delete", "content": "\ttoken, err := t.fetchToken(ctx)", "old_num": 31 },
            { "type": "add", "content": "\tresult := t.group.Do(\"refresh\", func() (any, error) {", "new_num": 32, "no_newline": false }
          ]
        }
      ]
    }
  ]
}
```

Query parameters:
- `whitespace=hide` — suppress whitespace-only changes by passing `-w` to `git diff`. When omitted (default), all changes including whitespace are shown. This is opt-in because whitespace is semantically significant in some languages (Python, YAML, Makefiles) where indentation changes are real behavioral changes.

Top-level response fields:
- `stale`: boolean, always present. For open and closed-but-unmerged PRs, `true` when either the head or base SHA used for the diff differs from what GitHub currently reports (e.g., after force-push, rebase, base branch advance, or fetch failure). For merged PRs, `true` only when `diff_head_sha != github_head_sha` (base-side movement is expected post-merge). The frontend shows a warning banner when true.
- `whitespace_only_count`: integer, always present. Number of whitespace-only files (hidden when `whitespace=hide`).

Per-file field definitions:
- `status`: one of `added`, `modified`, `deleted`, `renamed`, `copied`
- `is_binary`: boolean, always present. `true` when git detects the file as binary (`Binary files ... differ`). Binary files have an empty `hunks` array and zero additions/deletions. The frontend renders a "Binary file changed" placeholder instead of line-level diff.
- `is_whitespace_only`: boolean, always present. `true` when all changes in the file are whitespace-only. In default mode, whitespace-only files are included with this flag set to `true`. In `whitespace=hide` mode, whitespace-only files are excluded entirely; remaining files always have this field set to `false`.

Line object fields:
- `type`: one of `context`, `add`, `delete`
- `content`: the line content (with leading tab/space preserved)
- `old_num`: line number in the old file (present for `context` and `delete` lines)
- `new_num`: line number in the new file (present for `context` and `add` lines)
- `no_newline`: boolean, `true` when this line is not terminated by a newline (corresponds to git's `\ No newline at end of file` sentinel). The frontend renders a subtle annotation on these lines. Defaults to `false` and may be omitted when false.

#### Whitespace handling

The `-w` flag affects the diff at two levels:
1. **Whole files** — files where the only changes are whitespace are excluded entirely
2. **Individual lines** — within files that have substantive changes, whitespace-only line changes are also suppressed

The API always runs two `git diff --raw -z --no-renames` passes (one with `-w`, one without) to compute `whitespace_only_count`. Files present in the non-`-w` output but absent from the `-w` output are whitespace-only. `--no-renames` is used in both counting passes to avoid `-w` changing similarity scores. This pair of raw enumerations runs regardless of which mode is requested, so the count is always available and consistent.

In default mode (no query param), the full non-`-w` diff is parsed for the response, and `is_whitespace_only` is set on each file based on the counting result. In `whitespace=hide` mode, the `-w` diff is parsed for the response, whitespace-only files are absent, and the count tells the sidebar how many were hidden.

When `whitespace=hide` is set, the diff is computed with `-w`, which suppresses both whitespace-only files and whitespace-only line changes within files. The count is reported so the sidebar can show "N whitespace-only files hidden". When omitted (default), all changes are shown, and whitespace-only files are included with `is_whitespace_only: true`.

Error cases:
- Clone not yet available (first sync hasn't completed): return 404 with message
- SHA not found in clone (force-pushed and garbage collected): return 404 with message
- diff_head_sha or merge_base_sha not populated: return 404 with message "Diff not available for this pull request". This can happen when: (a) the PR was merged before the diff feature was deployed, or (b) the bare clone has never been successfully fetched since the PR was created. Both columns default to empty string and are only populated after a successful clone fetch

### Diff Parsing

The unified diff parser lives in `internal/gitclone/` alongside the git operations. It parses the raw `git diff` output into the structured JSON response. The parser handles the full git diff format, not just basic unified hunks:

Standard unified diff lines:
- `diff --git a/... b/...` lines mark file boundaries
- `---` / `+++` lines give old/new paths
- `@@ ... @@` lines mark hunk boundaries with line numbers
- Lines prefixed with ` `, `+`, `-` are context/add/delete
- `\ No newline at end of file` — metadata sentinel, not a content line. The parser sets a `no_newline: true` flag on the preceding diff line so the frontend can render an indicator (e.g., a subtle "no newline at end of file" annotation). Does not increment line numbers.

Extended git headers (required for rename/copy/binary support):
- `rename from <path>` / `rename to <path>` — populates `old_path` and sets status to `renamed`
- `copy from <path>` / `copy to <path>` — populates `old_path` and sets status to `copied`
- `similarity index N%` — available but not surfaced in the response
- `new file mode` / `deleted file mode` — sets status to `added` / `deleted`
- `Binary files ... differ` — marks the file as binary (no hunks, file is listed with status but no line-level diff)

The diff command uses `-M -C --find-copies-harder` for rename and copy detection, including copies from unchanged files. This is more expensive but produces accurate results for the typical PR sizes middleman handles. Binary files are listed in the file tree but show a "Binary file changed" placeholder instead of hunks.

File metadata (paths, status, rename/copy info) is extracted from `git diff --raw -z` output rather than parsing human-readable patch headers, which avoids issues with git's quoting/escaping of unusual filenames (tabs, backslashes, spaces, non-ASCII). The `-z` flag uses NUL-delimited output for reliable parsing. The patch content is still parsed from the standard unified diff output, but raw and patch records are correlated by output order (git emits them in the same file order), not by path string matching. This avoids the need to unquote git's C-style path escaping in patch headers.

## Frontend

### Route

New route: `/pulls/{owner}/{name}/{number}/files`

The router's `Route` union type for the `pulls` page gains a new view: `{ page: "pulls"; view: "diff"; owner: string; name: string; number: number }`. `parseRoute` matches `/pulls/{owner}/{name}/{number}/files` and returns this variant. `getPage()` returns `"pulls"` so the PRs tab stays active in the header. The main `App.svelte` switch renders the diff view component when `view === "diff"`. A new `isDiffView()` helper returns true for this route; global keyboard handlers (the existing `j`/`k` for PR/issue selection) check `!isDiffView()` before activating.

Accessed from the PR detail view via a "Files changed" link (in the chips row, near the existing `+N/-M` stats). The router's `navigate()` helper is extended to accept optional `state` that is passed to `history.pushState`. When navigating to the diff view, `navigate('/pulls/.../files', { fromApp: true })` is called, which handles base path prefixing and router state update consistently. The back button reads `history.state` on the current (diff) entry: if `fromApp` is true, it calls `history.back()` (safe because the previous entry is an in-app page). If `fromApp` is absent (direct navigation, external link, new tab), it calls `navigate(`/pulls/${owner}/${name}/${number}`)` as a safe fallback.

### Layout

Full-width three-part layout:

1. **Top bar** — back button (label "Back"; uses `history.back()` when `history.state?.fromApp` is true, otherwise navigates to PR detail), PR title, file count, total +/- stats
2. **File tree sidebar** (left) — collapsible, resizable via drag handle
3. **Diff area** (right) — scrollable, contains all file diffs vertically

### File Tree Sidebar

- Header with "Files" label and collapse chevron (`<`)
- Search/filter input
- Files grouped by directory (directory names as section headers)
- Each file shows: status badge, filename, right-aligned +/- counts. Badge values: M (modified, amber), A (added, green), D (deleted, red), R (renamed, blue), C (copied, blue)
- The +/- count columns are fixed-width and right-aligned so numbers align across all files
- Active file is highlighted with a left accent border
- Deleted files show strikethrough filename
- Footer: informational text "N whitespace-only files hidden" (visible only when the toolbar's "Hide whitespace" toggle is on). This is not a separate control — the toolbar toggle is the single whitespace control.
- Collapsible: chevron in header toggles sidebar visibility
- Resizable: drag handle on the right edge of the sidebar (4px hit target overlaying the border, `col-resize` cursor, highlights blue during drag)
- Sidebar width persisted in localStorage
- Click a file to scroll the diff area to that file

### Diff Area

#### Toolbar
- **Tab width selector**: segmented control with options 1, 2, 4, 8. Sets CSS `tab-size` on the diff container. Persisted in localStorage. Default: 4.
- **Hide whitespace toggle**: toggle switch, off by default. When toggled on, re-fetches the diff with `?whitespace=hide` to suppress whitespace-only changes. When off (default), all changes including whitespace are shown.

#### File Sections
Each file rendered as a section:
- **Sticky file header**: file path, +/- stats, collapse button. Sticks to top of the diff area during scroll so you always know which file you're in. Thick top border (2px) separates files.
- **Hunks**: each hunk starts with a hunk header line (`@@ ... @@`) with the function/context signature, rendered in a subtle blue tint.
- **Diff lines**: three types rendered differently:
  - **Context lines**: neutral background, both line numbers shown, code in default text color
  - **Added lines**: green-tinted background (lighter on code, slightly more saturated on gutter), only new line number shown, `+` marker in gutter
  - **Deleted lines**: red-tinted background (lighter on code, slightly more saturated on gutter), only old line number shown, `-` marker in gutter
- **Collapsed unchanged regions**: between hunks, unchanged lines are collapsed into a single row showing "N unchanged lines" with a subtle blue tint. These are non-expandable separators — the backend only returns hunk context lines, not the full file content between hunks. The line count is derived from the gap between the previous hunk's end and the next hunk's start.
- **Line numbers**: two gutter columns (old, new), each 50px, right-aligned. Line numbers are `user-select: none`.
- **Code content**: rendered in `<pre>` elements to preserve whitespace. Tab characters are literal (not converted to spaces), displayed at the configured `tab-size`.

#### Syntax Highlighting
- Shiki for tokenization and highlighting
- Two themes loaded: one for dark mode, one for light mode
- Theme switches with the app's existing theme toggle (no separate diff theme control)
- Highlighting applied per-line, respecting the diff line type (added/deleted lines use tinted colors appropriate to their background)
- Language detection from file extension

#### Theming
The diff view uses CSS custom properties for all colors, with two complete palettes (dark and light) that switch with the app theme. Both the sidebar/chrome and the diff area are themed together.

Dark theme syntax colors follow the GitHub Dark palette convention:
- Keywords: `#ff7b72` (red)
- Functions: `#d2a8ff` (purple)
- Types: `#79c0ff` (blue)
- Strings: `#a5d6ff` (light blue)
- Comments: `#8b949e` (gray)

Light theme syntax colors follow the GitHub Light palette convention:
- Keywords: `#cf222e` (red)
- Functions: `#8250df` (purple)
- Types: `#0969da` (blue)
- Strings: `#0a3069` (dark blue)
- Comments: `#6e7781` (gray)

### Navigation

- Clicking a file in the sidebar scrolls the diff area to that file
- The sidebar highlights the file currently visible in the diff area (based on scroll position)
- Keyboard: `j`/`k` to jump between files (next/previous file header)

### State Management

New store: `frontend/src/lib/stores/diff.svelte.ts`
- Fetches diff data from the API
- Tracks loading/error state
- Manages toolbar preferences (tab width, whitespace toggle) via localStorage
- Tracks per-file collapse state, persisted in localStorage keyed by `{owner}/{name}#{number}` so collapse state is remembered when navigating away and back to the same PR's diff
- Re-fetches when whitespace toggle changes

## Scope Boundaries

Explicitly out of scope for this design:
- Inline line comments or review submission
- Side-by-side (split) diff view
- Blame/history per file
- Storing diffs in the database
- Syntax highlighting for every language (start with common ones: Go, TypeScript, JavaScript, Python, Rust, JSON, YAML, Markdown, SQL, shell)
