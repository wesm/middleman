# Generated Go API Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a checked-in Go client generated from the Huma OpenAPI document and use it as the default client for integration-style backend tests.

**Architecture:** Keep Huma as the single API contract source, generate a checked-in OpenAPI document, and derive both frontend and Go client artifacts from it. Wrap the generated Go client with a thin handwritten helper package for `httptest` ergonomics, then migrate server contract tests to that client while keeping a few raw HTTP assertions for transport-only behavior.

**Tech Stack:** Go, Huma, `oapi-codegen`, `go generate`, `testify`, `httptest`, Bun, `openapi-typescript`, `prek`

---

## File map

- Modify: `cmd/middleman-openapi/main.go`
  - Keep OpenAPI generation as the canonical entrypoint and make it capable of producing the Go-client-compatible checked-in spec output.
- Modify: `Makefile`
  - Expand `api-generate` so it regenerates frontend and Go client artifacts in one command.
- Modify: `prek.toml`
  - Keep pre-commit regeneration aligned with the expanded API generation flow.
- Modify: `go.mod`
  - Add generation/runtime dependencies required for the Go client path.
- Modify: `go.sum`
  - Record dependency changes.
- Modify: `CLAUDE.md`
  - Document Huma and OpenAPI-generated clients as the preferred integration-test path.
- Create: `.gitattributes`
  - Mark generated artifacts with `linguist-generated=true`.
- Create: `internal/apiclient/spec/openapi.json`
  - Checked-in Go-client generation input if a separate Go-friendly spec path is required.
- Create: `internal/apiclient/generated/generate.go`
  - `go:generate` driver for `oapi-codegen`.
- Create: `internal/apiclient/generated/config.yaml`
  - Stable checked-in `oapi-codegen` config.
- Create: `internal/apiclient/generated/client.gen.go`
  - Checked-in generated Go client.
- Create: `internal/apiclient/client.go`
  - Thin handwritten wrapper for creating and using the generated client in tests.
- Modify: `internal/server/api_test.go`
  - Migrate integration-style API tests to the generated client.
- Modify: `internal/server/api_types.go`
  - Keep shared API response types in the new focused file if the migration work needs to finish the earlier split cleanly.
- Delete: `internal/server/handlers.go`
  - Remove stale API model definitions if still present in the tree.
- Delete: `internal/server/handlers_activity.go`
  - Remove stale activity API model definitions if still present in the tree.
- Delete: `internal/server/handlers_test.go`
  - Finalize the test rename to `api_test.go` if still uncommitted in the tree.
- Optional modify: `frontend/openapi/openapi.json`
  - Regenerate if the canonical spec output changes.
- Optional modify: `frontend/src/lib/api/generated/schema.ts`
  - Regenerate with the updated API generation flow.

### Task 1: Finalize the server API type/test file split already sitting in the worktree

**Files:**
- Create: `internal/server/api_types.go`
- Create: `internal/server/api_test.go`
- Delete: `internal/server/handlers.go`
- Delete: `internal/server/handlers_activity.go`
- Delete: `internal/server/handlers_test.go`

- [ ] **Step 1: Verify the partial rename/split state before changing anything else**

Run:

```bash
git status --short
```

Expected:

- the worktree shows `internal/server/api_types.go` and `internal/server/api_test.go` as new files
- the old `handlers*.go` server API files show as deleted

- [ ] **Step 2: Run the server tests that cover the renamed file before any further edits**

Run:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -count=1
```

Expected:

- PASS

- [ ] **Step 3: Commit the server file split cleanly**

Run:

```bash
git add internal/server/api_types.go internal/server/api_test.go internal/server/handlers.go internal/server/handlers_activity.go internal/server/handlers_test.go
git commit -m "refactor: split server api types and tests"
```

Expected:

- commit succeeds with only the server type/test split staged

### Task 2: Add the Go client generation inputs and toolchain

**Files:**
- Modify: `cmd/middleman-openapi/main.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/apiclient/spec/openapi.json`
- Create: `internal/apiclient/generated/config.yaml`
- Create: `internal/apiclient/generated/generate.go`

- [ ] **Step 1: Write the failing generation driver test by asserting the generate package is missing**

Run:

```bash
test -f internal/apiclient/generated/generate.go
```

Expected:

- command exits non-zero because the generator driver does not exist yet

- [ ] **Step 2: Add the checked-in `oapi-codegen` config**

Create `internal/apiclient/generated/config.yaml` with:

```yaml
package: generated
generate:
  client: true
  models: true
output: client.gen.go
output-options:
  skip-prune: true
```

- [ ] **Step 3: Add the `go:generate` driver**

Create `internal/apiclient/generated/generate.go` with:

```go
package generated

//go:generate go tool oapi-codegen --config config.yaml ../spec/openapi.json
```

- [ ] **Step 4: Make the OpenAPI generator produce the checked-in Go-client input**

Update `cmd/middleman-openapi/main.go` so it can write to an arbitrary path and is usable for both the frontend spec output and the Go client spec output without duplicating server setup logic.

The resulting main flow should still look like:

```go
spec, err := server.NewOpenAPI().MarshalJSON()
if err != nil {
	fmt.Fprintf(os.Stderr, "marshal openapi: %v\n", err)
	os.Exit(1)
}

if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
	fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(out), err)
	os.Exit(1)
}

if err := os.WriteFile(out, append(spec, '\n'), 0o644); err != nil {
	fmt.Fprintf(os.Stderr, "write %s: %v\n", out, err)
	os.Exit(1)
}
```

If a separate Go-client-friendly OpenAPI 3.0.3 output is required, make that an explicit deterministic output path rather than an implicit test-time transform.

- [ ] **Step 5: Add `oapi-codegen` to the module toolchain**

Run:

```bash
go get github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

Expected:

- `go.mod` and `go.sum` update successfully

- [ ] **Step 6: Generate the Go client inputs and verify they exist**

Run:

```bash
GOCACHE=/tmp/middleman-gocache go run ./cmd/middleman-openapi -out internal/apiclient/spec/openapi.json
go generate ./internal/apiclient/generated
```

Expected:

- `internal/apiclient/spec/openapi.json` exists
- `internal/apiclient/generated/client.gen.go` exists

- [ ] **Step 7: Commit the generation scaffold**

Run:

```bash
git add cmd/middleman-openapi/main.go go.mod go.sum internal/apiclient/spec/openapi.json internal/apiclient/generated/config.yaml internal/apiclient/generated/generate.go internal/apiclient/generated/client.gen.go
git commit -m "feat: add generated go api client scaffold"
```

Expected:

- commit succeeds with only the generation scaffolding changes

### Task 3: Integrate Go client generation into repo tooling and generated-file labeling

**Files:**
- Modify: `Makefile`
- Modify: `prek.toml`
- Create: `.gitattributes`
- Optional modify: `frontend/openapi/openapi.json`
- Optional modify: `frontend/src/lib/api/generated/schema.ts`

- [ ] **Step 1: Write the failing command check for the Go client artifact from `make api-generate`**

Run:

```bash
make api-generate
test -f internal/apiclient/generated/client.gen.go
```

Expected:

- if `make api-generate` does not yet generate the Go client, the second command fails

- [ ] **Step 2: Expand `Makefile` to regenerate all API artifacts**

Update the `api-generate` target so it runs the canonical spec generation plus both client generators. The target should end up conceptually like:

```make
api-generate:
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out frontend/openapi/openapi.json
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out internal/apiclient/spec/openapi.json
	cd frontend && bunx openapi-typescript openapi/openapi.json -o src/lib/api/generated/schema.ts
	go generate ./internal/apiclient/generated
```

- [ ] **Step 3: Update pre-commit so regeneration covers the Go client too**

Adjust `prek.toml` so the existing API regeneration hook continues to call `make api-generate` and therefore refreshes frontend and Go client artifacts together.

- [ ] **Step 4: Add generated-file Linguist labeling**

Create `.gitattributes` with entries like:

```gitattributes
frontend/src/lib/api/generated/* linguist-generated=true
internal/apiclient/generated/* linguist-generated=true
internal/apiclient/spec/openapi.json linguist-generated=true
```

Include any additional checked-in machine-generated API files that should be classified the same way.

- [ ] **Step 5: Run the generation command and verify all generated files are refreshed**

Run:

```bash
make api-generate
git status --short
```

Expected:

- generated artifacts appear updated only if the inputs changed
- no missing generated Go client files

- [ ] **Step 6: Commit tooling and generated-file labeling**

Run:

```bash
git add Makefile prek.toml .gitattributes frontend/openapi/openapi.json frontend/src/lib/api/generated/schema.ts internal/apiclient/spec/openapi.json internal/apiclient/generated/config.yaml internal/apiclient/generated/generate.go internal/apiclient/generated/client.gen.go
git commit -m "build: generate go api client artifacts"
```

Expected:

- commit succeeds with tooling and generated artifact updates

### Task 4: Add the thin handwritten Go API client wrapper

**Files:**
- Create: `internal/apiclient/client.go`
- Test: `internal/server/api_test.go`

- [ ] **Step 1: Write the failing compile check for the wrapper constructor**

Add this temporary compile use in `internal/server/api_test.go`:

```go
func TestAPIClientConstruction(t *testing.T) {
	srv, _ := setupTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	_ = apiclient.New(ts.URL)
}
```

Run:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -run TestAPIClientConstruction -count=1
```

Expected:

- FAIL with an undefined `apiclient.New`

- [ ] **Step 2: Implement the minimal wrapper**

Create `internal/apiclient/client.go` with a thin wrapper shaped like:

```go
package apiclient

import (
	"net/http"
	"strings"

	"github.com/wesm/middleman/internal/apiclient/generated"
)

type Client struct {
	HTTP *generated.ClientWithResponses
}

func New(baseURL string) (*Client, error) {
	client, err := generated.NewClientWithResponses(strings.TrimRight(baseURL, "/") + "/api/v1")
	if err != nil {
		return nil, err
	}
	return &Client{HTTP: client}, nil
}

func NewWithHTTPClient(baseURL string, httpClient *http.Client) (*Client, error) {
	client, err := generated.NewClientWithResponses(
		strings.TrimRight(baseURL, "/")+"/api/v1",
		generated.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, err
	}
	return &Client{HTTP: client}, nil
}
```

- [ ] **Step 3: Re-run the focused compile test**

Run:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -run TestAPIClientConstruction -count=1
```

Expected:

- PASS

- [ ] **Step 4: Commit the wrapper**

Run:

```bash
git add internal/apiclient/client.go internal/server/api_test.go
git commit -m "feat: add go api client wrapper"
```

Expected:

- commit succeeds with the thin wrapper and the initial construction test

### Task 5: Migrate server integration tests to the generated client

**Files:**
- Modify: `internal/server/api_test.go`

- [ ] **Step 1: Replace one existing raw request test with a client-driven failing test**

Convert `TestAPIListPulls` to client-driven setup first. The target shape is:

```go
func TestAPIListPulls(t *testing.T) {
	srv, database := setupTestServer(t)
	seedPR(t, database, "acme", "widget", 1)

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client, err := apiclient.NewWithHTTPClient(ts.URL, ts.Client())
	require.NoError(t, err)

	resp, err := client.HTTP.ListPullsWithResponse(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode())
	require.NotNil(t, resp.JSON200)
	require.Len(t, *resp.JSON200, 1)
}
```

Run:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -run TestAPIListPulls -count=1
```

Expected:

- FAIL until imports and response assertions are fully migrated

- [ ] **Step 2: Migrate the rest of the integration-style API tests**

Update the tests that exercise normal API contract behavior so they use the generated client methods instead of `httptest.NewRequest` and recorder plumbing.

The migrated set should include:

- list pulls
- get pull
- get missing pull
- set kanban state
- list repos
- sync status
- trigger sync
- ready for review
- set starred
- unset starred

Keep raw `httptest` requests only for tests specifically about low-level transport behavior, such as the raw OpenAPI document response.

- [ ] **Step 3: Keep `testify` assertions and tighten response typing**

Ensure the migrated tests prefer patterns like:

```go
require.NoError(t, err)
require.Equal(t, http.StatusOK, resp.StatusCode())
require.NotNil(t, resp.JSON200)
```

Use typed response bodies returned by the generated client instead of manually decoding JSON in these tests.

- [ ] **Step 4: Run the server package tests after the migration**

Run:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -count=1
```

Expected:

- PASS

- [ ] **Step 5: Commit the migrated server tests**

Run:

```bash
git add internal/server/api_test.go
git commit -m "test: use generated go api client"
```

Expected:

- commit succeeds with the server test migration only

### Task 6: Update repo guidance and validate coverage

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update repo guidance for Huma and generated clients**

Add concise guidance in `CLAUDE.md` stating:

- the project uses Huma for the web framework
- OpenAPI-derived clients are preferred for integration-style API tests
- new Go tests should use `testify`
- API regeneration should happen via `go generate` and `make api-generate`

- [ ] **Step 2: Capture before/after coverage for the server package**

Run before the final test migration commit range is considered complete:

```bash
GOCACHE=/tmp/middleman-gocache go test ./internal/server -coverprofile=/tmp/server.cover
go tool cover -func=/tmp/server.cover | tail -n 1
```

Expected:

- a total coverage percentage is printed for `internal/server`

Repeat after the migration and compare the totals. If coverage drops, add or restore client-driven assertions until it is at least maintained.

- [ ] **Step 3: Run full verification**

Run:

```bash
make api-generate
make lint
GOCACHE=/tmp/middleman-gocache go test ./...
```

Expected:

- all commands pass

- [ ] **Step 4: Commit the documentation and final verification state**

Run:

```bash
git add CLAUDE.md
git commit -m "docs: document generated api clients"
```

Expected:

- commit succeeds after full verification passes

## Self-review

### Spec coverage

- OpenAPI-to-Go-client generation: covered by Tasks 2 and 3.
- Thin handwritten wrapper for test ergonomics: covered by Task 4.
- Integration-test migration to generated client: covered by Task 5.
- Generated-file Linguist labeling: covered by Task 3.
- Documentation updates: covered by Task 6.
- Coverage preservation: covered by Task 6.

No spec gaps found.

### Placeholder scan

- No `TODO`, `TBD`, or deferred “implement later” markers remain.
- Each task lists exact files and concrete commands.
- Code-bearing steps include the intended code shape rather than a prose-only placeholder.

### Type consistency

- The plan consistently uses `internal/apiclient/generated` for generated code.
- The handwritten wrapper consistently uses `internal/apiclient`.
- Server integration tests consistently target `internal/server/api_test.go`.

No naming inconsistencies found.
