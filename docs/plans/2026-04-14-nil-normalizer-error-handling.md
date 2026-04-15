# Nil normalizer error handling implementation plan

> **For agentic workers:** REQUIRED: Use `/skill:orchestrator-implements` (in-session, orchestrator implements), `/skill:subagent-driven-development` (in-session, subagents implement), or `/skill:executing-plans` (parallel session) to implement this plan. Steps use checkbox syntax for tracking.

**Goal:** Make nil GitHub PR/issue payloads hard errors instead of silent zero-value writes, and add regression coverage for sync and HTTP mutation paths.

**Architecture:** Keep normalizer pointer return types but return `nil` on nil input. Push explicit nil checks into sync and API call sites before any upsert or label replacement. Add tests around both internal sync flows and HTTP mutation fallback behavior using real SQLite-backed paths.

**Tech Stack:** Go, testify, Huma, SQLite, go-github/v84

---

### Task 1: Lock nil-normalizer contract with tests

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/github/normalize_test.go`
- Test: `internal/github/normalize_test.go`

- [ ] **Step 1: Add failing tests for nil inputs**

```go
func TestNormalizePRNilInputReturnsNil(t *testing.T) {
	pr := NormalizePR(7, nil)
	require.Nil(t, pr)
}

func TestNormalizeIssueNilInputReturnsNil(t *testing.T) {
	issue := NormalizeIssue(10, nil)
	require.Nil(t, issue)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/github -run 'TestNormalize(PR|Issue)NilInputReturnsNil' -shuffle=on`
Expected: FAIL because current code returns non-nil structs

- [ ] **Step 3: Write minimal implementation**

```go
if ghPR == nil {
	return nil
}

if ghIssue == nil {
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/github -run 'TestNormalize(PR|Issue)NilInputReturnsNil' -shuffle=on`
Expected: PASS

### Task 2: Guard sync call sites against nil normalized payloads

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/github/sync_test.go`
- Modify: `internal/github/sync.go`
- Test: `internal/github/sync_test.go`

- [ ] **Step 1: Add failing sync tests for nil issue/PR payloads**

```go
func TestSyncMRReturnsErrorWhenClientReturnsNilPR(t *testing.T) {
	// setup syncer + SQLite DB + repo
	// mock GetPullRequest to return (nil, nil)
	// assert error contains "client returned nil pull request"
	// assert DB has no MR row for number
}

func TestSyncIssueReturnsErrorWhenClientReturnsNilIssue(t *testing.T) {
	// setup syncer + SQLite DB + repo
	// mock GetIssue to return (nil, nil)
	// assert error contains "client returned nil issue"
	// assert DB has no issue row for number
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/github -run 'TestSync(MRReturnsErrorWhenClientReturnsNilPR|IssueReturnsErrorWhenClientReturnsNilIssue)' -shuffle=on`
Expected: FAIL because current normalizers can still feed bad rows in some paths

- [ ] **Step 3: Update sync callers to guard nil normalized results**

```go
normalized := NormalizePR(repoID, ghPR)
if normalized == nil {
	return fmt.Errorf("get MR %s/%s#%d: client returned nil pull request", owner, name, number)
}
```

```go
normalized := NormalizeIssue(repoID, ghIssue)
if normalized == nil {
	return fmt.Errorf("get issue %s/%s#%d: client returned nil issue", owner, name, number)
}
```

- [ ] **Step 4: Repeat nil guards for bulk/index/backfill/closed-refresh paths before upsert or label replacement**

```go
if normalized == nil {
	return fmt.Errorf("sync issue #%d: client returned nil issue", number)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/github -run 'TestSync(MRReturnsErrorWhenClientReturnsNilPR|IssueReturnsErrorWhenClientReturnsNilIssue)' -shuffle=on`
Expected: PASS

### Task 3: Add API regression tests for missing clients and nil fallback payloads

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: `internal/server/api_test.go`
- Modify: `internal/server/huma_routes.go`
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Add failing HTTP tests**

```go
func TestSetIssueGitHubStateReturns404WhenNoClientConfigured(t *testing.T) {
	// start server with tracked repo but no client for host
	// call PATCH issue state endpoint
	// assert 404
}

func TestSetPRGitHubStateNilFallbackPayloadDoesNotCorruptDB(t *testing.T) {
	// seed MR row
	// EditPullRequest returns 422
	// GetPullRequest returns (nil, nil)
	// assert non-200 response
	// assert stored MR remains unchanged
}

func TestSetIssueGitHubStateNilFallbackPayloadDoesNotCorruptDB(t *testing.T) {
	// seed issue row
	// EditIssue returns 422
	// GetIssue returns (nil, nil)
	// assert non-200 response
	// assert stored issue remains unchanged
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server -run 'TestSet(IssueGitHubStateReturns404WhenNoClientConfigured|PRGitHubStateNilFallbackPayloadDoesNotCorruptDB|IssueGitHubStateNilFallbackPayloadDoesNotCorruptDB)' -shuffle=on`
Expected: FAIL until handlers explicitly treat nil fallback payload as error path

- [ ] **Step 3: Tighten handler fallback behavior**

```go
if fetchErr == nil && ghPR == nil {
	return nil, huma.Error502BadGateway("GitHub API returned no pull request")
}
```

```go
if fetchErr == nil && ghIssue == nil {
	return nil, huma.Error502BadGateway("GitHub API returned no issue")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server -run 'TestSet(IssueGitHubStateReturns404WhenNoClientConfigured|PRGitHubStateNilFallbackPayloadDoesNotCorruptDB|IssueGitHubStateNilFallbackPayloadDoesNotCorruptDB)' -shuffle=on`
Expected: PASS

### Task 4: Full verification

**TDD scenario:** Modifying tested code — run existing tests first

**Files:**
- Modify: none
- Test: `internal/github/normalize_test.go`, `internal/github/sync_test.go`, `internal/server/api_test.go`

- [ ] **Step 1: Run focused package tests**

Run: `go test ./internal/github ./internal/server -shuffle=on`
Expected: PASS

- [ ] **Step 2: Run broader project verification if package tests pass**

Run: `make test-short`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add docs/plans/2026-04-14-nil-normalizer-error-handling-design.md docs/plans/2026-04-14-nil-normalizer-error-handling.md internal/github/normalize.go internal/github/sync.go internal/github/normalize_test.go internal/github/sync_test.go internal/server/huma_routes.go internal/server/api_test.go
git commit -m "fix: fail on nil github payloads"
```
