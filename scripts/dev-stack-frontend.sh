#!/usr/bin/env sh

set -eu

mkdir -p tmp/logs
cd frontend
bun install ${BUN_INSTALL_FLAGS:-}
exec bun run dev -- ${FRONTEND_ARGS:-}
