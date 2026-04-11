# Comment Editor Mentions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub-style `@` and `#` autocomplete to pull request and issue comment editors while preserving plain-text markdown submission and existing draft behavior.

**Architecture:** Add a repo-scoped autocomplete API backed by existing SQLite data, then replace the duplicated textarea markup with a shared Tiptap-powered editor component that still stores and submits plain text. Keep the surrounding comment box submit logic intact and verify the new behavior through DB, API, component, and e2e-style tests.

**Tech Stack:** Go, Huma, SQLite, Svelte 5, Tiptap, Vitest, Testing Library, testify

---

### Task 1: Add DB suggestion query coverage

**Files:**
- Modify: `internal/db/types.go`
- Modify: `internal/db/queries.go`
- Test: `internal/db/queries_test.go`

- [ ] **Step 1: Write the failing DB tests**

```go
func TestListCommentAutocompleteUsers(t *testing.T) {
    ctx := context.Background()
    d := openTestDB(t)

    repoID, err := d.UpsertRepo(ctx, "github.com", "acme", "widget")
    require.NoError(t, err)

    prID, err := d.UpsertMergeRequest(ctx, &MergeRequest{
        RepoID: repoID, PlatformID: 1001, Number: 12,
        URL: "https://github.com/acme/widget/pull/12",
        Title: "Polish mentions", Author: "alice",
        State: "open", HeadBranch: "feature", BaseBranch: "main",
        CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
    })
    require.NoError(t, err)
    require.NoError(t, d.EnsureKanbanState(ctx, prID))
    require.NoError(t, d.UpsertMREvents(ctx, []MREvent{{
        MergeRequestID: prID, EventType: "comment", Author: "albert",
        CreatedAt: time.Now().UTC(), DedupeKey: "mr-comment-1",
    }}))

    issueID, err := d.UpsertIssue(ctx, &Issue{
        RepoID: repoID, PlatformID: 2001, Number: 7,
        URL: "https://github.com/acme/widget/issues/7",
        Title: "Mention bug", Author: "alex",
        State: "open", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
    })
    require.NoError(t, err)
    require.NoError(t, d.UpsertIssueEvents(ctx, []IssueEvent{{
        IssueID: issueID, EventType: "comment", Author: "alice",
        CreatedAt: time.Now().UTC(), DedupeKey: "issue-comment-1",
    }}))

    users, err := d.ListCommentAutocompleteUsers(ctx, "github.com", "acme", "widget", "al", 10)
    require.NoError(t, err)
    assert.Equal(t, []string{"alice", "albert", "alex"}, users)
}
```

- [ ] **Step 2: Run the DB test to verify it fails**

Run: `go test ./internal/db -run TestListCommentAutocompleteUsers`
Expected: FAIL with an undefined type or method for comment autocomplete users.

- [ ] **Step 3: Write the minimal DB types and query code**

```go
type CommentAutocompleteReference struct {
    Kind   string
    Number int
    Title  string
    State  string
}
```

```go
func (d *DB) ListCommentAutocompleteUsers(
    ctx context.Context,
    platformHost, owner, name, query string,
    limit int,
) ([]string, error) {
    // Query authors from merge requests, issues, mr events, and issue events
    // for the requested repo, score prefix matches first, then last activity,
    // then login, and return distinct logins.
}
```

- [ ] **Step 4: Add the failing `#` suggestion test**

```go
func TestListCommentAutocompleteReferences(t *testing.T) {
    ctx := context.Background()
    d := openTestDB(t)

    seedPR(t, d, "acme", "widget", 12)
    seedIssue(t, d, "acme", "widget", 7, "Mention bug")

    refs, err := d.ListCommentAutocompleteReferences(ctx, "github.com", "acme", "widget", "1", 10)
    require.NoError(t, err)
    assert.Equal(t, "pull", refs[0].Kind)
    assert.Equal(t, 12, refs[0].Number)
}
```

- [ ] **Step 5: Run the DB reference test to verify it fails**

Run: `go test ./internal/db -run TestListCommentAutocompleteReferences`
Expected: FAIL with an undefined method for comment autocomplete references.

- [ ] **Step 6: Write the minimal reference query implementation**

```go
func (d *DB) ListCommentAutocompleteReferences(
    ctx context.Context,
    platformHost, owner, name, query string,
    limit int,
) ([]CommentAutocompleteReference, error) {
    // Union pull requests and issues for the repo, match number prefix or title,
    // and order numeric prefix matches before title-only matches.
}
```

- [ ] **Step 7: Run the DB package tests**

Run: `go test ./internal/db`
Expected: PASS.

### Task 2: Add the repo autocomplete API

**Files:**
- Modify: `internal/server/api_types.go`
- Modify: `internal/server/huma_routes.go`
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Write the failing API test**

```go
func TestAPICommentAutocomplete(t *testing.T) {
    srv, database := setupTestServer(t)
    client := setupTestClient(t, srv)

    seedPR(t, database, "acme", "widget", 12)
    seedIssue(t, database, "acme", "widget", 7, "Mention bug")

    resp, err := client.HTTP.GetReposByOwnerByNameCommentAutocompleteWithResponse(
        context.Background(),
        "acme", "widget",
        &generated.GetReposByOwnerByNameCommentAutocompleteParams{
            Trigger: "#",
            Query: "1",
            Limit: ptrTo(10),
        },
    )
    require.NoError(t, err)
    require.Equal(t, http.StatusOK, resp.StatusCode())
    require.NotNil(t, resp.JSON200)
    assert.NotEmpty(t, resp.JSON200.References)
}
```

- [ ] **Step 2: Run the API test to verify it fails**

Run: `go test ./internal/server -run TestAPICommentAutocomplete`
Expected: FAIL because the route or generated client surface does not exist yet.

- [ ] **Step 3: Add the API types and route**

```go
type commentAutocompleteResponse struct {
    Users      []string                         `json:"users,omitempty"`
    References []db.CommentAutocompleteReference `json:"references,omitempty"`
}
```

```go
huma.Get(api, "/repos/{owner}/{name}/comment-autocomplete", s.getCommentAutocomplete)
```

```go
func (s *Server) getCommentAutocomplete(
    ctx context.Context,
    input *commentAutocompleteInput,
) (*commentAutocompleteOutput, error) {
    // Validate trigger, clamp limit, call DB helpers, and return users or references.
}
```

- [ ] **Step 4: Regenerate the API client and schema**

Run: `make api-generate`
Expected: generated client/schema updated with the new endpoint.

- [ ] **Step 5: Run the API test to verify it passes**

Run: `go test ./internal/server -run TestAPICommentAutocomplete`
Expected: PASS.

### Task 3: Add the editor-facing frontend API and failing component tests

**Files:**
- Modify: `frontend/package.json`
- Modify: `packages/ui/package.json`
- Create: `packages/ui/src/components/detail/CommentEditor.svelte`
- Test: `frontend/src/lib/components/detail-comment-persistence.test.ts`
- Possibly modify: `frontend/src/lib/components/CommentBoxContextHarness.svelte`

- [ ] **Step 1: Add the failing `@` editor test**

```ts
it("completes usernames from repo suggestions", async () => {
  mockAutocomplete([{ login: "alice" }, { login: "albert" }])
  renderPullCommentBox("octo", "repo", 1)

  const editor = screen.getByPlaceholderText(
    "Write a comment... (Cmd+Enter to submit)",
  )

  await fireEvent.input(editor, { target: { textContent: "@al" } })
  expect(await screen.findByRole("option", { name: /alice/i })).toBeInTheDocument()
})
```

- [ ] **Step 2: Run the frontend test to verify it fails**

Run: `bun run test frontend/src/lib/components/detail-comment-persistence.test.ts`
Expected: FAIL because no suggestion popup exists.

- [ ] **Step 3: Add editor dependencies and create the shared editor component**

```ts
"dependencies": {
  "@tiptap/core": "3.22.3",
  "@tiptap/extension-document": "3.22.3",
  "@tiptap/extension-paragraph": "3.22.3",
  "@tiptap/extension-placeholder": "3.22.3",
  "@tiptap/extension-text": "3.22.3",
  "@tiptap/suggestion": "3.22.3",
  "svelte-tiptap": "3.0.1"
}
```

```svelte
<EditorContent {editor} />
```

```ts
// Keep the editor content synchronized to the plain string draft value.
// Detect @/# triggers and fetch repo-scoped suggestions.
// Insert literal text like @alice and #123 when a suggestion is accepted.
```

- [ ] **Step 4: Add the failing `#` keyboard-selection test**

```ts
it("completes pull request and issue references with keyboard selection", async () => {
  mockAutocomplete([{ kind: "pull", number: 12, title: "Polish mentions", state: "open" }])
  renderIssueCommentBox("octo", "repo", 1)

  const editor = screen.getByPlaceholderText(
    "Write a comment... (Cmd+Enter to submit)",
  )

  await fireEvent.input(editor, { target: { textContent: "See #1" } })
  await fireEvent.keyDown(editor, { key: "ArrowDown" })
  await fireEvent.keyDown(editor, { key: "Enter" })

  expect(getCommentDraft("issue", "octo", "repo", 1)).toContain("#12")
})
```

- [ ] **Step 5: Run the frontend test to verify it fails**

Run: `bun run test frontend/src/lib/components/detail-comment-persistence.test.ts`
Expected: FAIL because `#` completion and keyboard selection are not implemented.

- [ ] **Step 6: Finish the shared editor implementation**

```ts
function acceptSuggestion(item: SuggestionItem): void {
  const inserted = item.type === "user" ? `@${item.login}` : `#${item.number}`
  // Replace the active trigger token and preserve surrounding text.
}
```

- [ ] **Step 7: Run the component test file**

Run: `bun run test frontend/src/lib/components/detail-comment-persistence.test.ts`
Expected: PASS.

### Task 4: Wire the editor into both comment boxes without regressions

**Files:**
- Modify: `packages/ui/src/components/detail/CommentBox.svelte`
- Modify: `packages/ui/src/components/detail/IssueCommentBox.svelte`
- Modify: `packages/ui/src/components/detail/comment-drafts.svelte.ts`
- Test: `frontend/src/lib/components/detail-comment-persistence.test.ts`

- [ ] **Step 1: Write the failing submit regression test**

```ts
it("still submits with Cmd+Enter from the editor", async () => {
  const submit = vi.fn(async () => {})
  render(CommentBoxContextHarness, {
    props: { kind: "pull", submitComment: submit },
  })

  const editor = screen.getByPlaceholderText(
    "Write a comment... (Cmd+Enter to submit)",
  )
  await fireEvent.input(editor, { target: { textContent: "hello @alice" } })
  await fireEvent.keyDown(editor, { key: "Enter", metaKey: true })

  await waitFor(() => expect(submit).toHaveBeenCalledWith("octo", "repo", 1, "hello @alice"))
})
```

- [ ] **Step 2: Run the regression test to verify it fails**

Run: `bun run test frontend/src/lib/components/detail-comment-persistence.test.ts -t "still submits with Cmd+Enter from the editor"`
Expected: FAIL until the new editor is wired through the existing handlers.

- [ ] **Step 3: Replace textarea markup with the shared editor in both wrappers**

```svelte
<CommentEditor
  owner={owner}
  name={name}
  value={body}
  disabled={isPostingCurrent}
  onsubmit={() => void handleSubmit()}
  oninput={setDraftValue}
/>
```

- [ ] **Step 4: Run the comment persistence test file again**

Run: `bun run test frontend/src/lib/components/detail-comment-persistence.test.ts`
Expected: PASS, including draft persistence and disabled-state regressions.

### Task 5: Verify end-to-end behavior and commit

**Files:**
- Modify: any generated API artifacts touched by `make api-generate`
- Verify: repo worktree

- [ ] **Step 1: Run focused backend verification**

Run: `go test ./internal/db ./internal/server`
Expected: PASS.

- [ ] **Step 2: Run focused frontend verification**

Run: `cd frontend && bun run test`
Expected: PASS.

- [ ] **Step 3: Run frontend typecheck**

Run: `cd frontend && bun run typecheck`
Expected: PASS.

- [ ] **Step 4: Review the final diff**

Run: `git status --short && git diff --stat`
Expected: only the planned editor, API, tests, and generated artifacts are changed.

- [ ] **Step 5: Commit the implementation**

```bash
git add frontend/package.json packages/ui/package.json packages/ui/src/components/detail/CommentBox.svelte packages/ui/src/components/detail/IssueCommentBox.svelte packages/ui/src/components/detail/CommentEditor.svelte internal/db/types.go internal/db/queries.go internal/db/queries_test.go internal/server/api_types.go internal/server/huma_routes.go internal/server/api_test.go docs/superpowers/plans/2026-04-11-comment-editor-mentions.md
git commit -m "feat: add comment editor mentions autocomplete"
```

## Self-Review

- Spec coverage: backend suggestion data, repo-scoped API, shared editor, plain-text presentation, keyboard behavior, draft persistence, and verification tasks are all represented.
- Placeholder scan: no `TODO` or `TBD` placeholders remain.
- Type consistency: the plan uses `CommentAutocompleteReference`, a repo-scoped autocomplete route, and a shared `CommentEditor` component consistently.
