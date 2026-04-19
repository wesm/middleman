# UI Design System

This document is the source of truth for routine UI work in `middleman`. Use it when adding or changing frontend UI so the app keeps one visual language instead of accumulating one-off styles.

## Goals

- Reuse shared primitives before adding local CSS.
- Extend the existing token system instead of hard-coding colors, spacing, or typography.
- Keep light and dark themes aligned through the same semantic variables.
- Prefer small, composable components in `packages/ui/src/components/shared/` when a pattern appears in more than one place.

## Tokens

Global UI tokens live in `frontend/src/app.css`.

### Color tokens

Use semantic variables instead of raw hex values whenever possible:

- Surfaces: `--bg-primary`, `--bg-surface`, `--bg-surface-hover`, `--bg-inset`
- Borders: `--border-default`, `--border-muted`
- Text: `--text-primary`, `--text-secondary`, `--text-muted`
- Accents: `--accent-blue`, `--accent-amber`, `--accent-purple`, `--accent-green`, `--accent-red`, `--accent-teal`
- Workflow/status tokens: `--kanban-*`, `--review-*`, `--verdict-*`

If a new semantic color is needed, add a token in both light and dark themes. Do not add a light-theme-only color.

### Shape and elevation

- Radius tokens: `--radius-sm`, `--radius-md`, `--radius-lg`
- Shadows: `--shadow-sm`, `--shadow-md`, `--shadow-lg`

Prefer token radii for panels, inputs, and buttons. The shared `Chip` component is an intentional exception and uses a capsule radius for compact metadata.

### Typography

- Sans: `--font-sans`
- Mono: `--font-mono`
- Base app text is `13px` with `line-height: 1.5`

Use the mono stack only for code, refs, branches, diffs, and machine-oriented content. UI labels, buttons, and chips should stay on the sans stack unless there is a clear data-density reason not to.

## Shared primitives

### Chip

`packages/ui/src/components/shared/Chip.svelte`

Use `Chip` for compact status and metadata UI. In this repo, the standard term is **chip**, not pill.

Use it for:

- PR/issue state
- CI state
- review state
- repo/count badges
- compact metadata markers

Do not create new local `.badge`, `.pill`, or `.chip` geometry when `Chip` can express the UI.

Rules:

- Default chip text is uppercase for status-style labels.
- Use `uppercase={false}` for literal text such as repo slugs or counts.
- Use `size="sm"` for sidebar/list density and `size="md"` for detail headers unless a tighter fit is necessary.
- Use `interactive={true}` when the chip behaves like a button.
- Add new color/state variants through semantic classes, not ad hoc inline geometry.

### ActionButton

`packages/ui/src/components/shared/ActionButton.svelte`

Use `ActionButton` for actionable controls instead of hand-rolled button styling when the existing size/tone/surface model fits.

Current model:

- Tones: `neutral`, `success`, `danger`, `info`, `workflow`
- Surfaces: `outline`, `soft`, `solid`
- Sizes: `sm`, `md`

If a new action treatment is needed in more than one place, extend `ActionButton` instead of creating a new one-off button style.

### GitHubLabels

`packages/ui/src/components/shared/GitHubLabels.svelte`

Use `GitHubLabels` for actual GitHub labels. Do not replace GitHub labels with generic chips unless the UI is intentionally not showing repository labels.

## Layout rules

- Use `--bg-surface` for cards, drawers, and panels that sit above the app background.
- Use `--bg-inset` for recessed UI such as secondary containers, count chips, and code-ish inline surfaces.
- Use `--border-default` for standard edges and `--border-muted` for lower-contrast internal separation.
- Preserve the app’s dense maintainer-tooling layout. Avoid oversized controls, excessive padding, or marketing-style whitespace.

When designing new layout structures:

- Start from existing patterns in `packages/ui/src/components/detail/`, `sidebar/`, `layout/`, and `workspace/`
- Keep list rows compact and information-dense
- Use subtle hover and selection states rather than dramatic motion or large transforms

## Status and semantic color usage

Default semantic mapping:

- Green: success, open, ready, passing
- Amber: pending, draft, in-progress, warning
- Purple: merged, waiting, workflow/secondary status
- Red: failure, conflict, destructive actions
- Blue: navigation focus, info, active controls
- Teal: worktree/workspace-linked status

Use `color-mix(... transparent)` backgrounds for soft status surfaces to match the rest of the app. Avoid fully saturated fills except for deliberate primary actions such as solid confirmation buttons.

## When to add a shared component

Promote a pattern into `packages/ui/src/components/shared/` when:

- the same geometry appears in two or more places
- the same semantic control exists in both detail and list views
- a future contributor would reasonably copy the styling instead of discovering the existing implementation

Do not create a shared primitive for a single incidental style. Shared components should encode repeated intent, not every bit of CSS.

## Implementation guidance

Before adding new UI styles:

1. Check whether `Chip`, `ActionButton`, `GitHubLabels`, or an existing shared component already covers the pattern.
2. Prefer extending a shared primitive with a semantic variant over duplicating layout CSS locally.
3. If local styling is still needed, keep it limited to context-specific color or spacing, not duplicated geometry.

Examples:

- Good: local class adds a kanban color variant on top of `Chip`
- Bad: a new `.badge` class redefines padding, radius, font size, and casing already owned by `Chip`

## Accessibility and interaction

- Keep `:focus-visible` behavior intact and consistent with `--accent-blue`
- Interactive chips and buttons must use actual buttons unless there is a strong reason otherwise
- Hover-only affordances must still be discoverable and keyboard reachable
- Text in chips and buttons must remain legible in both themes

## Testing expectations for UI work

When a UI change alters user-visible behavior or shared primitives:

- update or add Playwright coverage for the affected flow
- prefer regression tests around shared component contracts when the bug came from duplicated styling
- rebuild embedded frontend artifacts with `make frontend` before relying on e2e screenshots or browser checks against the Go-served app

## Current preferred sources of truth

- Tokens: `frontend/src/app.css`
- Shared primitives: `packages/ui/src/components/shared/`
- This guidance: `context/ui-design-system.md`
