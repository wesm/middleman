#!/usr/bin/env bash
set -u

output_file="${MIDDLEMAN_E2E_OUTPUT_FILE:-test-results/e2e.log}"
output_dir="$(dirname "$output_file")"
mkdir -p "$output_dir"
case "$output_file" in
  /*) display_file="$output_file" ;;
  *) display_file="$(pwd -P)/$output_file" ;;
esac

{
  printf '[%s] bun run test:e2e\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf '$ playwright test --config=playwright-e2e.config.ts'
  for arg in "$@"; do
    printf ' %q' "$arg"
  done
  printf '\n\n'

  playwright test --config=playwright-e2e.config.ts "$@"
} >"$output_file" 2>&1

status=$?
if [ "$status" -eq 0 ]; then
  printf '[e2e] pass; full output: %s\n' "$display_file"
else
  printf '[e2e] fail (exit %s); full output: %s\n' "$status" "$display_file" >&2
fi

exit "$status"
