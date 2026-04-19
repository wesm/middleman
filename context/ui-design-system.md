# UI Design System

Use this document as the intent-level guide for frontend UI work in `middleman`. It should stay short, stable, and useful in model context.

## Purpose

- Keep the app visually coherent.
- Prefer shared primitives over one-off styling.
- Extend semantic tokens and components instead of duplicating UI geometry.

## Design intent

`middleman` is a dense maintainer tool, not a marketing surface.

- Layouts should feel compact, deliberate, and information-rich.
- Visual emphasis should come from hierarchy and semantic color, not oversized controls or decorative effects.
- Light and dark themes should express the same UI language through shared tokens.

## Sources of truth

- Tokens: `frontend/src/app.css`
- Shared primitives: `packages/ui/src/components/shared/`
- This guidance: `context/ui-design-system.md`

## Shared primitives

### Chip

Use `Chip` for compact status and metadata UI.

Intent:

- one shared geometry for small labeled UI
- consistent vertical alignment, spacing, casing, and density
- reusable across detail views, sidebars, and compact status surfaces

Use it for:

- PR/issue state
- CI/review state
- repo and count badges
- other compact metadata markers

Do not create new local `.badge`, `.pill`, or `.chip` geometry when `Chip` fits.

In this repo, the standard term is **chip**, not pill.

### ActionButton

Use `ActionButton` for repeated action styling.

Intent:

- one shared button model for tone, surface, and size
- semantic action styling instead of per-screen button CSS

If a new repeated button treatment is needed, extend `ActionButton` rather than creating another local button pattern.

### GitHubLabels

Use `GitHubLabels` for actual GitHub labels.

Intent:

- keep repository labels distinct from generic status chips
- preserve GitHub-label semantics without collapsing them into a generic badge system

## Tokens and semantics

Use semantic variables instead of hard-coded values whenever possible.

- Surfaces and borders come from the app token set in `frontend/src/app.css`
- Text uses the shared primary / secondary / muted hierarchy
- Accent colors carry meaning, not decoration

Default color intent:

- green: success, open, ready
- amber: pending, draft, warning
- purple: merged, waiting, workflow-secondary status
- red: failure, conflict, destructive status
- blue: focus, active controls, informational emphasis
- teal: workspace/worktree-linked state

## Implementation guidance

Before adding UI styling:

1. Check whether an existing shared primitive already expresses the pattern.
2. If yes, extend that primitive with a semantic variant rather than duplicating layout CSS.
3. If no, add a shared component only when the pattern is clearly reusable.

Local CSS is acceptable for context-specific color or placement. Local CSS should not re-define repeated geometry that belongs in a shared primitive.

## When to add a shared component

Add or promote a shared component when:

- the same UI geometry appears in multiple places
- the same semantic control exists in both list and detail surfaces
- future work would otherwise copy and paste the same styling

Do not create a shared primitive for a one-off visual detail.

## Maintenance rule

If you add a new shared UI component, or materially change the intent of an existing one, you must update `context/ui-design-system.md` in the same turn.

The document should describe:

- what the component is for
- when to use it
- what UI duplication it is meant to prevent

It should not turn into implementation notes or a style dump.

## Testing expectation

When UI work changes shared primitives or visible interaction patterns, add or update regression coverage, preferably at the user-visible flow where the duplication or inconsistency previously appeared.
