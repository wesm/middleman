# E2E Test Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a full-stack E2E test suite with a seeded SQLite database that catches integration bugs between the frontend UI and backend API.

**Architecture:** A Go fixture seeder populates a SQLite DB with synthetic data. A small Go binary starts the real HTTP server with that DB and the embedded frontend. Playwright tests run against this server, exercising the full stack.

**Tech Stack:** Go (fixture seeder, test server), Playwright (E2E tests), SQLite (seeded database)

**Spec:** `docs/superpowers/specs/2026-04-02-e2e-test-infrastructure-design.md`

---

### Task 1: Fixture Client (ghclient.Client for E2E server)

The E2E server needs a `ghclient.Client` that returns fixture-consistent data from list methods (so `RunOnce` doesn't mark all PRs/issues as closed) and rejects mutations.

**Files:**
- Create: `internal/testutil/fixture_client.go`

- [ ] **Step 1: Create the fixture client**

```go
// internal/testutil/fixture_client.go
package testutil

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v84/github"
	ghclient "github.com/wesm/middleman/internal/github"
)

// errReadOnly is returned by all mutation methods.
var errReadOnly = fmt.Errorf("e2e fixture client is read-only")

// FixtureClient implements ghclient.Client for the E2E test server.
// List methods return the seeded open items so RunOnce sees a
// consistent open set. Mutation methods return errors.
type FixtureClient struct {
	openPRs    map[string][]*gh.PullRequest // "owner/repo" -> PRs
	openIssues map[string][]*gh.Issue       // "owner/repo" -> Issues
}

// NewFixtureClient builds a FixtureClient from maps of open items
// keyed by "owner/repo".
func NewFixtureClient(
	prs map[string][]*gh.PullRequest,
	issues map[string][]*gh.Issue,
) ghclient.Client {
	return &FixtureClient{openPRs: prs, openIssues: issues}
}

func (c *FixtureClient) ListOpenPullRequests(
	_ context.Context, owner, repo string,
) ([]*gh.PullRequest, error) {
	return c.openPRs[owner+"/"+repo], nil
}

func (c *FixtureClient) ListOpenIssues(
	_ context.Context, owner, repo string,
) ([]*gh.Issue, error) {
	return c.openIssues[owner+"/"+repo], nil
}

func (c *FixtureClient) GetPullRequest(
	_ context.Context, _, _ string, _ int,
) (*gh.PullRequest, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) GetIssue(
	_ context.Context, _, _ string, _ int,
) (*gh.Issue, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) GetUser(
	_ context.Context, login string,
) (*gh.User, error) {
	return &gh.User{Login: &login}, nil
}

func (c *FixtureClient) ListIssueComments(
	_ context.Context, _, _ string, _ int,
) ([]*gh.IssueComment, error) {
	return nil, nil
}

func (c *FixtureClient) ListReviews(
	_ context.Context, _, _ string, _ int,
) ([]*gh.PullRequestReview, error) {
	return nil, nil
}

func (c *FixtureClient) ListCommits(
	_ context.Context, _, _ string, _ int,
) ([]*gh.RepositoryCommit, error) {
	return nil, nil
}

func (c *FixtureClient) GetCombinedStatus(
	_ context.Context, _, _, _ string,
) (*gh.CombinedStatus, error) {
	return nil, nil
}

func (c *FixtureClient) ListCheckRunsForRef(
	_ context.Context, _, _, _ string,
) ([]*gh.CheckRun, error) {
	return nil, nil
}

func (c *FixtureClient) CreateIssueComment(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.IssueComment, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) GetRepository(
	_ context.Context, _, _ string,
) (*gh.Repository, error) {
	return &gh.Repository{}, nil
}

func (c *FixtureClient) CreateReview(
	_ context.Context, _, _ string, _ int, _, _ string,
) (*gh.PullRequestReview, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) MarkPullRequestReadyForReview(
	_ context.Context, _, _ string, _ int,
) (*gh.PullRequest, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) MergePullRequest(
	_ context.Context, _, _ string, _ int, _, _, _ string,
) (*gh.PullRequestMergeResult, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) EditPullRequest(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.PullRequest, error) {
	return nil, errReadOnly
}

func (c *FixtureClient) EditIssue(
	_ context.Context, _, _ string, _ int, _ string,
) (*gh.Issue, error) {
	return nil, errReadOnly
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/testutil/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/testutil/fixture_client.go
git commit -m "Add FixtureClient for E2E test server"
```

---

### Task 2: Fixture Seeder

Populates a DB with synthetic data matching the spec's data set. All timestamps relative to `time.Now()`.

**Files:**
- Create: `internal/testutil/fixtures.go`

The seeder must produce data with these properties:
1. Comments exist on both PRs and issues (same `activity_type`, different `item_type`)
2. Bot authors on both PRs and issues
3. Mixed states: open, merged, closed
4. 4 consecutive commits by same author on same PR (for collapse testing)
5. Cross-repo activity (acme/widgets and acme/tools)
6. Items in each time bucket (20h, 5d, 25d, 60d ago) well inside boundaries

- [ ] **Step 1: Create the fixture seeder**

```go
// internal/testutil/fixtures.go
package testutil

import (
	"context"
	"fmt"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

// SeedResult holds IDs and fixture client data produced by SeedTestDB.
type SeedResult struct {
	// FixtureClient returns a ghclient.Client that is sync-safe
	// for the seeded data.
	FixtureClient func() *FixtureClient
}

// SeedTestDB populates a database with synthetic data for E2E tests.
// Timestamps are relative to time.Now() so time-range filters work
// regardless of when the tests run.
func SeedTestDB(d *db.DB) (*SeedResult, error) {
	ctx := context.Background()
	now := time.Now().UTC()

	// --- Repos ---
	widgetsID, err := d.UpsertRepo(ctx, "acme", "widgets")
	if err != nil {
		return nil, fmt.Errorf("upsert widgets: %w", err)
	}
	toolsID, err := d.UpsertRepo(ctx, "acme", "tools")
	if err != nil {
		return nil, fmt.Errorf("upsert tools: %w", err)
	}
	if _, err := d.UpsertRepo(ctx, "acme", "archived"); err != nil {
		return nil, fmt.Errorf("upsert archived: %w", err)
	}

	// Time offsets -- well inside bucket boundaries to avoid flakes.
	h := func(hours int) time.Time { return now.Add(-time.Duration(hours) * time.Hour) }

	// --- Pull Requests ---
	// acme/widgets: 7 PRs covering all states and properties
	prs := []db.PullRequest{
		{ // PR #1: open, recent, has reviews+comments+commits
			RepoID: widgetsID, GitHubID: 1001, Number: 1,
			URL: "https://github.com/acme/widgets/pull/1",
			Title: "Add widget caching layer", Author: "alice",
			State: "open", HeadBranch: "feat/cache", BaseBranch: "main",
			Additions: 150, Deletions: 20, CommentCount: 3,
			ReviewDecision: "APPROVED", CIStatus: "success",
			CIChecksJSON: "[]",
			CreatedAt: h(5 * 24), UpdatedAt: h(4),
			LastActivityAt: h(4),
		},
		{ // PR #2: open, with merge conflict
			RepoID: widgetsID, GitHubID: 1002, Number: 2,
			URL: "https://github.com/acme/widgets/pull/2",
			Title: "Fix race condition in event loop", Author: "bob",
			State: "open", HeadBranch: "fix/race", BaseBranch: "main",
			Additions: 30, Deletions: 10, CommentCount: 1,
			MergeableState: "dirty", CIStatus: "failure",
			CIChecksJSON: "[]",
			CreatedAt: h(3 * 24), UpdatedAt: h(20),
			LastActivityAt: h(20),
		},
		{ // PR #3: merged recently (within 5d)
			RepoID: widgetsID, GitHubID: 1003, Number: 3,
			URL: "https://github.com/acme/widgets/pull/3",
			Title: "Upgrade dependency versions", Author: "carol",
			State: "merged", HeadBranch: "chore/deps", BaseBranch: "main",
			Additions: 40, Deletions: 40,
			CIChecksJSON: "[]",
			CreatedAt: h(7 * 24), UpdatedAt: h(4 * 24),
			LastActivityAt: h(4 * 24),
			MergedAt: timePtr(h(4 * 24)),
		},
		{ // PR #4: merged older (within 25d)
			RepoID: widgetsID, GitHubID: 1004, Number: 4,
			URL: "https://github.com/acme/widgets/pull/4",
			Title: "Refactor storage backend", Author: "alice",
			State: "merged", HeadBranch: "refactor/storage", BaseBranch: "main",
			Additions: 200, Deletions: 180,
			CIChecksJSON: "[]",
			CreatedAt: h(30 * 24), UpdatedAt: h(25 * 24),
			LastActivityAt: h(25 * 24),
			MergedAt: timePtr(h(25 * 24)),
		},
		{ // PR #5: closed (not merged)
			RepoID: widgetsID, GitHubID: 1005, Number: 5,
			URL: "https://github.com/acme/widgets/pull/5",
			Title: "Experimental new API", Author: "bob",
			State: "closed", HeadBranch: "experimental/api", BaseBranch: "main",
			CIChecksJSON: "[]",
			CreatedAt: h(10 * 24), UpdatedAt: h(5 * 24),
			LastActivityAt: h(5 * 24),
			ClosedAt: timePtr(h(5 * 24)),
		},
		{ // PR #6: draft, no reviews
			RepoID: widgetsID, GitHubID: 1006, Number: 6,
			URL: "https://github.com/acme/widgets/pull/6",
			Title: "WIP: new dashboard layout", Author: "carol",
			State: "open", IsDraft: true,
			HeadBranch: "wip/dashboard", BaseBranch: "main",
			CIChecksJSON: "[]",
			CreatedAt: h(2 * 24), UpdatedAt: h(20),
			LastActivityAt: h(20),
		},
		{ // PR #7: bot PR
			RepoID: widgetsID, GitHubID: 1007, Number: 7,
			URL: "https://github.com/acme/widgets/pull/7",
			Title: "Bump lodash from 4.17.20 to 4.17.21",
			Author: "dependabot[bot]",
			State: "open", HeadBranch: "dependabot/lodash", BaseBranch: "main",
			CIChecksJSON: "[]",
			CreatedAt: h(3 * 24), UpdatedAt: h(3 * 24),
			LastActivityAt: h(3 * 24),
		},
	}

	// acme/tools: 2 PRs
	prs = append(prs,
		db.PullRequest{ // PR #1 in tools: open
			RepoID: toolsID, GitHubID: 2001, Number: 1,
			URL:   "https://github.com/acme/tools/pull/1",
			Title: "Add CLI flag parser", Author: "dave",
			State: "open", HeadBranch: "feat/cli", BaseBranch: "main",
			Additions: 80, Deletions: 5,
			CIChecksJSON: "[]",
			CreatedAt: h(5 * 24), UpdatedAt: h(12),
			LastActivityAt: h(12),
		},
		db.PullRequest{ // PR #2 in tools: merged within 60d
			RepoID: toolsID, GitHubID: 2002, Number: 2,
			URL:   "https://github.com/acme/tools/pull/2",
			Title: "Initial project setup", Author: "alice",
			State: "merged", HeadBranch: "init", BaseBranch: "main",
			CIChecksJSON: "[]",
			CreatedAt: h(65 * 24), UpdatedAt: h(60 * 24),
			LastActivityAt: h(60 * 24),
			MergedAt: timePtr(h(60 * 24)),
		},
	)

	prIDs := make(map[string]int64) // "owner/repo#N" -> DB id
	for i := range prs {
		id, err := d.UpsertPullRequest(ctx, &prs[i])
		if err != nil {
			return nil, fmt.Errorf("upsert PR %d: %w", prs[i].Number, err)
		}
		repo := "widgets"
		if prs[i].RepoID == toolsID {
			repo = "tools"
		}
		prIDs[fmt.Sprintf("acme/%s#%d", repo, prs[i].Number)] = id
	}

	// --- Issues ---
	issues := []db.Issue{
		{ // Issue #10: open with comments, recent
			RepoID: widgetsID, GitHubID: 3001, Number: 10,
			URL:   "https://github.com/acme/widgets/issues/10",
			Title: "Widget rendering broken on Safari", Author: "eve",
			State: "open", LabelsJSON: `[{"name":"bug","color":"d73a4a"}]`,
			CreatedAt: h(4 * 24), UpdatedAt: h(8),
			LastActivityAt: h(8),
		},
		{ // Issue #11: open, older
			RepoID: widgetsID, GitHubID: 3002, Number: 11,
			URL:   "https://github.com/acme/widgets/issues/11",
			Title: "Add dark mode support", Author: "alice",
			State: "open", LabelsJSON: `[{"name":"enhancement","color":"a2eeef"}]`,
			CreatedAt: h(20 * 24), UpdatedAt: h(15 * 24),
			LastActivityAt: h(15 * 24),
		},
		{ // Issue #12: closed recently
			RepoID: widgetsID, GitHubID: 3003, Number: 12,
			URL:   "https://github.com/acme/widgets/issues/12",
			Title: "Crash on empty input", Author: "bob",
			State: "closed", LabelsJSON: `[{"name":"bug","color":"d73a4a"}]`,
			CreatedAt: h(10 * 24), UpdatedAt: h(3 * 24),
			LastActivityAt: h(3 * 24),
			ClosedAt: timePtr(h(3 * 24)),
		},
		{ // Issue #13: bot issue
			RepoID: widgetsID, GitHubID: 3004, Number: 13,
			URL:   "https://github.com/acme/widgets/issues/13",
			Title: "Security advisory: prototype pollution",
			Author: "dependabot[bot]",
			State: "open", LabelsJSON: `[{"name":"security","color":"e4e669"}]`,
			CreatedAt: h(2 * 24), UpdatedAt: h(2 * 24),
			LastActivityAt: h(2 * 24),
		},
		{ // Issue #5 in tools: open
			RepoID: toolsID, GitHubID: 4001, Number: 5,
			URL:   "https://github.com/acme/tools/issues/5",
			Title: "Support config file loading", Author: "dave",
			State: "open", LabelsJSON: "[]",
			CreatedAt: h(5 * 24), UpdatedAt: h(2 * 24),
			LastActivityAt: h(2 * 24),
		},
	}

	issueIDs := make(map[string]int64)
	for i := range issues {
		id, err := d.UpsertIssue(ctx, &issues[i])
		if err != nil {
			return nil, fmt.Errorf("upsert issue %d: %w", issues[i].Number, err)
		}
		repo := "widgets"
		if issues[i].RepoID == toolsID {
			repo = "tools"
		}
		issueIDs[fmt.Sprintf("acme/%s#%d", repo, issues[i].Number)] = id
	}

	// --- PR Events ---
	prEvents := []db.PREvent{
		// Comments on PR #1 (alice's caching PR)
		{
			PRID: prIDs["acme/widgets#1"], EventType: "issue_comment",
			Author: "bob", Body: "Looks good, just a few nits",
			CreatedAt: h(4*24 + 12), DedupeKey: "pr1-comment-1",
		},
		{
			PRID: prIDs["acme/widgets#1"], EventType: "issue_comment",
			Author: "carol", Body: "LGTM, approved",
			CreatedAt: h(4 * 24), DedupeKey: "pr1-comment-2",
		},
		// Review on PR #1
		{
			PRID: prIDs["acme/widgets#1"], EventType: "review",
			Author: "bob", Summary: "APPROVED",
			CreatedAt: h(4*24 + 6), DedupeKey: "pr1-review-1",
		},
		// 4 consecutive commits by alice on PR #1 (for collapse testing)
		{
			PRID: prIDs["acme/widgets#1"], EventType: "commit",
			Author: "alice", Summary: "aaa111",
			Body: "feat: add cache invalidation",
			CreatedAt: h(4*24 + 3), DedupeKey: "pr1-commit-1",
		},
		{
			PRID: prIDs["acme/widgets#1"], EventType: "commit",
			Author: "alice", Summary: "bbb222",
			Body: "feat: add cache warming",
			CreatedAt: h(4*24 + 2), DedupeKey: "pr1-commit-2",
		},
		{
			PRID: prIDs["acme/widgets#1"], EventType: "commit",
			Author: "alice", Summary: "ccc333",
			Body: "test: add cache tests",
			CreatedAt: h(4*24 + 1), DedupeKey: "pr1-commit-3",
		},
		{
			PRID: prIDs["acme/widgets#1"], EventType: "commit",
			Author: "alice", Summary: "ddd444",
			Body: "fix: cache TTL off-by-one",
			CreatedAt: h(4 * 24), DedupeKey: "pr1-commit-4",
		},
		// Comment on PR #2 (bob's race fix)
		{
			PRID: prIDs["acme/widgets#2"], EventType: "issue_comment",
			Author: "alice", Body: "Can you add a test for this?",
			CreatedAt: h(2 * 24), DedupeKey: "pr2-comment-1",
		},
		// Review on PR #2
		{
			PRID: prIDs["acme/widgets#2"], EventType: "review",
			Author: "alice", Summary: "CHANGES_REQUESTED",
			CreatedAt: h(2 * 24), DedupeKey: "pr2-review-1",
		},
		// Comment on tools PR #1
		{
			PRID: prIDs["acme/tools#1"], EventType: "issue_comment",
			Author: "alice", Body: "Nice approach for the flag parser",
			CreatedAt: h(12), DedupeKey: "tools-pr1-comment-1",
		},
	}

	if err := d.UpsertPREvents(ctx, prEvents); err != nil {
		return nil, fmt.Errorf("upsert pr events: %w", err)
	}

	// --- Issue Events ---
	issueEvents := []db.IssueEvent{
		// Comments on issue #10 (Safari bug)
		{
			IssueID: issueIDs["acme/widgets#10"],
			EventType: "issue_comment",
			Author: "alice", Body: "I can reproduce this on Safari 17",
			CreatedAt: h(3 * 24), DedupeKey: "issue10-comment-1",
		},
		{
			IssueID: issueIDs["acme/widgets#10"],
			EventType: "issue_comment",
			Author: "bob", Body: "Same here, seems related to flexbox",
			CreatedAt: h(8), DedupeKey: "issue10-comment-2",
		},
		// Comment on closed issue #12
		{
			IssueID: issueIDs["acme/widgets#12"],
			EventType: "issue_comment",
			Author: "carol", Body: "Fixed in PR #1",
			CreatedAt: h(3 * 24), DedupeKey: "issue12-comment-1",
		},
		// Comment on tools issue #5
		{
			IssueID: issueIDs["acme/tools#5"],
			EventType: "issue_comment",
			Author: "dave", Body: "Will handle TOML and YAML",
			CreatedAt: h(2 * 24), DedupeKey: "tools-issue5-comment-1",
		},
	}

	if err := d.UpsertIssueEvents(ctx, issueEvents); err != nil {
		return nil, fmt.Errorf("upsert issue events: %w", err)
	}

	// Build the fixture client data for sync safety.
	result := &SeedResult{
		FixtureClient: func() *FixtureClient {
			openPRs := make(map[string][]*gh.PullRequest)
			for i := range prs {
				if prs[i].State != "open" {
					continue
				}
				repo := "widgets"
				if prs[i].RepoID == toolsID {
					repo = "tools"
				}
				key := "acme/" + repo
				n := prs[i].Number
				openPRs[key] = append(openPRs[key],
					&gh.PullRequest{Number: &n})
			}
			openIssues := make(map[string][]*gh.Issue)
			for i := range issues {
				if issues[i].State != "open" {
					continue
				}
				repo := "widgets"
				if issues[i].RepoID == toolsID {
					repo = "tools"
				}
				key := "acme/" + repo
				n := issues[i].Number
				openIssues[key] = append(openIssues[key],
					&gh.Issue{Number: &n})
			}
			return &FixtureClient{
				openPRs:    openPRs,
				openIssues: openIssues,
			}
		},
	}

	return result, nil
}

func timePtr(t time.Time) *time.Time {
	return &t
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/testutil/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/testutil/fixtures.go
git commit -m "Add fixture seeder for E2E test database"
```

---

### Task 3: Fixture Seeder Test

Verify the seeder produces the expected data shape and counts.

**Files:**
- Create: `internal/testutil/fixtures_test.go`

- [ ] **Step 1: Write the test**

```go
package testutil

import (
	"context"
	"path/filepath"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSeedTestDB(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	result, err := SeedTestDB(d)
	require.NoError(t, err)

	t.Run("repos", func(t *testing.T) {
		assert := Assert.New(t)
		repos, err := d.ListRepos(ctx)
		require.NoError(t, err)
		assert.Len(repos, 3)
		names := make([]string, len(repos))
		for i, r := range repos {
			names[i] = r.Owner + "/" + r.Name
		}
		assert.Contains(names, "acme/widgets")
		assert.Contains(names, "acme/tools")
		assert.Contains(names, "acme/archived")
	})

	t.Run("pull requests", func(t *testing.T) {
		assert := Assert.New(t)
		prs, err := d.ListPullRequests(ctx, db.ListPullsOpts{
			State: "all", Limit: 100,
		})
		require.NoError(t, err)
		assert.Len(prs, 9)

		// Verify state distribution.
		states := map[string]int{}
		for _, pr := range prs {
			states[pr.State]++
		}
		assert.Equal(5, states["open"])
		assert.Equal(3, states["merged"])
		assert.Equal(1, states["closed"])

		// Verify bot PR exists.
		var hasBotPR bool
		for _, pr := range prs {
			if pr.Author == "dependabot[bot]" {
				hasBotPR = true
			}
		}
		assert.True(hasBotPR, "should have a bot-authored PR")
	})

	t.Run("issues", func(t *testing.T) {
		assert := Assert.New(t)
		issues, err := d.ListIssues(ctx, db.ListIssuesOpts{
			State: "all", Limit: 100,
		})
		require.NoError(t, err)
		assert.Len(issues, 5)

		// Verify bot issue exists.
		var hasBotIssue bool
		for _, issue := range issues {
			if issue.Author == "dependabot[bot]" {
				hasBotIssue = true
			}
		}
		assert.True(hasBotIssue, "should have a bot-authored issue")
	})

	t.Run("activity feed has PR and issue comments", func(t *testing.T) {
		assert := Assert.New(t)
		items, err := d.ListActivity(ctx, db.ListActivityOpts{
			Limit: 200,
		})
		require.NoError(t, err)
		require.NotEmpty(t, items)

		var prComments, issueComments int
		for _, it := range items {
			if it.ActivityType == "comment" && it.ItemType == "pr" {
				prComments++
			}
			if it.ActivityType == "comment" && it.ItemType == "issue" {
				issueComments++
			}
		}
		assert.Greater(prComments, 0,
			"must have comments on PRs")
		assert.Greater(issueComments, 0,
			"must have comments on issues")
	})

	t.Run("fixture client returns open items", func(t *testing.T) {
		assert := Assert.New(t)
		fc := result.FixtureClient()
		widgetPRs := fc.openPRs["acme/widgets"]
		assert.Len(widgetPRs, 4, "4 open PRs in widgets")
		widgetIssues := fc.openIssues["acme/widgets"]
		assert.Len(widgetIssues, 3, "3 open issues in widgets")
	})
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/testutil/ -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/testutil/fixtures_test.go
git commit -m "Add fixture seeder tests"
```

---

### Task 4: E2E Server Binary

The binary that starts the real HTTP server with a seeded DB and embedded frontend.

**Files:**
- Create: `cmd/e2e-server/main.go`
- Modify: `.gitignore`

- [ ] **Step 1: Create the e2e-server binary**

```go
// cmd/e2e-server/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/server"
	"github.com/wesm/middleman/internal/testutil"
	"github.com/wesm/middleman/internal/web"
)

func main() {
	port := flag.Int("port", 4174, "HTTP listen port")
	flag.Parse()

	if err := run(*port); err != nil {
		log.Fatal(err)
	}
}

func run(port int) error {
	dir, err := os.MkdirTemp("", "e2e-server-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	database, err := db.Open(dir + "/e2e.db")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	result, err := testutil.SeedTestDB(database)
	if err != nil {
		return fmt.Errorf("seed db: %w", err)
	}

	cfg := &config.Config{
		Repos: []config.Repo{
			{Owner: "acme", Name: "widgets"},
			{Owner: "acme", Name: "tools"},
			{Owner: "acme", Name: "archived"},
		},
		Activity: config.Activity{
			ViewMode:  "flat",
			TimeRange: "7d",
		},
	}

	fc := result.FixtureClient()
	repos := []ghclient.RepoRef{
		{Owner: "acme", Name: "widgets"},
		{Owner: "acme", Name: "tools"},
		{Owner: "acme", Name: "archived"},
	}
	syncer := ghclient.NewSyncer(fc, database, repos, time.Hour)

	frontend, err := web.Assets()
	if err != nil {
		return fmt.Errorf("load frontend assets: %w", err)
	}

	srv := server.New(
		database, fc, syncer, frontend, "/", cfg,
		server.ServerOptions{},
	)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("e2e server listening on %s", addr)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(addr)
	}()

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down", sig)
		return nil
	case err := <-errCh:
		return err
	}
}
```

- [ ] **Step 2: Add binary to .gitignore**

Add `cmd/e2e-server/e2e-server` to `.gitignore`.

- [ ] **Step 3: Verify it compiles**

Run: `go build -o ./cmd/e2e-server/e2e-server ./cmd/e2e-server`
Expected: no errors (binary created at `cmd/e2e-server/e2e-server`)

- [ ] **Step 4: Commit**

```bash
git add cmd/e2e-server/main.go .gitignore
git commit -m "Add E2E test server binary"
```

---

### Task 5: Makefile Target

Add the `test-e2e` target and update help text.

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add test-e2e target and update .PHONY**

Add to the `.PHONY` line: `test-e2e`

Add after the `test-short` target:

```makefile
# Run full-stack E2E tests (Playwright against real Go server)
test-e2e: frontend
	go build -o ./cmd/e2e-server/e2e-server ./cmd/e2e-server
	cd frontend && bun run playwright test --config=playwright-e2e.config.ts
```

Add to the help section after the `test-short` line:

```
	@echo "  test-e2e       - Run full-stack E2E Playwright tests"
```

- [ ] **Step 2: Verify make target parses**

Run: `make -n test-e2e`
Expected: prints the commands without executing

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "Add test-e2e Makefile target"
```

---

### Task 6: Playwright E2E Config

The Playwright config that points at the Go e2e-server binary.

**Files:**
- Create: `frontend/playwright-e2e.config.ts`

- [ ] **Step 1: Create the config**

```ts
import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e-full",
  fullyParallel: true,
  timeout: 30_000,
  expect: {
    timeout: 5_000,
  },
  use: {
    baseURL: "http://127.0.0.1:4174",
    trace: "on-first-retry",
  },
  webServer: {
    command: "../cmd/e2e-server/e2e-server -port 4174",
    port: 4174,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],
});
```

- [ ] **Step 2: Commit**

```bash
git add frontend/playwright-e2e.config.ts
git commit -m "Add Playwright config for full-stack E2E tests"
```

---

### Task 7: Activity Filter E2E Tests

The core test suite that would have caught the PR/Issues filter bug.

**Files:**
- Create: `frontend/tests/e2e-full/activity-filters.spec.ts`

The fixture data has:
- PR comments (activity_type=comment, item_type=pr): "Looks good..." on PR #1, "Can you add a test..." on PR #2, "Nice approach..." on tools PR #1
- Issue comments (activity_type=comment, item_type=issue): "I can reproduce..." on issue #10, "Same here..." on issue #10, "Fixed in PR #1" on issue #12, "Will handle TOML..." on tools issue #5
- Bot authors: "dependabot[bot]" on PR #7 and issue #13
- Merged/closed items: PR #3 merged, PR #4 merged, PR #5 closed, issue #12 closed

- [ ] **Step 1: Create the activity filter tests**

```ts
import { expect, test, type Locator, type Page } from "@playwright/test";

// Wait for the activity table to finish loading (no "Loading..." state).
async function waitForActivity(page: Page): Promise<void> {
  await page.locator(".activity-table tbody tr").first().waitFor({ timeout: 10_000 });
}

// Get all Kind badge texts from visible rows.
async function getBadgeTexts(page: Page): Promise<string[]> {
  const badges = page.locator(".activity-table .col-kind .badge");
  return badges.allTextContents();
}

// Get all author texts from visible rows.
async function getAuthors(page: Page): Promise<string[]> {
  const cells = page.locator(".activity-table .col-author");
  return cells.allTextContents();
}

// Click a segmented control button by its label text.
async function clickSegBtn(page: Page, label: string): Promise<void> {
  await page.locator(".segmented-control .seg-btn", { hasText: label }).click();
}

test.describe("Activity feed filters", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
    // Ensure we are on the flat view so we can inspect table rows.
    await clickSegBtn(page, "Flat");
    // Use 30d range for a good mix of items.
    await clickSegBtn(page, "30d");
    await waitForActivity(page);
  });

  test("PR filter shows only PR items", async ({ page }) => {
    await clickSegBtn(page, "PRs");
    await waitForActivity(page);
    const badges = await getBadgeTexts(page);
    expect(badges.length).toBeGreaterThan(0);
    for (const badge of badges) {
      expect(badge.trim()).toBe("PR");
    }
  });

  test("Issues filter shows only issue items", async ({ page }) => {
    await clickSegBtn(page, "Issues");
    await waitForActivity(page);
    const badges = await getBadgeTexts(page);
    expect(badges.length).toBeGreaterThan(0);
    for (const badge of badges) {
      expect(badge.trim()).toBe("ISSUE");
    }
  });

  test("All filter shows both PRs and issues", async ({ page }) => {
    await clickSegBtn(page, "All");
    await waitForActivity(page);
    const badges = await getBadgeTexts(page);
    const hasPR = badges.some((b) => b.trim() === "PR");
    const hasIssue = badges.some((b) => b.trim() === "ISSUE");
    expect(hasPR).toBe(true);
    expect(hasIssue).toBe(true);
  });

  test("disabling Comments hides comment rows", async ({ page }) => {
    // Open filter dropdown.
    await page.locator(".filter-btn").click();

    // Disable Comments.
    await page.locator(".filter-item", { hasText: "Comments" }).click();
    await page.locator(".filter-btn").click(); // close dropdown
    await waitForActivity(page);

    // No "Comment" should appear in the event column.
    const events = await page
      .locator(".activity-table .col-event .evt-label")
      .allTextContents();
    for (const evt of events) {
      expect(evt.trim()).not.toBe("Comment");
    }
  });

  test("hide closed/merged removes merged and closed items", async ({ page }) => {
    // Open filter dropdown.
    await page.locator(".filter-btn").click();
    await page
      .locator(".filter-item", { hasText: "Hide closed/merged" })
      .click();
    await page.locator(".filter-btn").click(); // close dropdown

    // Wait for the table to update.
    await waitForActivity(page);

    // No state badges for Merged or Closed should be visible.
    const stateBadges = page.locator(".state-merged, .state-closed");
    await expect(stateBadges).toHaveCount(0);
  });

  test("hide bots removes bot-authored rows", async ({ page }) => {
    // First verify bots exist.
    let authors = await getAuthors(page);
    const hasBotBefore = authors.some((a) =>
      a.toLowerCase().includes("[bot]"),
    );
    expect(hasBotBefore).toBe(true);

    // Toggle hide bots.
    await page.locator(".filter-btn").click();
    await page.locator(".filter-item", { hasText: "Hide bots" }).click();
    await page.locator(".filter-btn").click();

    authors = await getAuthors(page);
    const hasBotAfter = authors.some((a) =>
      a.toLowerCase().includes("[bot]"),
    );
    expect(hasBotAfter).toBe(false);
  });

  test("24h range shows fewer items than 7d", async ({ page }) => {
    await clickSegBtn(page, "7d");
    await waitForActivity(page);
    const rows7d = await page
      .locator(".activity-table tbody tr")
      .count();

    await clickSegBtn(page, "24h");
    // Small delay for reload.
    await page.waitForTimeout(500);
    const rows24h = await page
      .locator(".activity-table tbody tr")
      .count();

    expect(rows24h).toBeLessThan(rows7d);
  });

  test("search filters by title", async ({ page }) => {
    const searchInput = page.locator(".search-input");
    await searchInput.fill("caching layer");
    // Wait for debounce + reload.
    await page.waitForTimeout(500);
    await waitForActivity(page);

    const titles = await page
      .locator(".activity-table .col-item .item-title")
      .allTextContents();
    expect(titles.length).toBeGreaterThan(0);
    for (const title of titles) {
      expect(title.toLowerCase()).toContain("caching layer");
    }
  });

  test("combined: PRs + hide closed/merged", async ({ page }) => {
    // Select PRs only.
    await clickSegBtn(page, "PRs");
    await waitForActivity(page);

    // Hide closed/merged.
    await page.locator(".filter-btn").click();
    await page
      .locator(".filter-item", { hasText: "Hide closed/merged" })
      .click();
    await page.locator(".filter-btn").click();

    await waitForActivity(page);

    // All badges should be PR.
    const badges = await getBadgeTexts(page);
    expect(badges.length).toBeGreaterThan(0);
    for (const badge of badges) {
      expect(badge.trim()).toBe("PR");
    }

    // No merged/closed state badges.
    const stateBadges = page.locator(".state-merged, .state-closed");
    await expect(stateBadges).toHaveCount(0);
  });
});
```

- [ ] **Step 2: Build the E2E server binary (needed for Playwright)**

Run: `make frontend && go build -o ./cmd/e2e-server/e2e-server ./cmd/e2e-server`
Expected: binary built

- [ ] **Step 3: Run the tests**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts`
Expected: all tests pass

If any tests fail due to incorrect selectors or badge text (e.g. "ISSUE" vs "Issue"), inspect the actual page with `npx playwright test --headed --config=playwright-e2e.config.ts` and adjust the assertions. The badge text comes from `ActivityFeed.svelte:175` which returns "PR" or "Issue", and the CSS `text-transform: uppercase` on `.badge` makes it render as "PR" or "ISSUE" -- so `allTextContents()` may return the original casing or the CSS-transformed casing depending on browser behavior. Use case-insensitive comparison if needed.

- [ ] **Step 4: Commit**

```bash
git add frontend/tests/e2e-full/activity-filters.spec.ts
git commit -m "Add activity feed filter E2E tests"
```

---

### Task 8: PR List E2E Tests

**Files:**
- Create: `frontend/tests/e2e-full/pull-list.spec.ts`

Fixture data: 9 PRs total (7 in widgets, 2 in tools). 5 open, 3 merged, 1 closed.

- [ ] **Step 1: Create the PR list tests**

```ts
import { expect, test, type Page } from "@playwright/test";

async function waitForPRList(page: Page): Promise<void> {
  await page
    .locator(".sidebar-list .pr-card, .sidebar-list .issue-row")
    .first()
    .waitFor({ timeout: 10_000 });
}

test.describe("PR list", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/pulls");
    await waitForPRList(page);
  });

  test("renders open PRs by default", async ({ page }) => {
    // Default state filter is "open".
    const count = page.locator(".sidebar-count");
    const countText = await count.textContent();
    // 5 open PRs across widgets + tools.
    expect(countText).toContain("5");
  });

  test("closed state filter shows closed and merged PRs", async ({ page }) => {
    // Click the "Closed" state tab.
    await page.locator(".state-tab", { hasText: "Closed" }).click();
    await page.waitForTimeout(300);

    const count = page.locator(".sidebar-count");
    const countText = await count.textContent();
    // 3 merged + 1 closed = 4.
    expect(countText).toContain("4");
  });

  test("search filters by title", async ({ page }) => {
    const searchInput = page.locator(".sidebar-search input");
    await searchInput.fill("caching");
    await page.waitForTimeout(500);

    const titles = await page
      .locator(".sidebar-list .pr-title")
      .allTextContents();
    expect(titles.length).toBe(1);
    expect(titles[0]).toContain("caching");
  });
});
```

- [ ] **Step 2: Run the tests**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts tests/e2e-full/pull-list.spec.ts`
Expected: all tests pass

Fix any selector issues based on actual DOM inspection.

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/pull-list.spec.ts
git commit -m "Add PR list E2E tests"
```

---

### Task 9: Issue List E2E Tests

**Files:**
- Create: `frontend/tests/e2e-full/issue-list.spec.ts`

Fixture data: 5 issues total (4 in widgets, 1 in tools). 4 open, 1 closed.

- [ ] **Step 1: Create the issue list tests**

```ts
import { expect, test, type Page } from "@playwright/test";

async function waitForIssueList(page: Page): Promise<void> {
  await page
    .locator(".sidebar-list .issue-row, .sidebar-list .pr-card")
    .first()
    .waitFor({ timeout: 10_000 });
}

test.describe("Issue list", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/issues");
    await waitForIssueList(page);
  });

  test("renders open issues by default", async ({ page }) => {
    const count = page.locator(".sidebar-count");
    const countText = await count.textContent();
    // 4 open issues across widgets + tools.
    expect(countText).toContain("4");
  });

  test("closed state filter shows closed issues", async ({ page }) => {
    await page.locator(".state-tab", { hasText: "Closed" }).click();
    await page.waitForTimeout(300);

    const count = page.locator(".sidebar-count");
    const countText = await count.textContent();
    expect(countText).toContain("1");
  });

  test("search filters by title", async ({ page }) => {
    const searchInput = page.locator(".sidebar-search input");
    await searchInput.fill("Safari");
    await page.waitForTimeout(500);

    const titles = await page
      .locator(".sidebar-list .issue-title")
      .allTextContents();
    expect(titles.length).toBe(1);
    expect(titles[0]).toContain("Safari");
  });
});
```

- [ ] **Step 2: Run the tests**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts tests/e2e-full/issue-list.spec.ts`
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/issue-list.spec.ts
git commit -m "Add issue list E2E tests"
```

---

### Task 10: Navigation E2E Tests

**Files:**
- Create: `frontend/tests/e2e-full/navigation.spec.ts`

- [ ] **Step 1: Create the navigation tests**

```ts
import { expect, test } from "@playwright/test";

test.describe("Navigation", () => {
  test("sidebar nav switches between views", async ({ page }) => {
    await page.goto("/");

    // Start on Activity (default route).
    await expect(page.locator(".activity-feed")).toBeVisible();

    // Navigate to PRs.
    await page.locator(".view-tab", { hasText: "PRs" }).click();
    await expect(page).toHaveURL(/\/pulls/);

    // Navigate to Issues.
    await page.locator(".view-tab", { hasText: "Issues" }).click();
    await expect(page).toHaveURL(/\/issues/);

    // Navigate back to Activity.
    await page.locator(".view-tab", { hasText: "Activity" }).click();
    await expect(page).toHaveURL(/\/$/);
    await expect(page.locator(".activity-feed")).toBeVisible();
  });

  test("clicking a PR row opens detail view", async ({ page }) => {
    await page.goto("/pulls");
    // Wait for PR list to load.
    await page
      .locator(".sidebar-list .pr-card, .sidebar-list .issue-row")
      .first()
      .waitFor({ timeout: 10_000 });

    // Click the first PR.
    await page
      .locator(".sidebar-list .pr-card, .sidebar-list .issue-row")
      .first()
      .click();

    // Detail pane should appear with PR title.
    await expect(
      page.locator(".detail-pane, .pull-detail"),
    ).toBeVisible({ timeout: 5_000 });
  });

  test("clicking an issue row opens detail view", async ({ page }) => {
    await page.goto("/issues");
    await page
      .locator(".sidebar-list .issue-row, .sidebar-list .pr-card")
      .first()
      .waitFor({ timeout: 10_000 });

    await page
      .locator(".sidebar-list .issue-row, .sidebar-list .pr-card")
      .first()
      .click();

    await expect(
      page.locator(".detail-pane, .issue-detail"),
    ).toBeVisible({ timeout: 5_000 });
  });
});
```

- [ ] **Step 2: Run the full E2E suite**

Run: `cd frontend && bun run playwright test --config=playwright-e2e.config.ts`
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add frontend/tests/e2e-full/navigation.spec.ts
git commit -m "Add navigation E2E tests"
```

---

### Task 11: Final Verification

Run the complete suite and fix any remaining issues.

- [ ] **Step 1: Run Go tests to verify nothing is broken**

Run: `make test-short`
Expected: all Go tests pass

- [ ] **Step 2: Run the full E2E suite from the Makefile target**

Run: `make test-e2e`
Expected: frontend builds, e2e-server compiles, all Playwright tests pass

- [ ] **Step 3: Clean up the e2e-server binary**

Run: `rm -f cmd/e2e-server/e2e-server`

- [ ] **Step 4: Commit any remaining fixes**

If any test adjustments were needed (selectors, timing, assertions), commit them.
