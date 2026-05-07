#!/usr/bin/env bash
set -euo pipefail

: "${GITEA_URL:?GITEA_URL is required}"

python3 - <<'PY'
import json
import os
import time
import urllib.request

base_url = os.environ["GITEA_URL"].rstrip("/")
deadline = time.monotonic() + int(os.environ.get("GITEA_READY_TIMEOUT_SECONDS", "300"))
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(f"{base_url}/api/v1/version", timeout=10) as resp:
            if resp.status == 200 and json.loads(resp.read().decode()):
                raise SystemExit(0)
    except Exception:
        time.sleep(2)
raise SystemExit(f"Gitea did not become ready at {base_url}")
PY
