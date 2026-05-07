#!/usr/bin/env bash
set -euo pipefail

export GITEALIKE_ENV_PREFIX=GITEA
export GITEALIKE_BINARY=gitea
"$(dirname "$0")/../gitealike/bootstrap.py" "$@"
