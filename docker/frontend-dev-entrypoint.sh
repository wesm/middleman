#!/bin/sh

set -eu

cd /app/frontend

if [ -n "${BUN_INSTALL_FLAGS:-}" ]; then
  # Intentional word splitting for CLI flags.
  bun install ${BUN_INSTALL_FLAGS}
else
  bun install
fi

if [ -n "${FRONTEND_DEV_ARGS:-}" ]; then
  # Intentional word splitting for CLI args.
  exec bun run dev -- ${FRONTEND_DEV_ARGS}
fi

exec bun run dev
