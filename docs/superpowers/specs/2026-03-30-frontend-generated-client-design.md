# Frontend Generated Client Design

## Goal

Replace the handwritten frontend API wrappers with direct usage of a generated runtime client derived from the checked-in OpenAPI document.

The frontend should stop maintaining endpoint wrappers by hand. Runtime API calls should go through generated operations, with only a tiny shared setup module for client construction. The resulting Svelte and TypeScript code must remain fully type checked.

## Current State

- The backend API is defined with Huma and emits a checked-in OpenAPI document.
- The frontend already has a generated schema at `frontend/src/lib/api/generated/schema.ts`.
- The frontend runtime still uses handwritten fetch wrappers in:
  - `frontend/src/lib/api/client.ts`
  - `frontend/src/lib/api/activity.ts`
- The project already uses Bun tooling and a frontend type-check command via `make frontend-check`.

## Requirements

- Remove handwritten endpoint wrapper logic from the frontend API layer.
- Use a generated runtime client for frontend API calls.
- Keep only a minimal shared setup layer for client construction and shared error handling if needed.
- Prefer fixing awkward generated client usage at the OpenAPI contract level instead of adding handwritten domain wrappers.
- Keep the frontend fully type checked with Svelte and TypeScript checks passing.

## Approaches Considered

### 1. Direct generated client everywhere

Import and use the generated runtime client directly in each call site.

Pros:

- Minimal abstraction
- No handwritten endpoint drift

Cons:

- Repeats client construction or shared request/error handling unless carefully centralized

### 2. Direct generated operations with one tiny shared setup module

Keep a single construction module for base path and generated client instantiation, then call generated operations directly from the rest of the frontend.

Pros:

- No handwritten endpoint wrappers
- Shared base path logic in one place
- Keeps call sites clean enough without hiding the generated surface

Cons:

- Still one small handwritten module exists, though not as an endpoint SDK

### 3. Handwritten adapter layer over the generated client

Keep the generated client behind a handwritten domain API.

Pros:

- Can produce very polished call sites

Cons:

- Reintroduces drift risk
- Adds maintenance cost
- Not justified for an internal API we control

## Decision

Use approach 2.

The frontend will use the generated runtime client directly, with only a tiny shared setup module for constructing that client against the app’s base path and handling any shared low-level concerns.

## Design

### Generated runtime client

Add runtime client generation for the frontend from the checked-in OpenAPI document. The generated output should live alongside the existing generated schema under a generated path in `frontend/src/lib/api/generated/`.

The generated runtime client should become the source of truth for:

- operation method names
- request parameter shapes
- response body shapes

If those generated shapes are awkward enough to harm normal usage, the fix should be made on the Huma/OpenAPI side, not by introducing handwritten endpoint wrappers.

### Shared client setup

Keep one tiny handwritten setup module in the frontend API area. Its job is limited to:

- reading `window.__BASE_PATH__`
- constructing the generated client with the correct `/api/v1` base URL
- optionally centralizing low-level response/error translation if the generated client requires it

This module must not reintroduce handwritten endpoint functions like `listPulls()` or `getIssue()`.

### Frontend call-site migration

Replace the handwritten wrapper imports and calls with direct generated client operation usage.

This applies to all current uses of:

- pull APIs
- issue APIs
- repo APIs
- sync APIs
- activity APIs
- starred APIs

Existing local UI-specific types should be removed or narrowed where they are now duplicated by generated request/response types.

### Type checking and validation

Completion requires the frontend to remain fully type checked.

Validation must include:

- regenerated frontend runtime client artifacts
- `make frontend-check`
- backend tests and lint still passing where affected

If the migration exposes weak typing or schema mismatches, the fix should preserve typed correctness rather than falling back to `any`, unsafe casts, or handwritten duplicate types.

## Risks and mitigations

### Risk: generated runtime client method names are awkward

Mitigation:

- fix operation IDs or schema shapes in the Huma/OpenAPI source
- keep only the tiny construction module, not a handwritten endpoint wrapper layer

### Risk: call-site churn across the Svelte app

Mitigation:

- migrate imports systematically
- keep the setup module stable so only operation calls change

### Risk: generated client introduces a different error surface

Mitigation:

- centralize only low-level error normalization if needed
- do not wrap each endpoint individually

### Risk: frontend type breakage after removing handwritten types

Mitigation:

- treat `make frontend-check` as a required gate
- prefer generated types over duplicated local interfaces

## Scope boundaries

This design includes:

- generated runtime client usage in the frontend
- removal of handwritten frontend endpoint wrappers
- full frontend type-check validation

This design does not include:

- redesigning frontend state management
- public API DTO cleanup beyond what is necessary to make generated usage reasonable
- adding a handwritten frontend SDK layer
