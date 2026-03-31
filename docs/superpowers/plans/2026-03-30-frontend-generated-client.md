# Frontend Generated Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the handwritten frontend API wrappers with a generated runtime client while keeping the Svelte and TypeScript frontend fully type checked.

**Architecture:** Generate a runtime client from the checked-in OpenAPI document alongside the existing generated schema, keep only one tiny handwritten construction module for base-path-aware client setup, and migrate frontend call sites to generated operations directly. If generated usage is awkward, fix the OpenAPI source rather than adding handwritten endpoint wrappers.

**Tech Stack:** Bun, TypeScript, Svelte 5, `openapi-fetch`, `openapi-typescript`, Huma, `svelte-check`

---

## File map

- Modify: `Makefile`
  - Extend API generation so the frontend runtime client is regenerated together with the existing schema.
- Modify: `frontend/package.json`
  - Ensure the runtime generation toolchain is represented only if needed by the chosen generation path.
- Modify: `frontend/bun.lock`
  - Capture dependency updates from the generation workflow if they occur.
- Modify: `frontend/src/lib/api/generated/schema.ts`
  - Regenerated schema after any contract changes.
- Create: `frontend/src/lib/api/generated/client.ts`
  - Checked-in generated runtime client output.
- Create: `frontend/src/lib/api/runtime.ts`
  - Tiny handwritten module for base-path-aware generated client construction and low-level shared setup only.
- Delete: `frontend/src/lib/api/client.ts`
  - Remove handwritten endpoint wrappers.
- Delete: `frontend/src/lib/api/activity.ts`
  - Remove handwritten activity wrapper.
- Modify: frontend call sites that currently import the handwritten wrappers
  - Update to direct generated operation usage through the tiny runtime module.
- Modify: `CLAUDE.md`
  - Document the generated frontend runtime client workflow if the repo guidance should mention it.

## Frontend call-site inventory

Before editing, identify all current imports of the handwritten API wrappers. Expect changes across Svelte components and stores that currently depend on:

- `frontend/src/lib/api/client.ts`
- `frontend/src/lib/api/activity.ts`

Likely target files include detail views, stores, and action components that call:

- list/get pull APIs
- issue APIs
- repo APIs
- sync APIs
- activity APIs
- starred APIs

## Task 1: Add frontend runtime client generation

**Files:**
- Modify: `Makefile`
- Modify: `frontend/package.json`
- Modify: `frontend/bun.lock`
- Create: `frontend/src/lib/api/generated/client.ts`

- [ ] **Step 1: Write the failing existence check for the generated runtime client**

Run:

```bash
test -f frontend/src/lib/api/generated/client.ts
```

Expected:

- command exits non-zero because the generated runtime client does not exist yet

- [ ] **Step 2: Choose the generation command and wire it into repo tooling**

Use the existing OpenAPI document at `frontend/openapi/openapi.json` and generate a runtime client under `frontend/src/lib/api/generated/client.ts`.

If `openapi-typescript` already supports the needed runtime output directly in this repo version, prefer that. Otherwise use the minimal supported generation tool that works cleanly with the existing checked-in schema workflow.

The generation step added to `Makefile` should conceptually look like:

```make
api-generate:
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out frontend/openapi/openapi.json
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out internal/apiclient/spec/openapi.json -version 3.0
	cd frontend && bunx openapi-typescript openapi/openapi.json -o src/lib/api/generated/schema.ts
	cd frontend && <runtime-client-generation-command> openapi/openapi.json -o src/lib/api/generated/client.ts
	GOCACHE="$${GOCACHE:-/tmp/middleman-gocache}" go generate ./internal/apiclient/generated
```

- [ ] **Step 3: Regenerate the frontend API artifacts**

Run:

```bash
make api-generate
```

Expected:

- `frontend/src/lib/api/generated/client.ts` exists
- the generation command completes successfully

- [ ] **Step 4: Verify the generated runtime client compiles under frontend type checking**

Run:

```bash
cd frontend && bun run check
```

Expected:

- PASS, or at worst only unrelated pre-existing failures

- [ ] **Step 5: Commit the generated frontend runtime client scaffold**

Run:

```bash
git add Makefile frontend/package.json frontend/bun.lock frontend/src/lib/api/generated/schema.ts frontend/src/lib/api/generated/client.ts
git commit -m "feat: generate frontend api client"
```

Expected:

- commit succeeds with only the frontend runtime generation scaffold and artifacts

## Task 2: Add the tiny runtime construction module

**Files:**
- Create: `frontend/src/lib/api/runtime.ts`

- [ ] **Step 1: Write the failing import check for the runtime module**

Create a temporary import in one call site or a tiny focused frontend test/check use:

```ts
import { apiClient } from "$lib/api/runtime";
void apiClient;
```

Run:

```bash
cd frontend && bun run check
```

Expected:

- FAIL because `$lib/api/runtime` does not exist yet

- [ ] **Step 2: Implement the minimal client-construction module**

Create `frontend/src/lib/api/runtime.ts` with only base-path-aware setup. The target shape is:

```ts
import createClient from "openapi-fetch";
import type { paths } from "$lib/api/generated/schema";

const basePath = (window.__BASE_PATH__ ?? "/").replace(/\/$/, "");

export const apiClient = createClient<paths>({
  baseUrl: `${basePath}/api/v1`,
});
```

If the generated runtime client uses a different import shape than `openapi-fetch`, adapt this file accordingly, but keep it limited to construction and low-level shared setup only.

- [ ] **Step 3: Re-run frontend type checking**

Run:

```bash
cd frontend && bun run check
```

Expected:

- PASS, or failures only from still-unmigrated call sites

- [ ] **Step 4: Commit the runtime setup module**

Run:

```bash
git add frontend/src/lib/api/runtime.ts
git commit -m "feat: add frontend api runtime"
```

Expected:

- commit succeeds with only the tiny runtime setup module

## Task 3: Migrate pull, repo, sync, and starred call sites

**Files:**
- Delete: `frontend/src/lib/api/client.ts`
- Modify: every frontend file importing pull/repo/sync/starred functions from the handwritten API module

- [ ] **Step 1: Inventory the current call sites**

Run:

```bash
rg -n 'from "\\$lib/api/client|from "\\./client|from "\\$lib/api/activity|from "\\./activity' frontend/src
```

Expected:

- a concrete list of files that still depend on the handwritten wrappers

- [ ] **Step 2: Replace one pull-oriented call site first and run type check**

Take one representative call site, for example a pull list or pull detail consumer, and replace wrapper usage like:

```ts
const pulls = await listPulls(params);
```

with direct generated usage through the runtime client, shaped like:

```ts
const { data, error } = await apiClient.GET("/pulls", {
  params: { query: params },
});
if (error) throw new Error(JSON.stringify(error));
const pulls = data ?? [];
```

Run:

```bash
cd frontend && bun run check
```

Expected:

- FAIL or PASS depending on remaining unmigrated imports, but the new direct usage should type check

- [ ] **Step 3: Migrate the remaining pull, repo, sync, and starred consumers**

Replace handwritten wrapper imports/calls with direct generated operations for:

- list/get pulls
- set kanban state
- post comment
- get repo
- approve PR
- ready for review
- merge PR
- list repos
- trigger sync
- get sync status
- set starred
- unset starred

Do not recreate wrapper functions with the old names.

- [ ] **Step 4: Delete the handwritten client wrapper file**

Remove `frontend/src/lib/api/client.ts` once no imports remain.

- [ ] **Step 5: Run frontend type checking after the migration**

Run:

```bash
cd frontend && bun run check
```

Expected:

- PASS for the migrated areas

- [ ] **Step 6: Commit the first call-site migration batch**

Run:

```bash
git add frontend/src
git commit -m "refactor: use generated frontend api operations"
```

Expected:

- commit succeeds with the pull/repo/sync/starred migration batch

## Task 4: Migrate activity and issue call sites and remove duplicate API types

**Files:**
- Delete: `frontend/src/lib/api/activity.ts`
- Modify: frontend files importing issue or activity wrapper functions
- Modify: any local frontend type definitions duplicated by generated response types

- [ ] **Step 1: Migrate activity consumers**

Replace handwritten activity calls like:

```ts
const activity = await listActivity(params);
```

with direct generated calls through the runtime client for `GET /activity`.

- [ ] **Step 2: Migrate issue consumers**

Replace handwritten issue calls like:

```ts
await listIssues(params);
await getIssue(owner, name, number);
await postIssueComment(owner, name, number, body);
```

with direct generated operations and typed request/response handling.

- [ ] **Step 3: Remove or narrow duplicated local API-facing types**

Delete or reduce local types that only mirror generated request/response types, but keep truly UI-specific types that are not API-schema duplicates.

- [ ] **Step 4: Delete the handwritten activity wrapper file**

Remove `frontend/src/lib/api/activity.ts` once no imports remain.

- [ ] **Step 5: Run full frontend type checking**

Run:

```bash
cd frontend && bun run check
```

Expected:

- PASS

- [ ] **Step 6: Commit the remaining frontend API migration**

Run:

```bash
git add frontend/src
git commit -m "refactor: remove handwritten frontend api wrappers"
```

Expected:

- commit succeeds with the activity/issue migration and wrapper removals

## Task 5: Final verification and repo guidance

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update repo guidance for the frontend generated runtime client**

Document that:

- frontend runtime API calls should use the generated client
- handwritten endpoint wrappers should not be reintroduced
- `make api-generate` is the supported regeneration path
- frontend completion requires `make frontend-check`

- [ ] **Step 2: Run the required final verification**

Run:

```bash
make api-generate
make frontend-check
make lint
GOCACHE=/tmp/middleman-gocache go test ./...
```

Expected:

- all commands pass

- [ ] **Step 3: Commit the final doc and verification-aligned changes**

Run:

```bash
git add CLAUDE.md
git commit -m "docs: document generated frontend api client"
```

Expected:

- commit succeeds after all validation passes

## Self-review

### Spec coverage

- Generated runtime client addition: covered by Task 1.
- Tiny construction module only: covered by Task 2.
- Removal of handwritten endpoint wrappers: covered by Tasks 3 and 4.
- Generated operations used directly: covered by Tasks 3 and 4.
- Full frontend type checking: covered by Tasks 1 through 5, with `make frontend-check` as a required final gate.

No spec gaps found.

### Placeholder scan

- No deferred “TODO” or “implement later” placeholders remain.
- Each task includes exact files, commands, and concrete code shapes.
- The plan avoids “write tests for the above” style placeholders and instead names the actual validation commands.

### Type consistency

- The generated schema source is consistently `frontend/src/lib/api/generated/schema.ts`.
- The runtime setup module is consistently `frontend/src/lib/api/runtime.ts`.
- The handwritten wrappers slated for removal are consistently `frontend/src/lib/api/client.ts` and `frontend/src/lib/api/activity.ts`.

No naming inconsistencies found.
