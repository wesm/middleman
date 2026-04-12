# Stacked PRs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add stack-aware PR visualization with automatic detection from branch chains, a dedicated Stacks view, and a PR detail sidebar showing stack context.

**Architecture:** Detection runs after each sync via `SetOnSyncCompleted` callback. Two new DB tables (`middleman_stacks`, `middleman_stack_members`) store detected stacks. Two new API endpoints serve stack data. Frontend gets a new StacksStore and Stacks view, plus a sidebar panel on the PR detail view.

**Tech Stack:** Go (SQLite queries, Huma endpoints), Svelte 5 (runes-based stores, components), TypeScript (OpenAPI-generated client)

**Spec:** `docs/superpowers/specs/2026-04-09-stacked-prs-design.md`

---

### Task 1: Schema and DB Types

Add the two new tables and Go types for stacks.

**Files:**
- Modify: `internal/db/schema.sql` (append at end)
- Modify: `internal/db/types.go` (append new types)
- Modify: `internal/db/db_test.go` (verify tables created)

- [ ] **Step 1: Write failing test that stack tables exist**

```go
// internal/db/db_test.go — add to existing file

func TestStackTablesExist(t *testing.T) {
	d := openTestDB(t)
	tables := []string{"middleman_stacks", "middleman_stack_members"}
	for _, tbl := range tables {
		var name string
		err := d.ReadDB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		require.NoErrorf(t, err, "table %s should exist", tbl)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestStackTablesExist -v`
Expected: FAIL — tables don't exist yet

- [ ] **Step 3: Bump SchemaVersion**

In `internal/db/db.go`, change `const SchemaVersion = 3` to `const SchemaVersion = 4`. This forces existing databases to be recreated with the new tables.

- [ ] **Step 4: Add schema DDL**

Append to `internal/db/schema.sql`:

```sql
CREATE TABLE IF NOT EXISTS middleman_stacks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER NOT NULL REFERENCES middleman_repos(id),
    base_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS middleman_stack_members (
    stack_id INTEGER NOT NULL REFERENCES middleman_stacks(id) ON DELETE CASCADE,
    merge_request_id INTEGER NOT NULL REFERENCES middleman_merge_requests(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    PRIMARY KEY (stack_id, merge_request_id)
);

CREATE INDEX IF NOT EXISTS idx_stack_members_mr
    ON middleman_stack_members(merge_request_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_stacks_repo_base
    ON middleman_stacks(repo_id, base_number);
CREATE INDEX IF NOT EXISTS idx_stacks_repo
    ON middleman_stacks(repo_id);
```

- [ ] **Step 5: Add Go types**

Append to `internal/db/types.go`:

```go
// Stack represents a detected chain of dependent PRs.
type Stack struct {
	ID         int64
	RepoID     int64
	BaseNumber int
	Name       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// StackMember links a merge request to a stack with a position.
type StackMember struct {
	StackID        int64
	MergeRequestID int64
	Position       int
}

// StackWithRepo extends Stack with resolved repo owner/name.
type StackWithRepo struct {
	Stack
	RepoOwner string
	RepoName  string
}

// StackMemberWithPR combines stack membership with PR fields needed for display.
type StackMemberWithPR struct {
	StackID        int64
	MergeRequestID int64
	Position       int
	Number         int
	Title          string
	State          string
	CIStatus       string
	ReviewDecision string
	IsDraft        bool
}

// MergeRequestRow extends MergeRequest with optional stack fields for list queries.
type MergeRequestRow struct {
	MergeRequest
	StackID       *int64
	StackName     *string
	StackPosition *int
	StackSize     *int
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestStackTablesExist -v`
Expected: PASS

- [ ] **Step 7: Run full test suite**

Run: `go test ./internal/db/ -v`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
git add internal/db/schema.sql internal/db/types.go internal/db/db_test.go
git commit -m "feat: add stack tables and Go types for stacked PRs"
```

---

### Task 2: Stack Query Functions

Add all CRUD operations for stacks: detection queries, upsert, member replacement, listing, and per-PR lookup.

**Files:**
- Create: `internal/db/queries_stacks.go`
- Create: `internal/db/queries_stacks_test.go`

- [ ] **Step 1: Write test for ListPRsForStackDetection**

```go
// internal/db/queries_stacks_test.go

package db

import (
	"context"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestMRWithBranches(t *testing.T, d *DB, repoID int64, number int, head, base, state string) int64 {
	t.Helper()
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	mr := &MergeRequest{
		RepoID:         repoID,
		PlatformID:     repoID*10000 + int64(number),
		Number:         number,
		Title:          "PR " + head,
		Author:         "author",
		State:          state,
		HeadBranch:     head,
		BaseBranch:     base,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastActivityAt: now,
	}
	id, err := d.UpsertMergeRequest(context.Background(), mr)
	require.NoError(t, err)
	return id
}

func TestListPRsForStackDetection(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	// open PR — included
	insertTestMRWithBranches(t, d, repoID, 1, "feature/a", "main", "open")
	// merged PR — included
	insertTestMRWithBranches(t, d, repoID, 2, "feature/b", "feature/a", "merged")
	// closed PR — excluded
	insertTestMRWithBranches(t, d, repoID, 3, "feature/c", "main", "closed")

	prs, err := d.ListPRsForStackDetection(ctx, repoID)
	require.NoError(t, err)
	assert.Len(prs, 2)
	numbers := []int{prs[0].Number, prs[1].Number}
	assert.ElementsMatch([]int{1, 2}, numbers)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestListPRsForStackDetection -v`
Expected: FAIL — function doesn't exist

- [ ] **Step 3: Implement ListPRsForStackDetection**

```go
// internal/db/queries_stacks.go

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ListPRsForStackDetection returns non-closed PRs for a repo (open + merged).
func (d *DB) ListPRsForStackDetection(ctx context.Context, repoID int64) ([]MergeRequest, error) {
	rows, err := d.ro.QueryContext(ctx, `
		SELECT id, number, title, head_branch, base_branch, state, ci_status, review_decision
		FROM middleman_merge_requests
		WHERE repo_id = ? AND state IN ('open', 'merged')
		ORDER BY number`,
		repoID,
	)
	if err != nil {
		return nil, fmt.Errorf("list prs for stack detection: %w", err)
	}
	defer rows.Close()

	var prs []MergeRequest
	for rows.Next() {
		var mr MergeRequest
		mr.RepoID = repoID
		if err := rows.Scan(
			&mr.ID, &mr.Number, &mr.Title, &mr.HeadBranch, &mr.BaseBranch,
			&mr.State, &mr.CIStatus, &mr.ReviewDecision,
		); err != nil {
			return nil, fmt.Errorf("scan pr for stack detection: %w", err)
		}
		prs = append(prs, mr)
	}
	return prs, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestListPRsForStackDetection -v`
Expected: PASS

- [ ] **Step 5: Write test for UpsertStack and ReplaceStackMembers**

Add to `queries_stacks_test.go`:

```go
func TestUpsertStackAndReplaceMembers(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	mrID1 := insertTestMRWithBranches(t, d, repoID, 1, "feature/a", "main", "open")
	mrID2 := insertTestMRWithBranches(t, d, repoID, 2, "feature/b", "feature/a", "open")

	// Create stack (keyed by repo_id + base_number)
	stackID, err := d.UpsertStack(ctx, repoID, 1, "feature")
	require.NoError(t, err)
	assert.Greater(stackID, int64(0))

	// Idempotent upsert returns same ID
	stackID2, err := d.UpsertStack(ctx, repoID, 1, "feature")
	require.NoError(t, err)
	assert.Equal(stackID, stackID2)

	// Replace members
	members := []StackMember{
		{StackID: stackID, MergeRequestID: mrID1, Position: 1},
		{StackID: stackID, MergeRequestID: mrID2, Position: 2},
	}
	err = d.ReplaceStackMembers(ctx, stackID, members)
	require.NoError(t, err)

	// Verify via ListStacksWithMembers
	stacks, memberMap, err := d.ListStacksWithMembers(ctx, "")
	require.NoError(t, err)
	assert.Len(stacks, 1)
	assert.Equal("feature", stacks[0].Name)
	assert.Equal("org", stacks[0].RepoOwner)
	assert.Equal("repo", stacks[0].RepoName)
	assert.Len(memberMap[stackID], 2)
	assert.Equal(1, memberMap[stackID][0].Position)
	assert.Equal(2, memberMap[stackID][1].Position)
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestUpsertStackAndReplaceMembers -v`
Expected: FAIL

- [ ] **Step 7: Implement UpsertStack and ReplaceStackMembers**

Add to `queries_stacks.go`:

```go
// UpsertStack inserts or updates a stack keyed by (repo_id, base_number).
// Name is a display-only field updated on each detection cycle.
func (d *DB) UpsertStack(ctx context.Context, repoID int64, baseNumber int, name string) (int64, error) {
	res, err := d.rw.ExecContext(ctx, `
		INSERT INTO middleman_stacks (repo_id, base_number, name)
		VALUES (?, ?, ?)
		ON CONFLICT(repo_id, base_number) DO UPDATE SET
			name = excluded.name, updated_at = datetime('now')`,
		repoID, baseNumber, name,
	)
	if err != nil {
		return 0, fmt.Errorf("upsert stack: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		err = d.ro.QueryRowContext(ctx,
			`SELECT id FROM middleman_stacks WHERE repo_id = ? AND base_number = ?`,
			repoID, baseNumber,
		).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("get stack id: %w", err)
		}
	}
	return id, nil
}

// ReplaceStackMembers atomically replaces all members of a stack.
func (d *DB) ReplaceStackMembers(ctx context.Context, stackID int64, members []StackMember) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM middleman_stack_members WHERE stack_id = ?`, stackID,
		); err != nil {
			return fmt.Errorf("delete old stack members: %w", err)
		}
		if len(members) == 0 {
			return nil
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO middleman_stack_members (stack_id, merge_request_id, position)
			VALUES (?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare insert stack member: %w", err)
		}
		defer stmt.Close()
		for _, m := range members {
			if _, err := stmt.ExecContext(ctx, m.StackID, m.MergeRequestID, m.Position); err != nil {
				return fmt.Errorf("insert stack member: %w", err)
			}
		}
		return nil
	})
}
```

- [ ] **Step 8: Implement ListStacksWithMembers**

Add to `queries_stacks.go`:

```go
// ListStacksWithMembers returns stacks with repo info and their members.
func (d *DB) ListStacksWithMembers(ctx context.Context, repoFilter string) ([]StackWithRepo, map[int64][]StackMemberWithPR, error) {
	var conds []string
	var args []any
	if repoFilter != "" {
		parts := strings.SplitN(repoFilter, "/", 2)
		if len(parts) == 2 {
			conds = append(conds, "r.owner = ? AND r.name = ?")
			args = append(args, parts[0], parts[1])
		}
	}
	// Only return stacks that have at least one open member.
	conds = append(conds, `EXISTS (
		SELECT 1 FROM middleman_stack_members sm2
		JOIN middleman_merge_requests p2 ON p2.id = sm2.merge_request_id
		WHERE sm2.stack_id = s.id AND p2.state = 'open')`)

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	stackQuery := fmt.Sprintf(`
		SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at,
		       r.owner, r.name
		FROM middleman_stacks s
		JOIN middleman_repos r ON r.id = s.repo_id
		%s
		ORDER BY s.updated_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, stackQuery, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("list stacks: %w", err)
	}
	defer rows.Close()

	var stacks []StackWithRepo
	var stackIDs []int64
	for rows.Next() {
		var s StackWithRepo
		if err := rows.Scan(
			&s.ID, &s.RepoID, &s.BaseNumber, &s.Name, &s.CreatedAt, &s.UpdatedAt,
			&s.RepoOwner, &s.RepoName,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack: %w", err)
		}
		stacks = append(stacks, s)
		stackIDs = append(stackIDs, s.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(stackIDs) == 0 {
		return stacks, make(map[int64][]StackMemberWithPR), nil
	}

	// Fetch members for all stacks.
	placeholders := make([]string, len(stackIDs))
	memberArgs := make([]any, len(stackIDs))
	for i, id := range stackIDs {
		placeholders[i] = "?"
		memberArgs[i] = id
	}
	memberQuery := `
		SELECT sm.stack_id, sm.merge_request_id, sm.position,
		       p.number, p.title, p.state, p.ci_status, p.review_decision, p.is_draft
		FROM middleman_stack_members sm
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		WHERE sm.stack_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY sm.stack_id, sm.position`

	mRows, err := d.ro.QueryContext(ctx, memberQuery, memberArgs...)
	if err != nil {
		return nil, nil, fmt.Errorf("list stack members: %w", err)
	}
	defer mRows.Close()

	memberMap := make(map[int64][]StackMemberWithPR)
	for mRows.Next() {
		var m StackMemberWithPR
		if err := mRows.Scan(
			&m.StackID, &m.MergeRequestID, &m.Position,
			&m.Number, &m.Title, &m.State, &m.CIStatus, &m.ReviewDecision, &m.IsDraft,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack member: %w", err)
		}
		memberMap[m.StackID] = append(memberMap[m.StackID], m)
	}
	return stacks, memberMap, mRows.Err()
}
```

- [ ] **Step 9: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestUpsertStackAndReplaceMembers -v`
Expected: PASS

- [ ] **Step 10: Write test for DeleteStaleStacks**

Add to `queries_stacks_test.go`:

```go
func TestDeleteStaleStacks(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	id1, err := d.UpsertStack(ctx, repoID, 1, "keep")
	require.NoError(t, err)
	_, err = d.UpsertStack(ctx, repoID, 2, "delete-me")
	require.NoError(t, err)

	err = d.DeleteStaleStacks(ctx, repoID, []int64{id1})
	require.NoError(t, err)

	stacks, _, err := d.ListStacksWithMembers(ctx, "")
	require.NoError(t, err)
	// "keep" has no open members so it won't appear in ListStacksWithMembers.
	// Verify directly that "delete-me" is gone.
	var count int
	err = d.ReadDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM middleman_stacks WHERE repo_id = ?`, repoID,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(1, count) // only "keep" remains
}
```

- [ ] **Step 11: Implement DeleteStaleStacks**

Add to `queries_stacks.go`:

```go
// DeleteStaleStacks removes stacks for a repo that are not in the active set.
func (d *DB) DeleteStaleStacks(ctx context.Context, repoID int64, activeStackIDs []int64) error {
	if len(activeStackIDs) == 0 {
		_, err := d.rw.ExecContext(ctx,
			`DELETE FROM middleman_stacks WHERE repo_id = ?`, repoID)
		if err != nil {
			return fmt.Errorf("delete all stacks for repo: %w", err)
		}
		return nil
	}
	placeholders := make([]string, len(activeStackIDs))
	args := make([]any, 0, len(activeStackIDs)+1)
	args = append(args, repoID)
	for i, id := range activeStackIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := d.rw.ExecContext(ctx,
		`DELETE FROM middleman_stacks WHERE repo_id = ? AND id NOT IN (`+
			strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("delete stale stacks: %w", err)
	}
	return nil
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestDeleteStaleStacks -v`
Expected: PASS

- [ ] **Step 13: Write test for GetStackForPR**

Add to `queries_stacks_test.go`:

```go
func TestGetStackForPR(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()
	repoID := insertTestRepo(t, d, "org", "repo")

	mrID1 := insertTestMRWithBranches(t, d, repoID, 10, "feature/a", "main", "open")
	mrID2 := insertTestMRWithBranches(t, d, repoID, 11, "feature/b", "feature/a", "open")

	stackID, err := d.UpsertStack(ctx, repoID, 10, "feature")
	require.NoError(t, err)
	err = d.ReplaceStackMembers(ctx, stackID, []StackMember{
		{StackID: stackID, MergeRequestID: mrID1, Position: 1},
		{StackID: stackID, MergeRequestID: mrID2, Position: 2},
	})
	require.NoError(t, err)

	// Found
	stack, members, err := d.GetStackForPR(ctx, "org", "repo", 10)
	require.NoError(t, err)
	require.NotNil(t, stack)
	assert.Equal("feature", stack.Name)
	assert.Len(members, 2)

	// Not found
	stack2, _, err := d.GetStackForPR(ctx, "org", "repo", 999)
	require.NoError(t, err)
	assert.Nil(stack2)
}
```

- [ ] **Step 14: Implement GetStackForPR**

Add to `queries_stacks.go`:

```go
// GetStackForPR returns the stack and members for a specific PR, or nil if not in a stack.
func (d *DB) GetStackForPR(ctx context.Context, owner, name string, number int) (*Stack, []StackMemberWithPR, error) {
	var stack Stack
	err := d.ro.QueryRowContext(ctx, `
		SELECT s.id, s.repo_id, s.base_number, s.name, s.created_at, s.updated_at
		FROM middleman_stacks s
		JOIN middleman_stack_members sm ON sm.stack_id = s.id
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		JOIN middleman_repos r ON r.id = p.repo_id
		WHERE r.owner = ? AND r.name = ? AND p.number = ?`,
		owner, name, number,
	).Scan(&stack.ID, &stack.RepoID, &stack.BaseNumber, &stack.Name, &stack.CreatedAt, &stack.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get stack for pr: %w", err)
	}

	rows, err := d.ro.QueryContext(ctx, `
		SELECT sm.stack_id, sm.merge_request_id, sm.position,
		       p.number, p.title, p.state, p.ci_status, p.review_decision, p.is_draft
		FROM middleman_stack_members sm
		JOIN middleman_merge_requests p ON p.id = sm.merge_request_id
		WHERE sm.stack_id = ?
		ORDER BY sm.position`, stack.ID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get stack members: %w", err)
	}
	defer rows.Close()

	var members []StackMemberWithPR
	for rows.Next() {
		var m StackMemberWithPR
		if err := rows.Scan(
			&m.StackID, &m.MergeRequestID, &m.Position,
			&m.Number, &m.Title, &m.State, &m.CIStatus, &m.ReviewDecision, &m.IsDraft,
		); err != nil {
			return nil, nil, fmt.Errorf("scan stack member: %w", err)
		}
		members = append(members, m)
	}
	return &stack, members, rows.Err()
}
```

- [ ] **Step 15: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestGetStackForPR -v`
Expected: PASS

- [ ] **Step 16: Run full DB test suite**

Run: `go test ./internal/db/ -v`
Expected: All PASS

- [ ] **Step 17: Commit**

```bash
git add internal/db/queries_stacks.go internal/db/queries_stacks_test.go
git commit -m "feat: add stack query functions with tests"
```

---

### Task 3: Stack Detection Engine

Implement the detection algorithm that runs after each sync cycle. This is the core logic that walks branch chains and creates/updates stacks.

**Files:**
- Create: `internal/stacks/detect.go`
- Create: `internal/stacks/detect_test.go`
- Modify: `cmd/middleman/main.go` (wire up callback — small change at end)

- [ ] **Step 1: Write test for basic linear chain detection**

```go
// internal/stacks/detect_test.go

package stacks

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/wesm/middleman/internal/db"
)

func makePR(id int64, number int, head, base, state string) db.MergeRequest {
	return db.MergeRequest{
		ID:         id,
		Number:     number,
		Title:      "PR " + head,
		HeadBranch: head,
		BaseBranch: base,
		State:      state,
	}
}

func TestDetectChains_LinearStack(t *testing.T) {
	assert := Assert.New(t)
	prs := []db.MergeRequest{
		makePR(1, 100, "feature/auth-token", "main", "open"),
		makePR(2, 101, "feature/auth-retry", "feature/auth-token", "open"),
		makePR(3, 102, "feature/auth-ui", "feature/auth-retry", "open"),
	}

	chains := DetectChains(prs)
	assert.Len(chains, 1)
	assert.Len(chains[0], 3)
	assert.Equal(100, chains[0][0].Number) // base
	assert.Equal(101, chains[0][1].Number)
	assert.Equal(102, chains[0][2].Number) // tip
}

func TestDetectChains_SinglePRNotAStack(t *testing.T) {
	assert := Assert.New(t)
	prs := []db.MergeRequest{
		makePR(1, 100, "feature/solo", "main", "open"),
	}
	chains := DetectChains(prs)
	assert.Len(chains, 0)
}

func TestDetectChains_ForkPicksLowestNumber(t *testing.T) {
	assert := Assert.New(t)
	prs := []db.MergeRequest{
		makePR(1, 100, "feature/base", "main", "open"),
		makePR(2, 102, "feature/child-b", "feature/base", "open"),
		makePR(3, 101, "feature/child-a", "feature/base", "open"),
	}

	chains := DetectChains(prs)
	assert.Len(chains, 1)
	assert.Len(chains[0], 2)
	assert.Equal(100, chains[0][0].Number)
	assert.Equal(101, chains[0][1].Number) // lowest number wins
}

func TestDetectChains_CycleSkipped(t *testing.T) {
	assert := Assert.New(t)
	prs := []db.MergeRequest{
		makePR(1, 100, "branch-a", "branch-b", "open"),
		makePR(2, 101, "branch-b", "branch-a", "open"),
	}
	chains := DetectChains(prs)
	assert.Len(chains, 0)
}

func TestDetectChains_PartialMerge(t *testing.T) {
	assert := Assert.New(t)
	prs := []db.MergeRequest{
		makePR(1, 100, "feature/a", "main", "merged"),
		makePR(2, 101, "feature/b", "feature/a", "open"),
	}
	chains := DetectChains(prs)
	assert.Len(chains, 1)
	assert.Len(chains[0], 2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/stacks/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement DetectChains**

```go
// internal/stacks/detect.go

package stacks

import (
	"sort"

	"github.com/wesm/middleman/internal/db"
)

// DetectChains finds linear PR chains from branch metadata.
// Returns chains of length >= 2, ordered base-to-tip.
func DetectChains(prs []db.MergeRequest) [][]db.MergeRequest {
	// Sort by number for deterministic tie-breaking.
	sorted := make([]db.MergeRequest, len(prs))
	copy(sorted, prs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Number < sorted[j].Number
	})

	// head_branch -> PR (first by number wins for duplicates).
	headMap := make(map[string]db.MergeRequest, len(sorted))
	for _, pr := range sorted {
		if _, exists := headMap[pr.HeadBranch]; !exists {
			headMap[pr.HeadBranch] = pr
		}
	}

	// base_branch -> []PR (children targeting that base).
	childMap := make(map[string][]db.MergeRequest)
	for _, pr := range sorted {
		childMap[pr.BaseBranch] = append(childMap[pr.BaseBranch], pr)
	}

	// Find bases: PRs whose base_branch is NOT in headMap.
	var bases []db.MergeRequest
	for _, pr := range sorted {
		if _, isHead := headMap[pr.BaseBranch]; !isHead {
			bases = append(bases, pr)
		}
	}

	// Walk chains from each base.
	var chains [][]db.MergeRequest
	for _, base := range bases {
		chain := walkChain(base, childMap, headMap)
		if len(chain) >= 2 {
			chains = append(chains, chain)
		}
	}

	return chains
}

func walkChain(
	start db.MergeRequest,
	childMap map[string][]db.MergeRequest,
	headMap map[string]db.MergeRequest,
) []db.MergeRequest {
	visited := make(map[string]bool)
	var chain []db.MergeRequest
	current := start

	for {
		if visited[current.HeadBranch] {
			return nil // cycle
		}
		visited[current.HeadBranch] = true
		chain = append(chain, current)

		children := childMap[current.HeadBranch]
		if len(children) == 0 {
			break
		}
		// Pick lowest PR number child (already sorted).
		current = children[0]
	}

	return chain
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/stacks/ -v`
Expected: All PASS

- [ ] **Step 5: Write test for DeriveStackName**

Add to `detect_test.go`:

```go
func TestDeriveStackName(t *testing.T) {
	assert := Assert.New(t)

	// Common prefix on token boundary
	assert.Equal("auth", DeriveStackName([]db.MergeRequest{
		makePR(1, 1, "feature/auth-fix", "main", "open"),
		makePR(2, 2, "feature/auth-retry", "feature/auth-fix", "open"),
	}))

	// No common prefix — falls back to base PR title
	assert.Equal("PR branch-x", DeriveStackName([]db.MergeRequest{
		makePR(1, 1, "branch-x", "main", "open"),
		makePR(2, 2, "other-y", "branch-x", "open"),
	}))

	// Partial word boundary rejected
	assert.Equal("PR feature/authorization", DeriveStackName([]db.MergeRequest{
		makePR(1, 1, "feature/authorization", "main", "open"),
		makePR(2, 2, "feature/authorizer", "feature/authorization", "open"),
	}))
}
```

- [ ] **Step 6: Implement DeriveStackName**

Add to `detect.go`:

```go
import "strings"

var conventionalPrefixes = []string{
	"feature/", "feat/", "fix/", "bugfix/",
	"hotfix/", "chore/", "refactor/", "docs/",
}

// DeriveStackName computes a stack name from branch names.
func DeriveStackName(chain []db.MergeRequest) string {
	if len(chain) == 0 {
		return ""
	}
	branches := make([]string, len(chain))
	for i, pr := range chain {
		b := pr.HeadBranch
		for _, prefix := range conventionalPrefixes {
			b = strings.TrimPrefix(b, prefix)
		}
		branches[i] = b
	}

	prefix := tokenBoundaryPrefix(branches)
	if prefix != "" {
		return prefix
	}
	return chain[0].Title
}

func tokenBoundaryPrefix(names []string) string {
	if len(names) < 2 {
		return ""
	}
	prefix := names[0]
	for _, name := range names[1:] {
		prefix = commonPrefix(prefix, name)
		if prefix == "" {
			return ""
		}
	}
	// Trim to last token boundary.
	separators := "/-_"
	trimmed := strings.TrimRight(prefix, separators)
	if trimmed == "" {
		return ""
	}
	// Verify we stopped at a boundary, not mid-word.
	for _, name := range names {
		if len(name) > len(trimmed) {
			next := name[len(trimmed)]
			if !strings.ContainsRune(separators, rune(next)) {
				return ""
			}
		}
	}
	return trimmed
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/stacks/ -run TestDeriveStackName -v`
Expected: PASS

- [ ] **Step 8: Write test for RunDetection (integration with DB)**

Add to `detect_test.go`:

```go
import (
	"context"
	"os"
	"path/filepath"

	realdb "github.com/wesm/middleman/internal/db"
)

func openTestDB(t *testing.T) *realdb.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := realdb.Open(path)
	require.NoError(t, err, "open test db")
	t.Cleanup(func() { d.Close() })
	return d
}

func TestRunDetection(t *testing.T) {
	assert := Assert.New(t)
	d := openTestDB(t)
	ctx := context.Background()

	repoID, err := d.UpsertRepo(ctx, "", "org", "repo")
	require.NoError(t, err)

	// Create a 3-PR chain.
	now := time.Now()
	for i, pr := range []struct{ num int; head, base string }{
		{100, "feature/auth", "main"},
		{101, "feature/auth-retry", "feature/auth"},
		{102, "feature/auth-ui", "feature/auth-retry"},
	} {
		_, err := d.UpsertMergeRequest(ctx, &realdb.MergeRequest{
			RepoID: repoID, PlatformID: int64(i + 1), Number: pr.num,
			Title: "PR " + pr.head, Author: "a", State: "open",
			HeadBranch: pr.head, BaseBranch: pr.base,
			CreatedAt: now, UpdatedAt: now, LastActivityAt: now,
		})
		require.NoError(t, err)
	}

	err = RunDetection(ctx, d, repoID)
	assert.NoError(err)

	stack, members, err := d.GetStackForPR(ctx, "org", "repo", 101)
	assert.NoError(err)
	assert.NotNil(stack)
	assert.Equal("auth", stack.Name)
	assert.Len(members, 3)
	assert.Equal(1, members[0].Position)
	assert.Equal(100, members[0].Number)
}
```

- [ ] **Step 9: Implement RunDetection**

Add to `detect.go`:

```go
import "context"

// RunDetection detects stacks for a single repo and persists results.
func RunDetection(ctx context.Context, database *db.DB, repoID int64) error {
	prs, err := database.ListPRsForStackDetection(ctx, repoID)
	if err != nil {
		return err
	}

	chains := DetectChains(prs)

	var activeIDs []int64
	for _, chain := range chains {
		name := DeriveStackName(chain)
		baseNumber := chain[0].Number
		stackID, err := database.UpsertStack(ctx, repoID, baseNumber, name)
		if err != nil {
			return err
		}
		activeIDs = append(activeIDs, stackID)

		members := make([]db.StackMember, len(chain))
		for i, pr := range chain {
			members[i] = db.StackMember{
				StackID:        stackID,
				MergeRequestID: pr.ID,
				Position:       i + 1,
			}
		}
		if err := database.ReplaceStackMembers(ctx, stackID, members); err != nil {
			return err
		}
	}

	return database.DeleteStaleStacks(ctx, repoID, activeIDs)
}
```

- [ ] **Step 10: Run all stacks tests**

Run: `go test ./internal/stacks/ -v`
Expected: All PASS

Note: Add `"time"` to import block for TestRunDetection.

- [ ] **Step 11: Commit**

```bash
git add internal/stacks/
git commit -m "feat: add stack detection engine with chain walking and naming"
```

---

### Task 4: Wire Detection into Sync + API Endpoints

Hook detection into the sync callback, add the two new API endpoints, and modify the pulls list to include stack fields.

**Files:**
- Modify: `cmd/middleman/main.go` (add import, register callback)
- Modify: `internal/server/api_types.go` (add response types)
- Modify: `internal/server/huma_routes.go` (add endpoints, modify listPulls)
- Modify: `internal/db/queries.go` (add stack JOIN to ListMergeRequests)

- [ ] **Step 1: Add stack response types to api_types.go**

Add to `internal/server/api_types.go`:

```go
type stackMemberResponse struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	CIStatus       string `json:"ci_status"`
	ReviewDecision string `json:"review_decision"`
	Position       int    `json:"position"`
	IsDraft        bool   `json:"is_draft"`
	BlockedBy      *int   `json:"blocked_by"`
}

type stackResponse struct {
	ID        int64                 `json:"id"`
	Name      string                `json:"name"`
	RepoOwner string                `json:"repo_owner"`
	RepoName  string                `json:"repo_name"`
	Health    string                `json:"health"`
	Members   []stackMemberResponse `json:"members"`
}

type stackContextResponse struct {
	StackID   int64                 `json:"stack_id"`
	StackName string                `json:"stack_name"`
	Position  int                   `json:"position"`
	Size      int                   `json:"size"`
	Health    string                `json:"health"`
	Members   []stackMemberResponse `json:"members"`
}
```

- [ ] **Step 2: Add stack fields to mergeRequestResponse**

Modify `mergeRequestResponse` in `api_types.go`:

```go
type mergeRequestResponse struct {
	db.MergeRequest
	RepoOwner       string                 `json:"repo_owner"`
	RepoName        string                 `json:"repo_name"`
	WorktreeLinks   []worktreeLinkResponse `json:"worktree_links"`
	DetailLoaded    bool                   `json:"detail_loaded"`
	DetailFetchedAt string                 `json:"detail_fetched_at,omitempty"`
	StackID         *int64                 `json:"stack_id"`
	StackName       *string                `json:"stack_name"`
	StackPosition   *int                   `json:"stack_position"`
	StackSize       *int                   `json:"stack_size"`
}
```

- [ ] **Step 3: Add health computation helper**

Create `internal/server/stack_health.go`:

```go
package server

import "github.com/wesm/middleman/internal/db"

func computeStackHealth(members []db.StackMemberWithPR) string {
	if len(members) == 0 {
		return "in_progress"
	}

	hasMerged := false
	allGreen := true
	hasBlocker := false
	lowestOpenIdx := -1

	for i, m := range members {
		if m.State == "merged" {
			hasMerged = true
			continue
		}
		if lowestOpenIdx == -1 {
			lowestOpenIdx = i
		}

		isGreen := m.CIStatus == "success" && m.ReviewDecision == "APPROVED"
		if !isGreen {
			allGreen = false
		}

		isBlocked := m.CIStatus == "failure" || m.ReviewDecision == "CHANGES_REQUESTED"
		if isBlocked {
			// Check if has descendants
			hasDescendant := false
			for j := i + 1; j < len(members); j++ {
				if members[j].State != "merged" {
					hasDescendant = true
					break
				}
			}
			if hasDescendant {
				hasBlocker = true
			}
		}
	}

	switch {
	case hasBlocker:
		return "blocked"
	case hasMerged:
		return "partial_merge"
	case allGreen:
		return "all_green"
	case lowestOpenIdx >= 0:
		m := members[lowestOpenIdx]
		if m.CIStatus == "success" && m.ReviewDecision == "APPROVED" {
			return "base_ready"
		}
	}
	return "in_progress"
}

func computeBlockedBy(members []db.StackMemberWithPR) map[int]int {
	blockedBy := make(map[int]int)
	var rootBlocker int
	for _, m := range members {
		if m.State == "merged" {
			continue
		}
		isBlocked := m.CIStatus == "failure" || m.ReviewDecision == "CHANGES_REQUESTED"
		if isBlocked && rootBlocker == 0 {
			rootBlocker = m.Number
		} else if rootBlocker != 0 && m.Number != rootBlocker {
			// Cascade: all descendants after a blocker are blocked, per spec.
			blockedBy[m.Number] = rootBlocker
		}
	}
	return blockedBy
}

func toStackMemberResponses(members []db.StackMemberWithPR) []stackMemberResponse {
	blocked := computeBlockedBy(members)
	out := make([]stackMemberResponse, len(members))
	for i, m := range members {
		out[i] = stackMemberResponse{
			Number:         m.Number,
			Title:          m.Title,
			State:          m.State,
			CIStatus:       m.CIStatus,
			ReviewDecision: m.ReviewDecision,
			Position:       m.Position,
			IsDraft:        m.IsDraft,
		}
		if b, ok := blocked[m.Number]; ok {
			out[i].BlockedBy = &b
		}
	}
	return out
}
```

- [ ] **Step 3b: Write tests for health computation**

Create `internal/server/stack_health_test.go`:

```go
package server

import (
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/wesm/middleman/internal/db"
)

func member(number, pos int, state, ci, review string) db.StackMemberWithPR {
	return db.StackMemberWithPR{
		Number: number, Position: pos, State: state,
		CIStatus: ci, ReviewDecision: review,
	}
}

func TestComputeStackHealth(t *testing.T) {
	tests := []struct {
		name    string
		members []db.StackMemberWithPR
		want    string
	}{
		{"empty", nil, "in_progress"},
		{"all green", []db.StackMemberWithPR{
			member(1, 1, "open", "success", "APPROVED"),
			member(2, 2, "open", "success", "APPROVED"),
		}, "all_green"},
		{"blocked by failing CI with descendant", []db.StackMemberWithPR{
			member(1, 1, "open", "failure", ""),
			member(2, 2, "open", "success", "APPROVED"),
		}, "blocked"},
		{"partial merge", []db.StackMemberWithPR{
			member(1, 1, "merged", "success", "APPROVED"),
			member(2, 2, "open", "success", "APPROVED"),
		}, "partial_merge"},
		{"base ready", []db.StackMemberWithPR{
			member(1, 1, "open", "success", "APPROVED"),
			member(2, 2, "open", "pending", ""),
		}, "base_ready"},
		{"in progress", []db.StackMemberWithPR{
			member(1, 1, "open", "pending", ""),
			member(2, 2, "open", "pending", ""),
		}, "in_progress"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Assert.Equal(t, tt.want, computeStackHealth(tt.members))
		})
	}
}

func TestComputeBlockedBy(t *testing.T) {
	assert := Assert.New(t)

	// No blockers
	members := []db.StackMemberWithPR{
		member(1, 1, "open", "success", "APPROVED"),
		member(2, 2, "open", "success", "APPROVED"),
	}
	assert.Empty(computeBlockedBy(members))

	// #1 blocks #2 and #3 (transitive cascade)
	members = []db.StackMemberWithPR{
		member(1, 1, "open", "failure", ""),
		member(2, 2, "open", "success", "APPROVED"),
		member(3, 3, "open", "success", "APPROVED"),
	}
	blocked := computeBlockedBy(members)
	assert.Equal(1, blocked[2])
	assert.Equal(1, blocked[3])

	// Merged PRs skipped, blocker is #2
	members = []db.StackMemberWithPR{
		member(1, 1, "merged", "success", "APPROVED"),
		member(2, 2, "open", "failure", ""),
		member(3, 3, "open", "success", ""),
	}
	blocked = computeBlockedBy(members)
	assert.Equal(2, blocked[3])
	assert.NotContains(blocked, 1)

	// CHANGES_REQUESTED also triggers blocking
	members = []db.StackMemberWithPR{
		member(1, 1, "open", "success", "CHANGES_REQUESTED"),
		member(2, 2, "open", "success", "APPROVED"),
	}
	blocked = computeBlockedBy(members)
	assert.Equal(1, blocked[2])
}
```

- [ ] **Step 3c: Run health tests**

Run: `go test ./internal/server/ -run "TestComputeStack|TestComputeBlockedBy" -v`
Expected: All PASS

- [ ] **Step 4: Add Huma input/output types and register endpoints**

Add to `huma_routes.go` (new types near top, register in `registerAPI`, implement handlers):

```go
// Types
type listStacksInput struct {
	Repo string `query:"repo"`
}
type listStacksOutput struct {
	Body []stackResponse
}
type getStackForPROutput struct {
	Body stackContextResponse
}

// In registerAPI, add:
huma.Get(api, "/stacks", s.listStacks)
huma.Get(api, "/repos/{owner}/{name}/pulls/{number}/stack", s.getStackForPR)
```

Handler implementations:

```go
func (s *Server) listStacks(ctx context.Context, input *listStacksInput) (*listStacksOutput, error) {
	stacks, memberMap, err := s.db.ListStacksWithMembers(ctx, input.Repo)
	if err != nil {
		return nil, huma.Error500InternalServerError("list stacks failed")
	}

	out := make([]stackResponse, 0, len(stacks))
	for _, st := range stacks {
		members := memberMap[st.ID]
		out = append(out, stackResponse{
			ID:        st.ID,
			Name:      st.Name,
			RepoOwner: st.RepoOwner,
			RepoName:  st.RepoName,
			Health:    computeStackHealth(members),
			Members:   toStackMemberResponses(members),
		})
	}

	return &listStacksOutput{Body: out}, nil
}

func (s *Server) getStackForPR(ctx context.Context, input *repoNumberInput) (*getStackForPROutput, error) {
	stack, members, err := s.db.GetStackForPR(ctx, input.Owner, input.Name, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get stack for pr failed")
	}
	if stack == nil {
		return nil, huma.Error404NotFound("PR is not part of a stack")
	}

	// Find current PR's position
	var position int
	for _, m := range members {
		if m.Number == input.Number {
			position = m.Position
			break
		}
	}

	return &getStackForPROutput{
		Body: stackContextResponse{
			StackID:   stack.ID,
			StackName: stack.Name,
			Position:  position,
			Size:      len(members),
			Health:    computeStackHealth(members),
			Members:   toStackMemberResponses(members),
		},
	}, nil
}
```

- [ ] **Step 5: Modify ListMergeRequests to include stack fields**

In `internal/db/queries.go`, modify `ListMergeRequests`:

a) Change return type from `[]MergeRequest` to `[]MergeRequestRow`.

b) Add LEFT JOINs after the starred join (after the `LEFT JOIN middleman_starred_items` line):
```sql
LEFT JOIN middleman_stack_members sm ON sm.merge_request_id = p.id
LEFT JOIN middleman_stacks st ON st.id = sm.stack_id
```

c) Add to SELECT (after `(s.number IS NOT NULL) AS starred`):
```sql
st.id, st.name, sm.position,
CASE WHEN sm.stack_id IS NOT NULL
     THEN (SELECT COUNT(*) FROM middleman_stack_members WHERE stack_id = sm.stack_id)
     ELSE NULL END
```

Note: The `CASE WHEN` ensures NULL (not 0) for PRs not in a stack, matching `*int` semantics in `MergeRequestRow.StackSize`.

d) Change `var mrs []MergeRequest` to `var mrs []MergeRequestRow` and `var mr MergeRequest` to `var mr MergeRequestRow`.

e) Extend the Scan call — add these 4 fields after `&mr.Starred`:
```go
&mr.StackID, &mr.StackName, &mr.StackPosition, &mr.StackSize,
```

f) Update all callers of `ListMergeRequests` that assign to `[]MergeRequest` — must become `[]MergeRequestRow`. The embedded `MergeRequest` fields are still accessible (e.g., `prs[0].Number` still works). Files to update:
- `internal/db/queries_test.go`: `TestListMergeRequests_Order`, `TestListMergeRequests_RepoFilter`, `TestListMergeRequests_Search`, `TestListMergeRequests_KanbanFilter`
- `internal/testutil/fixtures_test.go`: 4 calls (`allPRs`, `openPRs`, `mergedPRs`, `closedPRs`)

- [ ] **Step 6: Update listPulls handler to use MergeRequestRow**

In `huma_routes.go`, update `listPulls` handler: `ListMergeRequests` now returns `[]MergeRequestRow`. Change `var mr MergeRequest` references to use `mr.MergeRequest` for the embedded struct, and add stack fields to the response. The full mapping becomes:

```go
resp := mergeRequestResponse{
    MergeRequest:  mr.MergeRequest,
    RepoOwner:     rp.Owner,
    RepoName:      rp.Name,
    WorktreeLinks: wl,
    DetailLoaded:  mr.DetailFetchedAt != nil,
    StackID:       mr.StackID,
    StackName:     mr.StackName,
    StackPosition: mr.StackPosition,
    StackSize:     mr.StackSize,
}
if mr.DetailFetchedAt != nil {
    resp.DetailFetchedAt = mr.DetailFetchedAt.UTC().Format(time.RFC3339)
}
```

- [ ] **Step 7: Wire detection into sync callback**

Stack detection must run after sync in both the CLI (`cmd/middleman/main.go`) and the library path (`middleman.go`). Wire it at two levels:

**a) Create a helper in `internal/stacks/hook.go`:**

```go
package stacks

import (
	"context"
	"log/slog"

	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// SyncCompletedHook returns a callback for Syncer.SetOnSyncCompleted
// that runs stack detection for each synced repo. ctx controls the
// lifecycle — detection stops if ctx is canceled (e.g. during shutdown).
// If next is non-nil, it is called after detection completes.
func SyncCompletedHook(ctx context.Context, database *db.DB, next func([]ghclient.RepoSyncResult)) func([]ghclient.RepoSyncResult) {
	return func(results []ghclient.RepoSyncResult) {
		defer func() {
			if next != nil {
				next(results)
			}
		}()
		for _, result := range results {
			if ctx.Err() != nil {
				return
			}
			if result.Error != "" {
				continue
			}
			repo, err := database.GetRepoByOwnerName(ctx, result.Owner, result.Name)
			if err != nil || repo == nil {
				continue
			}
			if err := RunDetection(ctx, database, repo.ID); err != nil {
				slog.Error("stack detection failed",
					"repo", result.Owner+"/"+result.Name, "err", err)
			}
		}
	}
}
```

**b) In `cmd/middleman/main.go`**, add `"github.com/wesm/middleman/internal/stacks"` to imports. Insert **after** `ctx, stop := signal.NotifyContext(...)` (line ~185), **before** `syncer.Start(ctx)` (line ~192). Note: `ctx` is created via `signal.NotifyContext` well after the syncer, so the hook must be registered here:

```go
syncer.SetOnSyncCompleted(stacks.SyncCompletedHook(ctx, database, nil))
```

**c) In `middleman.go`**, replace the existing `OnSyncCompleted` wiring block (the `if opts.EmbedHooks.OnSyncCompleted != nil { ... }` block that converts `ghclient.RepoSyncResult` to public `RepoSyncResult`) with:

```go
// Build adapter for embed hook if present.
var embedNext func([]ghclient.RepoSyncResult)
if opts.EmbedHooks.OnSyncCompleted != nil {
    cb := opts.EmbedHooks.OnSyncCompleted
    embedNext = func(results []ghclient.RepoSyncResult) {
        out := make([]RepoSyncResult, len(results))
        for i, r := range results {
            out[i] = RepoSyncResult{
                Owner:        r.Owner,
                Name:         r.Name,
                PlatformHost: r.PlatformHost,
                Error:        r.Error,
            }
        }
        cb(out)
    }
}
// Note: New() has no ctx — use context.Background(). The sync engine
// manages its own lifecycle; the hook won't be called after syncer.Stop().
syncer.SetOnSyncCompleted(stacks.SyncCompletedHook(context.Background(), database, embedNext))
```

This ensures stack detection always runs in the library path. The CLI path (`cmd/middleman/main.go`) passes the signal-derived ctx for cancellation-aware detection.

- [ ] **Step 8: Run `make api-generate`**

Run: `make api-generate`
Expected: Regenerates OpenAPI specs and generated clients

- [ ] **Step 9: Run full Go test suite**

Run: `go test ./...`
Expected: All PASS

- [ ] **Step 10: Commit**

```bash
git add internal/server/ internal/stacks/hook.go internal/db/queries.go \
  cmd/middleman/main.go middleman.go
git commit -m "feat: add stacks API endpoints and wire detection to sync"
```

---

### Task 5: Frontend — StacksStore and Routing

Add the new store, route, and type plumbing for the frontend.

**Files:**
- Create: `packages/ui/src/stores/stacks.svelte.ts`
- Modify: `packages/ui/src/types.ts` (add StacksStore, "stacks" to page union)
- Modify: `frontend/src/lib/stores/router.svelte.ts` (add stacks route)
- Modify: `packages/ui/src/Provider.svelte` (create and expose StacksStore)

- [ ] **Step 1: Create StacksStore**

```typescript
// packages/ui/src/stores/stacks.svelte.ts

import type { MiddlemanClient } from "../types.js";

// Use the generated schema types after api-generate.
type StackResponse = {
  id: number;
  name: string;
  repo_owner: string;
  repo_name: string;
  health: string;
  members: StackMemberResponse[];
};

type StackMemberResponse = {
  number: number;
  title: string;
  state: string;
  ci_status: string;
  review_decision: string;
  position: number;
  is_draft: boolean;
  blocked_by: number | null;
};

export interface StacksStoreOptions {
  client: MiddlemanClient;
  getGlobalRepo?: () => string | undefined;
}

export type StacksStore = ReturnType<typeof createStacksStore>;

export function createStacksStore(opts: StacksStoreOptions) {
  const apiClient = opts.client;
  const getGlobalRepo = opts.getGlobalRepo ?? (() => undefined);

  let stacks = $state<StackResponse[]>([]);
  let loading = $state(false);
  let storeError = $state<string | null>(null);

  function getStacks(): StackResponse[] {
    return stacks;
  }

  function isLoading(): boolean {
    return loading;
  }

  function getError(): string | null {
    return storeError;
  }

  function getStacksByRepo(): Map<string, StackResponse[]> {
    const grouped = new Map<string, StackResponse[]>();
    for (const stack of stacks) {
      const key = `${stack.repo_owner}/${stack.repo_name}`;
      const list = grouped.get(key) ?? [];
      list.push(stack);
      grouped.set(key, list);
    }
    return grouped;
  }

  async function loadStacks(): Promise<void> {
    loading = true;
    storeError = null;
    try {
      const globalRepo = getGlobalRepo();
      const { data, error } = await apiClient.GET("/stacks", {
        params: {
          query: {
            ...(globalRepo !== undefined && { repo: globalRepo }),
          },
        },
      });
      if (error) {
        throw new Error(
          error.detail ?? error.title ?? "failed to load stacks",
        );
      }
      stacks = (data as StackResponse[]) ?? [];
    } catch (err) {
      storeError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  return {
    getStacks,
    getStacksByRepo,
    isLoading,
    getError,
    loadStacks,
  };
}
```

- [ ] **Step 2: Add StacksStore to types.ts**

In `packages/ui/src/types.ts`:

Add `"stacks"` to `NavigateEvent.route.page` union:
```typescript
page: "pulls" | "issues" | "activity" | "diff" | "board" | "reviews" | "stacks";
```

Add export and import for StacksStore:
```typescript
export type { StacksStore } from "./stores/stacks.svelte.js";
```

Add import:
```typescript
import type { StacksStore } from "./stores/stacks.svelte.js";
```

Add to `StoreInstances`:
```typescript
stacks: StacksStore;
```

- [ ] **Step 3: Add stacks route to router**

In `frontend/src/lib/stores/router.svelte.ts`:

a) Add to `Route` union (at top of file, alongside existing page variants):
```typescript
| { page: "stacks" }
```

b) Update `getPage()` return type — add `"stacks"`:
```typescript
export function getPage():
  "activity" | "pulls" | "issues" | "settings" | "focus" | "reviews" | "stacks" {
  return route.page;
}
```

c) In `parseRoute`, add before the final `return { page: "activity" }` fallback:
```typescript
if (path === "/stacks") return { page: "stacks" };
```

d) Update `frontend/src/vite-env.d.ts`: add `"stacks"` to `MiddlemanNavigateEvent.type`:
```typescript
type: "pull" | "issue" | "activity" | "board" | "reviews" | "stacks";
```

e) In `buildRouteEvent`, add a stacks case before the final `else`:
```typescript
} else if (r.page === "stacks") {
  navType = "stacks";
} else {
  navType = r.page as "activity";
}
```

- [ ] **Step 4: Wire StacksStore in Provider.svelte**

In `packages/ui/src/Provider.svelte`:

a) Add imports (alongside existing store imports):
```typescript
import type { StacksStoreOptions } from "./stores/stacks.svelte.js";
import { createStacksStore } from "./stores/stacks.svelte.js";
```

b) In the `init()` function, after `const diffStore = createDiffStore(diffOpts);`, add:
```typescript
const stacksOpts: StacksStoreOptions = { client: cl };
if (hs.getGlobalRepo) {
  stacksOpts.getGlobalRepo = hs.getGlobalRepo;
}
const stacksStore = createStacksStore(stacksOpts);
```

c) Add `stacks: stacksStore` to the existing `StoreInstances` object. Find where `si` is constructed and add the `stacks` property alongside the existing stores (pulls, issues, detail, activity, sync, diff, grouping, collapsedRepos, settings, events, roborev stores):
```typescript
stacks: stacksStore,
```

d) Add stacks reload to `onDataChanged` callback inside `createEventsStore`. Find the callback that reloads pulls, issues, and activity on SSE `data_changed`, and add:
```typescript
void stacksStore.loadStacks();
```

- [ ] **Step 5: Commit**

```bash
git add packages/ui/src/stores/stacks.svelte.ts packages/ui/src/types.ts \
  frontend/src/lib/stores/router.svelte.ts packages/ui/src/Provider.svelte
git commit -m "feat: add StacksStore, stacks route, and provider wiring"
```

---

### Task 6: Frontend — Stacks View Component

Build the Stacks view with collapsible repo sections and stack cards.

**Files:**
- Create: `packages/ui/src/views/StacksView.svelte`
- Modify: `frontend/src/App.svelte` (add stacks view rendering)
- Modify: `frontend/src/lib/components/layout/AppHeader.svelte` (add Stacks nav link)

- [ ] **Step 1: Create StacksView component**

Create `packages/ui/src/views/StacksView.svelte` implementing:
- Header with total stack count
- Collapsible repo sections (repo name + stack count badge)
- Stack cards: collapsed (chevron + name + PR count + health dots + badge), expanded (vertical chain)
- Empty states: check `stores.settings.hasConfiguredRepos()` to distinguish "No repositories configured" vs "No stacks detected" (same pattern as PullList/IssueList)
- Subscribe to `sync.subscribeSyncComplete()` for auto-refresh; call returned unsubscribe function in `onDestroy` (follow `ActivityFeed.svelte` pattern)
- Use dot colors from spec: green (#238636), red (#f85149), yellow (#d29922), gray (#8b949e)
- Health badge pills: green for all_green/base_ready, yellow for partial_merge/in_progress, red for blocked

This is the largest single component. Follow the visual patterns from `packages/ui/src/components/ActivityFeed.svelte` for overall structure (header, empty states, scrollable list) and from the spec mockups for the specific card/chain design.

The component receives `stores` from Provider context and uses `stores.stacks.getStacksByRepo()` for grouped data.

- [ ] **Step 2: Export StacksView from packages/ui**

Add to `packages/ui/src/index.ts`:
```typescript
export { default as StacksView } from "./views/StacksView.svelte";
```

- [ ] **Step 3: Add Stacks view to App.svelte**

In `frontend/src/App.svelte`:

a) Add `StacksView` to the existing import from `@middleman/ui`. Current import includes `Provider, PRListView, IssueListView, ActivityFeedView, KanbanBoardView, ReviewsView, FocusListView`. Add `StacksView` to that list:
```typescript
import {
  Provider,
  PRListView,
  IssueListView,
  ActivityFeedView,
  KanbanBoardView,
  ReviewsView,
  FocusListView,
  StacksView,
} from "@middleman/ui";
```

b) Insert the stacks conditional **before** `{/if}` at the end of the view routing chain. The current chain ends with `{:else if getPage() === "reviews"}` then `{/if}`. Add stacks between reviews and `{/if}`:
```svelte
{:else if getPage() === "stacks"}
  <StacksView />
{/if}
```

c) Add keyboard shortcut `3` for stacks in `handleKeydown`, after the `case "2"` block:
```typescript
case "3":
  e.preventDefault();
  navigate("/stacks");
  break;
```

- [ ] **Step 4: Add Stacks nav link to AppHeader**

In `frontend/src/lib/components/layout/AppHeader.svelte`:

a) In the narrow-mode `<select>` dropdown, add a stacks option:
```svelte
<option value="stacks">Stacks</option>
```
And add handling in the `onchange` handler: `else if (v === "stacks") navigate("/stacks");`

Update the `value` binding to also check for stacks: add `getPage() === "stacks" ? "stacks" :` to the ternary.

b) In the `<div class="tab-group">`, add a Stacks button after Issues, before Board:
```svelte
<button class="view-tab" class:active={getPage() === "stacks"} onclick={() => navigate("/stacks")}>
  Stacks
</button>
```

- [ ] **Step 5: Load stacks data on mount**

In `App.svelte`, within the `onMount` async block where other stores load data (`stores.pulls.loadPulls()`, etc.), add:
```typescript
void stores.stacks.loadStacks();
```

And in the `$effect` that watches `getGlobalRepo()`:
```typescript
void stores.stacks.loadStacks();
```

- [ ] **Step 6: Test manually**

Run: `make dev` and `make frontend-dev`
Navigate to Stacks view. Verify empty state renders. If you have repos with stacked PRs, verify they appear.

- [ ] **Step 7: Commit**

```bash
git add packages/ui/src/views/StacksView.svelte packages/ui/src/index.ts \
  frontend/src/App.svelte frontend/src/lib/components/layout/AppHeader.svelte
git commit -m "feat: add Stacks view with collapsible repo sections and health badges"
```

---

### Task 7: Frontend — PR Detail Stack Sidebar

Add the right-rail sidebar panel that shows stack context when viewing a PR that belongs to a stack.

**Files:**
- Create: `packages/ui/src/components/detail/StackSidebar.svelte`
- Modify: `packages/ui/src/views/PRListView.svelte` (add sidebar alongside detail pane)

- [ ] **Step 1: Create StackSidebar component**

Create `packages/ui/src/components/detail/StackSidebar.svelte`:

Props: `owner: string`, `name: string`, `number: number`, `onNavigate: (path: string) => void`

**Important:** Use the `MiddlemanClient` from Provider context (via `getStores()` or a `client` prop) to make the API call. Do NOT hard-code the URL — the app may be served under a non-root base path or embed prefix. Use `client.GET("/repos/{owner}/{name}/pulls/{number}/stack", { params: { path: { owner, name, number } } })`.

Behavior:
- On mount / when props change, fetch `GET /repos/{owner}/{name}/pulls/{number}/stack` via the client
- If 404, render nothing (hidden)
- If data, render 200px right rail with:
  - "STACK · {name}" header in purple (#a371f7)
  - Vertical chain: 10px filled purple dot for current PR, 8px colored dots for others, gray for merged
  - Each member: clickable number + title, CI status, review status
  - "blocked by #N" in red italic for blocked descendants (using `blocked_by` field from response)
  - "You are here" label on current PR
  - "View full stack" link at bottom navigating to `/stacks`

- [ ] **Step 2: Integrate sidebar into PRListView**

In `packages/ui/src/views/PRListView.svelte`, when a PR is selected and detail is shown, add `StackSidebar` alongside the detail panel. The sidebar appears on the right when present, shrinking the detail pane width by 200px.

Use CSS flex layout: detail pane gets `flex: 1`, sidebar gets `width: 200px; flex-shrink: 0`.

- [ ] **Step 3: Test manually**

Run dev servers. Navigate to a PR that's part of a stack. Verify sidebar appears with correct chain, "You are here" label, health dots. Click a sibling PR number — should navigate to that PR's detail. Click "View full stack" — should go to Stacks view.

- [ ] **Step 4: Commit**

```bash
git add packages/ui/src/components/detail/StackSidebar.svelte \
  packages/ui/src/views/PRListView.svelte
git commit -m "feat: add stack sidebar to PR detail view"
```

---

### Post-Implementation

After all tasks are complete:

- [ ] Run `make api-generate` one final time to ensure all generated artifacts are up to date
- [ ] Run `make lint` and fix any warnings
- [ ] Run `make test` to verify all Go tests pass
- [ ] Run `make build` to verify the full binary builds with embedded frontend
