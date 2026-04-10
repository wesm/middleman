# Normalized PR And Issue Labels Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add repo-scoped normalized label storage for pull requests and issues, expose structured label arrays from the API, and render GitHub-style label pills in shared Svelte UI.

**Architecture:** Store labels in `middleman_labels` plus per-item join tables, then attach labels to PR and issue rows in Go before serializing API responses. Keep runtime reads and writes on normalized tables only, hide legacy `issues.labels_json` from the API, and use a single `GitHubLabels.svelte` component for compact and full rendering.

**Tech Stack:** Go, SQLite migrations, Huma/OpenAPI, Bun, Svelte 5, Vitest, Testing Library

---

## File Structure

**Create:**

- `internal/db/migrations/000005_normalize_labels.up.sql` - creates normalized label tables, indexes, and backfills issue labels from legacy JSON.
- `internal/db/migrations/000005_normalize_labels.down.sql` - drops the new tables and indexes.
- `packages/ui/src/components/shared/GitHubLabels.svelte` - shared label renderer for compact and full modes.
- `packages/ui/src/components/shared/GitHubLabels.test.ts` - label component tests.

**Modify:**

- `internal/db/types.go` - add shared label model and attach label slices to `MergeRequest` and `Issue`; hide legacy `LabelsJSON` from JSON output.
- `internal/db/db_test.go` - assert the new tables exist after opening a DB.
- `internal/db/queries.go` - add label upsert/replace/load helpers and attach labels in PR/issue list/detail queries.
- `internal/db/queries_test.go` - cover normalized label queries and repo scoping.
- `internal/github/normalize.go` - normalize GitHub labels into structured rows instead of item JSON blobs.
- `internal/github/normalize_test.go` - cover PR/issue label normalization.
- `internal/github/sync.go` - persist labels for PRs and issues in index sync, full sync, closed-item sync, and backfill paths.
- `internal/github/sync_test.go` - cover label persistence and label replacement on resync.
- `internal/server/api_types.go` - ensure response structs expose the new structured labels only.
- `internal/server/huma_routes.go` - verify the PR and issue handlers still serialize the updated DB models cleanly after the schema change.
- `internal/server/api_test.go` - assert list/detail endpoints return structured labels.
- `cmd/middleman-openapi` generated outputs via `make api-generate`:
  - `frontend/openapi/openapi.json`
  - `internal/apiclient/spec/openapi.json`
  - `internal/apiclient/generated/client.gen.go`
  - `packages/ui/src/api/generated/schema.ts`
  - `packages/ui/src/api/generated/client.ts`
- `packages/ui/src/api/types.ts` - replace the hand-written issue-only label interface with the generated shared label type.
- `packages/ui/src/components/sidebar/PullItem.svelte` - render compact labels.
- `packages/ui/src/components/sidebar/IssueItem.svelte` - switch from inline JSON parsing to shared component.
- `packages/ui/src/components/detail/PullDetail.svelte` - render full labels.
- `packages/ui/src/components/detail/IssueDetail.svelte` - switch from inline JSON parsing to shared component.

**Reference While Implementing:**

- `docs/superpowers/specs/2026-04-10-pr-issue-labels-design.md`
- `frontend/vite.config.ts` - confirms frontend tests include `../packages/ui/src/**/*.{test,spec}.*`.
- `internal/server/huma_routes.go` - PR and issue list/detail handlers.

### Task 1: Add The Normalized Label Schema

**Files:**
- Create: `internal/db/migrations/000005_normalize_labels.up.sql`
- Create: `internal/db/migrations/000005_normalize_labels.down.sql`
- Modify: `internal/db/db_test.go`

- [ ] **Step 1: Write the failing migration test**

Update `internal/db/db_test.go` so `TestOpenAndSchema` expects the new label tables, and add a migration backfill test.

```go
func TestOpenAndSchema(t *testing.T) {
	d := openTestDB(t)
	tables := []string{
		"middleman_repos",
		"middleman_merge_requests",
		"middleman_mr_events",
		"middleman_kanban_state",
		"middleman_labels",
		"middleman_merge_request_labels",
		"middleman_issue_labels",
	}
	for _, tbl := range tables {
		var name string
		err := d.ReadDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		require.NoErrorf(t, err, "table %s should exist", tbl)
	}
}

func TestOpenBackfillsIssueLabelsIntoNormalizedTables(t *testing.T) {
	require := require.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(err)
	_, err = raw.Exec(legacySchemaSQLForTest(t, 4))
	require.NoError(err)
	_, err = raw.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool)`)
	require.NoError(err)
	_, err = raw.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (4, FALSE)`)
	require.NoError(err)
	_, err = raw.Exec(`INSERT INTO middleman_repos (id, platform, platform_host, owner, name, created_at) VALUES (1, 'github', 'github.com', 'acme', 'widget', datetime('now'))`)
	require.NoError(err)
	_, err = raw.Exec(`
		INSERT INTO middleman_issues (
			id, repo_id, platform_id, number, url, title, author, state,
			body, comment_count, labels_json, created_at, updated_at, last_activity_at
		) VALUES (
			1, 1, 1001, 5, 'https://github.com/acme/widget/issues/5', 'Bug', 'alice', 'open',
			'', 0, '[{"name":"bug","color":"d73a4a"}]', datetime('now'), datetime('now'), datetime('now')
		)`)
	require.NoError(err)
	require.NoError(raw.Close())

	d, err := Open(path)
	require.NoError(err)
	t.Cleanup(func() { d.Close() })

	var count int
	err = d.ReadDB().QueryRow(`SELECT COUNT(*) FROM middleman_issue_labels`).Scan(&count)
	require.NoError(err)
	require.Equal(1, count)

	var name, color string
	err = d.ReadDB().QueryRow(`SELECT name, color FROM middleman_labels LIMIT 1`).Scan(&name, &color)
	require.NoError(err)
	require.Equal("bug", name)
	require.Equal("d73a4a", color)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db -run 'TestOpenAndSchema|TestOpenBackfillsIssueLabelsIntoNormalizedTables' -count=1`
Expected: FAIL because the three new label tables do not exist yet and there is no migration backfill.

- [ ] **Step 3: Write the migration files**

Create `internal/db/migrations/000005_normalize_labels.up.sql` with this content:

```sql
CREATE TABLE IF NOT EXISTS middleman_labels (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id     INTEGER NOT NULL REFERENCES middleman_repos(id) ON DELETE CASCADE,
    platform_id INTEGER,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    color       TEXT NOT NULL DEFAULT '',
    is_default  INTEGER NOT NULL DEFAULT 0,
    updated_at  DATETIME NOT NULL,
    UNIQUE(repo_id, platform_id),
    UNIQUE(repo_id, name)
);

CREATE TABLE IF NOT EXISTS middleman_merge_request_labels (
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    label_id         INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (merge_request_id, label_id)
);

CREATE TABLE IF NOT EXISTS middleman_issue_labels (
    issue_id  INTEGER NOT NULL REFERENCES middleman_issues(id) ON DELETE CASCADE,
    label_id  INTEGER NOT NULL REFERENCES middleman_labels(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

CREATE INDEX IF NOT EXISTS idx_labels_repo_name
    ON middleman_labels(repo_id, name);
CREATE INDEX IF NOT EXISTS idx_labels_repo_platform_id
    ON middleman_labels(repo_id, platform_id);
CREATE INDEX IF NOT EXISTS idx_mr_labels_label_id
    ON middleman_merge_request_labels(label_id);
CREATE INDEX IF NOT EXISTS idx_issue_labels_label_id
    ON middleman_issue_labels(label_id);

INSERT INTO middleman_labels (repo_id, platform_id, name, description, color, is_default, updated_at)
SELECT
    i.repo_id,
    NULL,
    json_extract(je.value, '$.name'),
    COALESCE(json_extract(je.value, '$.description'), ''),
    COALESCE(json_extract(je.value, '$.color'), ''),
    0,
    COALESCE(i.updated_at, datetime('now'))
FROM middleman_issues i,
     json_each(CASE WHEN i.labels_json = '' THEN '[]' ELSE i.labels_json END) je
WHERE json_extract(je.value, '$.name') IS NOT NULL
ON CONFLICT(repo_id, name) DO UPDATE SET
    description = excluded.description,
    color = excluded.color,
    updated_at = excluded.updated_at;

INSERT INTO middleman_issue_labels (issue_id, label_id)
SELECT DISTINCT
    i.id,
    l.id
FROM middleman_issues i,
     json_each(CASE WHEN i.labels_json = '' THEN '[]' ELSE i.labels_json END) je
JOIN middleman_labels l
  ON l.repo_id = i.repo_id
 AND l.name = json_extract(je.value, '$.name')
WHERE json_extract(je.value, '$.name') IS NOT NULL
ON CONFLICT(issue_id, label_id) DO NOTHING;
```

Create `internal/db/migrations/000005_normalize_labels.down.sql` with this content:

```sql
DROP INDEX IF EXISTS idx_issue_labels_label_id;
DROP INDEX IF EXISTS idx_mr_labels_label_id;
DROP INDEX IF EXISTS idx_labels_repo_platform_id;
DROP INDEX IF EXISTS idx_labels_repo_name;

DROP TABLE IF EXISTS middleman_issue_labels;
DROP TABLE IF EXISTS middleman_merge_request_labels;
DROP TABLE IF EXISTS middleman_labels;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db -run 'TestOpenAndSchema|TestOpenBackfillsIssueLabelsIntoNormalizedTables' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/migrations/000005_normalize_labels.up.sql internal/db/migrations/000005_normalize_labels.down.sql internal/db/db_test.go
git commit -m "feat: add normalized label schema"
```

### Task 2: Move DB Models And Queries To Structured Labels

**Files:**
- Modify: `internal/db/types.go`
- Modify: `internal/db/queries.go`
- Modify: `internal/db/queries_test.go`

- [ ] **Step 1: Write the failing query tests**

Add tests to `internal/db/queries_test.go` covering:

```go
func TestListMergeRequests_AttachesLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	_, err = d.UpsertMergeRequest(ctx, &MergeRequest{
		RepoID: repoID, PlatformID: 101, Number: 7,
		URL: "https://github.com/acme/widget/pull/7",
		Title: "Add labels", Author: "alice", State: "open",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
	})
	require.NoError(err)

	mrID, err := d.GetMRIDByRepoAndNumber(ctx, "acme", "widget", 7)
	require.NoError(err)
	require.NoError(d.ReplaceMergeRequestLabels(ctx, repoID, mrID, []Label{{
		PlatformID: 5001,
		Name:      "needs-review",
		Color:     "fbca04",
	}}))

	mrs, err := d.ListMergeRequests(ctx, ListMergeRequestsOpts{})
	require.NoError(err)
	require.Len(mrs, 1)
	require.Len(mrs[0].Labels, 1)
	require.Equal("needs-review", mrs[0].Labels[0].Name)
}

func TestListIssues_AttachesRepoScopedLabels(t *testing.T) {
	require := require.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoA, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	repoB, err := d.UpsertRepo(ctx, "github.com", "acme", "gadget")
	require.NoError(err)

	issueID, err := d.UpsertIssue(ctx, &Issue{
		RepoID: repoA, PlatformID: 201, Number: 3,
		URL: "https://github.com/acme/widget/issues/3",
		Title: "Bug", Author: "bob", State: "open",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
	})
	require.NoError(err)

	require.NoError(d.ReplaceIssueLabels(ctx, repoA, issueID, []Label{{PlatformID: 11, Name: "bug", Color: "d73a4a"}}))
	require.NoError(d.UpsertLabels(ctx, repoB, []Label{{PlatformID: 22, Name: "bug", Color: "0e8a16"}}))

	issues, err := d.ListIssues(ctx, ListIssuesOpts{})
	require.NoError(err)
	require.Len(issues, 1)
	require.Equal("d73a4a", issues[0].Labels[0].Color)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/db -run 'TestListMergeRequests_AttachesLabels|TestListIssues_AttachesRepoScopedLabels' -count=1`
Expected: FAIL because `Label`, `ReplaceMergeRequestLabels`, `ReplaceIssueLabels`, and `Labels` fields do not exist yet.

- [ ] **Step 3: Write the minimal DB model and query implementation**

Update `internal/db/types.go` with a shared label type and attach it to PRs and issues.

```go
type Label struct {
	ID          int64
	RepoID       int64
	PlatformID   int64 `json:"-"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Color        string `json:"color"`
	IsDefault    bool   `json:"is_default"`
	UpdatedAt    time.Time `json:"-"`
}

type MergeRequest struct {
	KanbanStatus string
	Starred      bool
	Labels []Label `json:"labels,omitempty"`
}

type Issue struct {
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastActivityAt  time.Time
	ClosedAt        *time.Time
	DetailFetchedAt *time.Time
	Starred         bool
	LabelsJSON string `json:"-"`
	Labels     []Label `json:"labels,omitempty"`
}
```

Add a SQL placeholder helper and transaction-safe label helpers to `internal/db/queries.go`:

```go
func sqlPlaceholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func upsertLabelsTx(ctx context.Context, tx *sql.Tx, repoID int64, labels []Label) (map[string]int64, error) {
	ids := make(map[string]int64, len(labels))
	for _, label := range labels {
		var id int64
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM middleman_labels
			WHERE repo_id = ? AND (name = ? OR platform_id = ?)
			LIMIT 1`,
			repoID, label.Name, label.PlatformID,
		).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			result, err := tx.ExecContext(ctx, `
				INSERT INTO middleman_labels (repo_id, platform_id, name, description, color, is_default, updated_at)
				VALUES (?, NULLIF(?, 0), ?, ?, ?, ?, ?)`,
				repoID, label.PlatformID, label.Name, label.Description, label.Color, label.IsDefault, label.UpdatedAt,
			)
			if err != nil {
				return nil, fmt.Errorf("insert label %s: %w", label.Name, err)
			}
			id, err = result.LastInsertId()
			if err != nil {
				return nil, fmt.Errorf("label insert id %s: %w", label.Name, err)
			}
		} else if err != nil {
			return nil, fmt.Errorf("lookup label %s: %w", label.Name, err)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE middleman_labels
				SET platform_id = COALESCE(NULLIF(?, 0), platform_id),
				    name = ?,
				    description = ?,
				    color = ?,
				    is_default = ?,
				    updated_at = ?
				WHERE id = ?`,
				label.PlatformID, label.Name, label.Description, label.Color, label.IsDefault, label.UpdatedAt, id,
			)
			if err != nil {
				return nil, fmt.Errorf("update label %s: %w", label.Name, err)
			}
		}
		ids[label.Name] = id
	}
	return ids, nil
}

func (d *DB) UpsertLabels(ctx context.Context, repoID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := upsertLabelsTx(ctx, tx, repoID, labels)
		return err
	})
}

func (d *DB) ReplaceIssueLabels(ctx context.Context, repoID, issueID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM middleman_issue_labels WHERE issue_id = ?`, issueID); err != nil {
			return err
		}
		ids, err := upsertLabelsTx(ctx, tx, repoID, labels)
		if err != nil {
			return err
		}
		for _, label := range labels {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO middleman_issue_labels (issue_id, label_id) VALUES (?, ?)`,
				issueID, ids[label.Name],
			); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) ReplaceMergeRequestLabels(ctx context.Context, repoID, mrID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM middleman_merge_request_labels WHERE merge_request_id = ?`, mrID); err != nil {
			return err
		}
		ids, err := upsertLabelsTx(ctx, tx, repoID, labels)
		if err != nil {
			return err
		}
		for _, label := range labels {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO middleman_merge_request_labels (merge_request_id, label_id) VALUES (?, ?)`,
				mrID, ids[label.Name],
			); err != nil {
				return err
			}
		}
		return nil
	})
}
```

Add batched label loaders and call them from `ListMergeRequests`, `GetMergeRequest`, `ListIssues`, and `GetIssue` after scanning the base rows:

```go
func (d *DB) loadLabelsForMergeRequests(ctx context.Context, ids []int64) (map[int64][]Label, error) {
	if len(ids) == 0 {
		return map[int64][]Label{}, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT ml.merge_request_id, l.id, l.repo_id, COALESCE(l.platform_id, 0), l.name, l.description, l.color, l.is_default, l.updated_at
		FROM middleman_merge_request_labels ml
		JOIN middleman_labels l ON l.id = ml.label_id
		WHERE ml.merge_request_id IN (%s)
		ORDER BY l.name`, sqlPlaceholders(len(ids)))
	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]Label, len(ids))
	for rows.Next() {
		var ownerID int64
		var label Label
		if err := rows.Scan(&ownerID, &label.ID, &label.RepoID, &label.PlatformID, &label.Name, &label.Description, &label.Color, &label.IsDefault, &label.UpdatedAt); err != nil {
			return nil, err
		}
		out[ownerID] = append(out[ownerID], label)
	}
	return out, rows.Err()
}
```

Mirror that helper for issues, then assign:

```go
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, mrIDs)
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	for i := range mrs {
		mrs[i].Labels = labelsByMR[mrs[i].ID]
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/db -run 'TestListMergeRequests_AttachesLabels|TestListIssues_AttachesRepoScopedLabels' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db/types.go internal/db/queries.go internal/db/queries_test.go
git commit -m "feat: load normalized labels from the database"
```

### Task 3: Persist Labels In GitHub Sync Paths

**Files:**
- Modify: `internal/github/normalize.go`
- Modify: `internal/github/normalize_test.go`
- Modify: `internal/github/sync.go`
- Modify: `internal/github/sync_test.go`

- [ ] **Step 1: Write the failing normalization and sync tests**

Add these tests:

```go
func TestNormalizeLabels(t *testing.T) {
	labels := NormalizeLabels([]*gh.Label{
		{ID: github.Int64(10), Name: github.String("bug"), Color: github.String("d73a4a"), Description: github.String("Something is broken"), Default: github.Bool(true)},
	})
	require.Len(t, labels, 1)
	require.Equal(t, int64(10), labels[0].PlatformID)
	require.Equal(t, "bug", labels[0].Name)
	require.Equal(t, "d73a4a", labels[0].Color)
	require.Equal(t, "Something is broken", labels[0].Description)
	require.True(t, labels[0].IsDefault)
}
```

And in `internal/github/sync_test.go` add two sync tests: one for an open PR list sync and one for an issue detail sync. In each test, return GitHub labels from the mock client, run the real sync method, and assert `database.GetMergeRequest(...).Labels` or `database.GetIssue(...).Labels` contain the expected label names and colors.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github -run 'TestNormalizeLabels|TestSync.*Labels' -count=1`
Expected: FAIL because there is no structured label normalization or persistence yet.

- [ ] **Step 3: Implement normalized label persistence**

In `internal/github/normalize.go`, replace the JSON helper with a structured label helper:

```go
func NormalizeLabels(labels []*gh.Label) []db.Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]db.Label, 0, len(labels))
	for _, l := range labels {
		if l == nil || l.GetName() == "" {
			continue
		}
		out = append(out, db.Label{
			PlatformID:  l.GetID(),
			Name:        l.GetName(),
			Description: l.GetDescription(),
			Color:       l.GetColor(),
			IsDefault:   l.GetDefault(),
			UpdatedAt:   time.Now().UTC(),
		})
	}
	return out
}
```

In `internal/github/sync.go`, persist labels immediately after each item upsert:

```go
	mrID, err := s.db.UpsertMergeRequest(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert MR #%d: %w", ghPR.GetNumber(), err)
	}
	if err := s.db.ReplaceMergeRequestLabels(ctx, repoID, mrID, NormalizeLabels(ghPR.Labels)); err != nil {
		return fmt.Errorf("replace labels for MR #%d: %w", ghPR.GetNumber(), err)
	}
```

```go
	issueID, err := s.db.UpsertIssue(ctx, normalized)
	if err != nil {
		return fmt.Errorf("upsert issue #%d: %w", ghIssue.GetNumber(), err)
	}
	if err := s.db.ReplaceIssueLabels(ctx, repoID, issueID, NormalizeLabels(ghIssue.Labels)); err != nil {
		return fmt.Errorf("replace labels for issue #%d: %w", ghIssue.GetNumber(), err)
	}
```

Call the same `Replace*Labels` methods in these functions immediately after the item upsert succeeds:

- `indexUpsertMR`
- `fetchMRDetail`
- `syncMRWithHost`
- `syncOpenIssue`
- `SyncIssue`
- `fetchAndUpdateClosed`
- `fetchAndUpdateClosedIssue`
- `backfillRepo` PR page loop
- `backfillRepo` issue page loop

This keeps one invariant true across index sync, detail sync, closure refresh, and backfill: every successful item refresh replaces the join-table label set from the current GitHub payload.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/github -run 'TestNormalizeLabels|TestSync.*Labels' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/github/normalize.go internal/github/normalize_test.go internal/github/sync.go internal/github/sync_test.go
git commit -m "feat: sync normalized labels from github"
```

### Task 4: Expose Structured Labels In The API Contract

**Files:**
- Modify: `internal/server/api_types.go`
- Modify: `internal/server/api_test.go`
- Modify: `packages/ui/src/api/types.ts`
- Modify: `frontend/openapi/openapi.json`
- Modify: `internal/apiclient/spec/openapi.json`
- Modify: `internal/apiclient/generated/client.gen.go`
- Modify: `packages/ui/src/api/generated/schema.ts`
- Modify: `packages/ui/src/api/generated/client.ts`

- [ ] **Step 1: Write the failing API tests**

Add one test per endpoint shape in `internal/server/api_test.go`:

```go
func seedLabeledPR(t *testing.T, database *db.DB) {
	t.Helper()
	require := require.New(t)
	ctx := context.Background()
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	_, err = database.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID: repoID, PlatformID: 1, Number: 1,
		URL: "https://github.com/acme/widget/pull/1",
		Title: "Labeled PR", Author: "alice", State: "open",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
	})
	require.NoError(err)
	mrID, err := database.GetMRIDByRepoAndNumber(ctx, "acme", "widget", 1)
	require.NoError(err)
	require.NoError(database.ReplaceMergeRequestLabels(ctx, repoID, mrID, []db.Label{{
		PlatformID: 9,
		Name: "enhancement",
		Color: "a2eeef",
	}}))
}

func seedLabeledIssue(t *testing.T, database *db.DB) {
	t.Helper()
	require := require.New(t)
	ctx := context.Background()
	repoID, err := database.UpsertRepo(ctx, "github.com", "acme", "widget")
	require.NoError(err)
	issueID, err := database.UpsertIssue(ctx, &db.Issue{
		RepoID: repoID, PlatformID: 2, Number: 5,
		URL: "https://github.com/acme/widget/issues/5",
		Title: "Labeled issue", Author: "bob", State: "open",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
	})
	require.NoError(err)
	require.NoError(database.ReplaceIssueLabels(ctx, repoID, issueID, []db.Label{{
		PlatformID: 12,
		Name: "bug",
		Color: "d73a4a",
	}}))
}

func TestListPullsIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedLabeledPR(t, database)
	ctx := context.Background()
	client := setupTestClient(t, srv)
	resp, err := client.ListPulls(ctx, &generated.ListPullsParams{})
	require.NoError(err)
	require.Len(resp, 1)
	require.Len(resp[0].Labels, 1)
	require.Equal("enhancement", resp[0].Labels[0].Name)
}

func TestGetPullIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedLabeledPR(t, database)
	ctx := context.Background()

	client := setupTestClient(t, srv)
	resp, err := client.GetPull(ctx, "acme", "widget", 1)
	require.NoError(err)
	require.Len(resp.Labels, 1)
	require.Equal("enhancement", resp.Labels[0].Name)
}

func TestListIssuesIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedLabeledIssue(t, database)
	ctx := context.Background()

	client := setupTestClient(t, srv)
	resp, err := client.ListIssues(ctx, &generated.ListIssuesParams{})
	require.NoError(err)
	require.Len(resp, 1)
	require.Len(resp[0].Labels, 1)
	require.Equal("bug", resp[0].Labels[0].Name)
}

func TestGetIssueIncludesLabels(t *testing.T) {
	require := require.New(t)
	srv, database := setupTestServer(t)
	seedLabeledIssue(t, database)
	ctx := context.Background()

	client := setupTestClient(t, srv)
	resp, err := client.GetIssue(ctx, "acme", "widget", 5)
	require.NoError(err)
	require.Len(resp.Labels, 1)
	require.Equal("bug", resp.Labels[0].Name)
}
```

Use the same seeded repo, issue, and `Replace*Labels` helpers so every API shape is covered by a label assertion.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server -run 'TestListPullsIncludesLabels|TestGetPullIncludesLabels|TestListIssuesIncludesLabels|TestGetIssueIncludesLabels' -count=1`
Expected: FAIL because generated client/server types do not yet expose `labels`.

- [ ] **Step 3: Update API-facing types and regenerate artifacts**

In `packages/ui/src/api/types.ts`, replace the hand-written label interface with the generated schema type:

```ts
export type Label = components["schemas"]["Label"];
```

Ensure the Go API models use the updated DB structs with `Labels []Label` and `LabelsJSON` hidden from JSON output. If Huma needs an explicit schema anchor, add one in `internal/server/api_types.go`:

```go
type labelResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color"`
	IsDefault   bool   `json:"is_default"`
}
```

Then regenerate artifacts:

```bash
make api-generate
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server -run 'TestListPullsIncludesLabels|TestGetPullIncludesLabels|TestListIssuesIncludesLabels|TestGetIssueIncludesLabels' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/server/api_types.go internal/server/api_test.go internal/apiclient/spec/openapi.json internal/apiclient/generated/client.gen.go frontend/openapi/openapi.json packages/ui/src/api/generated/schema.ts packages/ui/src/api/generated/client.ts packages/ui/src/api/types.ts
git commit -m "feat: expose labels in api responses"
```

### Task 5: Render Shared GitHub-Style Label Pills In Svelte

**Files:**
- Create: `packages/ui/src/components/shared/GitHubLabels.svelte`
- Create: `packages/ui/src/components/shared/GitHubLabels.test.ts`
- Modify: `packages/ui/src/components/sidebar/PullItem.svelte`
- Modify: `packages/ui/src/components/sidebar/IssueItem.svelte`
- Modify: `packages/ui/src/components/detail/PullDetail.svelte`
- Modify: `packages/ui/src/components/detail/IssueDetail.svelte`

- [ ] **Step 1: Write the failing component tests**

Create `packages/ui/src/components/shared/GitHubLabels.test.ts`:

```ts
import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";
import GitHubLabels from "./GitHubLabels.svelte";
import type { Label } from "../../api/types.js";

const labels: Label[] = [
  { name: "bug", color: "d73a4a", description: "", is_default: true },
  { name: "needs-review", color: "fbca04", description: "", is_default: false },
  { name: "ux", color: "0052cc", description: "", is_default: false },
];

describe("GitHubLabels", () => {
  afterEach(() => cleanup());

  it("renders compact labels with overflow", () => {
    render(GitHubLabels, { props: { labels, mode: "compact", maxVisible: 2 } });
    expect(screen.getByText("bug")).toBeTruthy();
    expect(screen.getByText("needs-review")).toBeTruthy();
    expect(screen.getByText("+1")).toBeTruthy();
  });

  it("renders all labels in full mode", () => {
    render(GitHubLabels, { props: { labels, mode: "full" } });
    expect(screen.getByText("bug")).toBeTruthy();
    expect(screen.getByText("needs-review")).toBeTruthy();
    expect(screen.getByText("ux")).toBeTruthy();
  });

  it("uses dark text on light labels and light text on dark labels", () => {
    const { container } = render(GitHubLabels, {
      props: {
        labels: [
          { name: "light", color: "fbca04", description: "", is_default: false },
          { name: "dark", color: "0e4429", description: "", is_default: false },
        ],
        mode: "full",
      },
    });
    const pills = container.querySelectorAll(".label-pill");
    expect(pills[0]?.getAttribute("style")).toContain("color:#1f2328");
    expect(pills[1]?.getAttribute("style")).toContain("color:#ffffff");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && bun run test ../packages/ui/src/components/shared/GitHubLabels.test.ts`
Expected: FAIL because the shared component does not exist yet.

- [ ] **Step 3: Write the shared component and wire it into all four consumers**

Create `packages/ui/src/components/shared/GitHubLabels.svelte`:

```svelte
<script lang="ts">
  import type { Label } from "../../api/types.js";

  interface Props {
    labels: Label[];
    mode: "compact" | "full";
    maxVisible?: number;
  }

  const { labels, mode, maxVisible = 2 }: Props = $props();

  function normalizeHex(color: string): string {
    if (!color) return "#57606a";
    return color.startsWith("#") ? color : `#${color}`;
  }

  function textColor(hex: string): string {
    const value = normalizeHex(hex).slice(1);
    const r = parseInt(value.slice(0, 2), 16);
    const g = parseInt(value.slice(2, 4), 16);
    const b = parseInt(value.slice(4, 6), 16);
    const luminance = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
    return luminance > 0.6 ? "#1f2328" : "#ffffff";
  }

  const visible = $derived(mode === "compact" ? labels.slice(0, maxVisible) : labels);
  const overflow = $derived(mode === "compact" ? Math.max(0, labels.length - maxVisible) : 0);
</script>

{#if visible.length > 0}
  <div class:compact={mode === "compact"} class:full={mode === "full"} class="labels">
    {#each visible as label}
      {@const bg = normalizeHex(label.color)}
      <span class="label-pill" style={`background:${bg};color:${textColor(bg)};border-color:color-mix(in srgb, ${bg} 72%, #1f2328 28%);`}>
        {label.name}
      </span>
    {/each}
    {#if overflow > 0}
      <span class="label-overflow">+{overflow}</span>
    {/if}
  </div>
{/if}

<style>
  .labels { display: flex; align-items: center; gap: 4px; min-width: 0; }
  .labels.full { flex-wrap: wrap; }
  .labels.compact { overflow: hidden; }
  .label-pill {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    padding: 0 7px;
    border: 1px solid transparent;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 600;
    line-height: 18px;
    white-space: nowrap;
    text-overflow: ellipsis;
    overflow: hidden;
  }
  .label-overflow { font-size: 11px; color: var(--text-muted); flex-shrink: 0; }
</style>
```

Then update the four consumers:

```svelte
<script lang="ts">
  import GitHubLabels from "../shared/GitHubLabels.svelte";
</script>

{#if pr.Labels.length > 0}
  <GitHubLabels labels={pr.Labels} mode="compact" maxVisible={2} />
{/if}
```

```svelte
{#if issue.Labels.length > 0}
  <GitHubLabels labels={issue.Labels} mode="full" />
{/if}
```

Delete the local `parseLabels` and `labelColor` helpers from the issue components. Do not reintroduce JSON parsing in any Svelte file.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && bun run test ../packages/ui/src/components/shared/GitHubLabels.test.ts`
Expected: PASS.

- [ ] **Step 5: Run typecheck and focused UI tests**

Run: `cd frontend && bun run typecheck && bun run test ../packages/ui/src/components/detail/EventTimeline.test.ts ../packages/ui/src/components/shared/GitHubLabels.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add packages/ui/src/components/shared/GitHubLabels.svelte packages/ui/src/components/shared/GitHubLabels.test.ts packages/ui/src/components/sidebar/PullItem.svelte packages/ui/src/components/sidebar/IssueItem.svelte packages/ui/src/components/detail/PullDetail.svelte packages/ui/src/components/detail/IssueDetail.svelte
git commit -m "feat: render github-style labels in shared ui"
```

### Task 6: Final Verification

**Files:**
- Modify: none unless a failing verification exposes a defect

- [ ] **Step 1: Run database, sync, and server tests**

Run: `go test ./internal/db ./internal/github ./internal/server -count=1`
Expected: PASS.

- [ ] **Step 2: Run frontend tests and typecheck**

Run: `cd frontend && bun run typecheck && bun run test`
Expected: PASS.

- [ ] **Step 3: Regenerate and verify API artifacts are clean**

Run: `make api-generate && git diff --exit-code -- frontend/openapi/openapi.json internal/apiclient/spec/openapi.json internal/apiclient/generated/client.gen.go packages/ui/src/api/generated/schema.ts packages/ui/src/api/generated/client.ts`
Expected: command exits 0 with no diff.

- [ ] **Step 4: Build the frontend bundle**

Run: `make frontend`
Expected: PASS and `internal/web/dist` updated from the current frontend output.

- [ ] **Step 5: Create the final commit**

```bash
git add internal/db internal/github internal/server frontend/openapi/openapi.json internal/apiclient/spec/openapi.json internal/apiclient/generated/client.gen.go packages/ui/src/api packages/ui/src/components docs/superpowers/specs/2026-04-10-pr-issue-labels-design.md docs/superpowers/plans/2026-04-10-normalized-labels.md
git commit -m "feat: add normalized github labels to pulls and issues"
```
