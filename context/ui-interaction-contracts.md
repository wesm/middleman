# UI Interaction Contracts

Use this document for frontend behavior changes where the risk is not visual
style but stale identity, broken persistence, or surprising interaction
semantics.

## Purpose

- Make behavior-level UI contracts explicit.
- Keep route identity, persisted browser state, and keyboard/pointer semantics
  consistent across the app.
- Prevent narrow regressions that usually show up only after review or in e2e
  flows.

## Identity And Route State

Interactive surfaces must agree on which item is selected.

- Treat `platform_host` as part of PR and issue identity in route state, drawer
  state, and stale-detail guards.
- When host is omitted for the default GitHub host, normalize comparisons so
  `github.com` and an omitted host do not look like different items.
- Use shared named route/item reference types from
  `frontend/src/lib/stores/router.svelte.ts` instead of repeating anonymous
  `{ owner, name, number }`-style shapes.
- When a view changes from item A to item B, reset transient action state that
  could otherwise submit or render against the wrong item.

Examples of transient state that should usually reset on identity change:

- inline edit drafts
- merge/close/reopen dialogs
- approve/review forms
- embedded detail-tab selection when the parent surface owns the item

## Persistence Scope

Persisted controls must state their scope clearly.

- Browser-local preferences belong in `localStorage` only when the behavior is
  intentionally per-browser and not worth server settings.
- URL query state belongs in the route only when deep-linking or back/forward
  navigation is part of the feature contract.
- Server-backed settings belong in the API only when the preference should
  follow the user/config rather than one browser session.

Whenever a control persists, document and test:

- where it persists
- whether it is global, per-view, or per-item
- what happens after navigating away and back

## Nested Interaction Rules

Rows that contain buttons, links, or toggles need clear event ownership.

- Activating a nested control inside a clickable row must not also trigger the
  row's navigation or selection behavior.
- Escape should close drawers, split-detail panels, menus, or modals when that
  surface is currently active.
- Focus-visible states matter for controls that are visually subtle, such as tab
  close buttons or compact action affordances.
- If a component claims menu-like behavior, it must honor the keyboard and focus
  contract of that role. Otherwise, use simpler semantics honestly.

## Filtering And Visibility Rules

Not every visibility control means "remove this entity entirely."

- Controls that toggle detail visibility should preserve the parent row unless
  the feature explicitly removes that category from the result set.
- When two data sources race, prefer the source that matches the user's current
  filter/scope rather than a stale but faster preview.
- Empty states should make it clear when filters, not missing data, are hiding
  results.

## Label Editing

Desktop label editing should reuse existing detail header/meta/chip rows. Do not
add an empty `No labels` row or otherwise consume vertical space when no labels
are assigned; show the compact `Labels` action inline with existing metadata.

The picker opens only for the currently visible PR/issue detail. Compare
provider, platform host, repo path, owner, name, and number before opening or
applying mutation responses. While catalogs are stale or syncing, keep refreshing
non-blockingly. Before sending a replacement label set, filter assigned labels to
labels present in the current catalog so historical labels do not block edits.
Command palette label actions target only the current detail item.

## Testing Expectations

Behavior contracts should usually be tested where the user would notice the
breakage.

- Component tests for local state transitions, event propagation, and route/item
  identity helpers.
- Store tests for persistence scope and normalization logic.
- Playwright/e2e tests for navigation away/back, Escape behavior, nested button
  activation, and other multi-surface flows.

Related docs:

- [`context/ui-design-system.md`](./ui-design-system.md) for visual primitives
  and styling guidance.
- [`context/workspace-runtime-lifecycle.md`](./workspace-runtime-lifecycle.md)
  for runtime-specific workspace tab and shell behavior.
