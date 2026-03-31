# middleman favicon design

## Summary

Create a favicon for `middleman` that communicates "central review hub" first and GitHub PR flow second. The approved direction is a compact, vector-first mark derived from concept `E2`.

## Goals

- Give the app a distinctive favicon instead of a generic browser default.
- Reflect the product's role as a central review desk for pull requests and issues across multiple repositories.
- Keep the mark legible at favicon sizes, especially `16x16` and `32x32`.
- Preserve a restrained, developer-tool visual language that fits the existing product UI.

## Constraints

- The icon should not read as a generic Git logo.
- The primary semantic read should be "review hub" rather than "source control operation."
- The mark should use a balanced level of detail: enough structure to feel intentional, but not so much that it collapses at small sizes.
- The final asset set should be derived from one master vector so all variants stay visually consistent.

## Approved concept

The favicon is a rounded-square dark tile containing:

- Three small colored repository nodes arranged around a bright central review node.
- Fine inbound connectors from the outer nodes toward the center.
- A center-originating squared J-like hook that turns into a bottom-left arrow.

The central node and inbound connectors communicate the app as a review hub. The J-like outbound arrow adds directional PR-flow energy without making the icon feel like a stock Git merge symbol.

## Visual language

- Background: dark, neutral tile that works in both browser chrome and the app's existing dark-first aesthetic.
- Accent nodes: restrained blue, amber, and green points to echo existing product accent colors.
- Main stroke: light foreground stroke for the center hook and arrow to maximize contrast at small sizes.
- Geometry: slightly squared, compact composition rather than a tall or circular silhouette.

## Asset plan

Implementation should produce:

- One source SVG for the approved mark.
- Standard favicon wiring in `frontend/index.html`.
- At least one raster fallback generated from the same source geometry for compatibility.

The SVG should be the primary asset. Raster output should be treated as a compatibility derivative, not a separately designed logo.

## Integration plan

- Add the source icon asset under the frontend static asset path.
- Reference the favicon assets from `frontend/index.html`.
- Keep the integration minimal and reversible.

## Verification

- Build the frontend to confirm the favicon assets are emitted correctly.
- Check the mark visually at small sizes, especially `16x16` and `32x32`.
- Confirm the linked favicon loads from the built app shell.

## Out of scope

- Full brand system work.
- Alternate app icon packs for every platform.
- Reworking broader UI branding outside the favicon addition.
