# Dev compose task design

## Summary
Add a `mise` task for local Docker compose development that fetches a GitHub token from host `gh auth token`, injects it into compose, and mounts host middleman config read-only into backend container.

## Goals
- One command for compose dev startup.
- Reuse host `~/.config/middleman/config.toml`.
- Keep SQLite state in Docker volume, not host config directory.
- Avoid installing `gh` inside container.

## Chosen approach
- Add `mise` task `dev-compose` that runs:
  - `MIDDLEMAN_GITHUB_TOKEN="$(gh auth token)" docker compose up --build`
- Update `compose.yml` backend service to:
  - mount host `~/.config/middleman/config.toml` to `/data/config.toml:ro`
  - set `MIDDLEMAN_HOME=/data`
  - stop passing `-config docker/dev-config.toml`
- Document compose behavior in `README.md`.

## Why
- Host shell can access `gh auth token`; container cannot.
- `MIDDLEMAN_HOME=/data` makes default config path `/data/config.toml`.
- Default `data_dir` also becomes `/data`, so SQLite remains in `middleman-data` volume.
- Read-only config mount avoids accidental container edits to host config.

## Constraints
- Host config should not override `data_dir` away from `/data`, or compose will store SQLite elsewhere.
- Compose stack still listens on host ports `18090` and `15173`.
- No change to non-compose local workflow.

## Non-goals
- No `gh` installation in Docker image.
- No whole-directory mount of `~/.config/middleman`.
- No change to runtime port defaults.

## Verification
- Render compose config with `docker compose config` using env injection.
- Confirm `mise run dev-compose` task exists in `mise.toml`.
- Validate docs/examples match behavior.
