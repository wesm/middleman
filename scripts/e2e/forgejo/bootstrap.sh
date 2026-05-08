#!/usr/bin/env bash
set -euo pipefail

export GITEALIKE_ENV_PREFIX=FORGEJO
export GITEALIKE_BINARY=forgejo
"$(dirname "$0")/../gitealike/bootstrap.py" "$@"
