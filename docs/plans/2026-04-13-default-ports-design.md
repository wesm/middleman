# Default ports design

## Summary
Change middleman default ports to avoid collisions with agentsview defaults and test ports.

## Current overlap
- agentsview app default: `8080`
- agentsview Vite dev default: `5173`
- agentsview Playwright/web server: `8090`
- middleman app default: `8090`
- middleman Vite dev default: `5173`

## Chosen ports
- middleman app default: `8091`
- middleman Vite dev default: `5174`

## Why
- Avoid collision with agentsview local app and test flows.
- Keep changes small: one-step bump from existing defaults.
- Preserve loopback-only behavior and existing Docker/dev override ports.

## Scope
Update:
- Go config defaults
- default config template
- library defaults used by embedded mode
- Vite dev server default and API proxy fallback
- docs and example config
- tests asserting default port values

## Non-goals
- No Docker compose port changes
- No reverse proxy behavior changes
- No runtime port auto-discovery changes

## Verification
- Run relevant Go tests covering config and server settings.
- Run frontend build/config check for Vite config validity.
