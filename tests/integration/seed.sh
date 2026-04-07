#!/usr/bin/env bash
set -euo pipefail

ENDPOINT="${1:-http://localhost:7373}"

echo "Waiting for roborev daemon..."
for i in $(seq 1 30); do
  if curl -sf "$ENDPOINT/api/status" >/dev/null 2>&1; then
    echo "Daemon ready"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "Daemon not ready after 30 attempts" >&2
    exit 1
  fi
  sleep 1
done

echo "Seeding test data..."

REPO_DIR=$(mktemp -d)
cd "$REPO_DIR"
git init -q
git -c user.name="test" -c user.email="test@test.com" commit --allow-empty -m "initial commit"

curl -sf -X POST "$ENDPOINT/api/repos/register" \
  -H 'Content-Type: application/json' \
  -d "{\"path\":\"$REPO_DIR\"}" \
  || echo "Repo registration may not be available, continuing..."

echo "Seed data complete"
echo "Roborev status:"
curl -sf "$ENDPOINT/api/status" \
  | python3 -m json.tool 2>/dev/null \
  || curl -sf "$ENDPOINT/api/status"
