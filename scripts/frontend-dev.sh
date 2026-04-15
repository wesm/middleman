#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_dir="$repo_root/tmp/logs"
log_file="$log_dir/frontend-dev.log"

mkdir -p "$log_dir"

cd "$repo_root/frontend"
bun install ${BUN_INSTALL_FLAGS:-}
bun run dev -- "${@}" 2>&1 | tee "$log_file"
