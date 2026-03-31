#!/bin/sh

set -eu

state_dir="tmp/air"
input_hash_file="$state_dir/openapi-inputs.sha256"
frontend_spec="frontend/openapi/openapi.json"
backend_spec="internal/apiclient/spec/openapi.json"
frontend_schema="frontend/src/lib/api/generated/schema.ts"
frontend_client="frontend/src/lib/api/generated/client.ts"

mkdir -p "$state_dir"

compute_inputs_hash() {
  {
    printf '%s\n' "go.mod" "go.sum"
    find cmd/middleman-openapi internal/server -type f -name '*.go' | sort
  } | while IFS= read -r path; do
    [ -f "$path" ] || continue
    shasum -a 256 "$path"
  done | shasum -a 256 | awk '{print $1}'
}

write_if_changed() {
  destination="$1"
  source="$2"

  if [ -f "$destination" ] && cmp -s "$destination" "$source"; then
    rm -f "$source"
    return 1
  fi

  mv "$source" "$destination"
  return 0
}

generate_frontend_client() {
  tmp_client="$(mktemp "$state_dir/frontend-client.XXXXXX")"

  cat > "$tmp_client" <<'EOF'
/**
 * This file was auto-generated from frontend/openapi/openapi.json.
 * Do not make direct changes to the file.
 */

import createClient, { type ClientOptions } from "openapi-fetch";
import type { paths } from "./schema";

export function createAPIClient(baseUrl: string, options: Pick<ClientOptions, "fetch" | "querySerializer"> = {}) {
  return createClient<paths>({ baseUrl, ...options });
}
EOF

  write_if_changed "$frontend_client" "$tmp_client" >/dev/null 2>&1 || true
}

generate_api_artifacts() {
  tmp_frontend_spec="$(mktemp "$state_dir/frontend-openapi.XXXXXX")"
  tmp_backend_spec="$(mktemp "$state_dir/backend-openapi.XXXXXX")"
  frontend_changed=0

  GOCACHE="${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out "$tmp_frontend_spec"
  GOCACHE="${GOCACHE:-/tmp/middleman-gocache}" go run ./cmd/middleman-openapi -out "$tmp_backend_spec" -version 3.0

  if write_if_changed "$frontend_spec" "$tmp_frontend_spec"; then
    frontend_changed=1
  fi

  write_if_changed "$backend_spec" "$tmp_backend_spec" >/dev/null 2>&1 || true

  if [ "$frontend_changed" -eq 1 ]; then
    tmp_schema="$(mktemp "$state_dir/frontend-schema.XXXXXX")"
    (
      cd frontend
      bunx openapi-typescript openapi/openapi.json -o "../$tmp_schema"
    )
    write_if_changed "$frontend_schema" "$tmp_schema" >/dev/null 2>&1 || true
    generate_frontend_client
  fi

  GOCACHE="${GOCACHE:-/tmp/middleman-gocache}" go generate ./internal/apiclient/generated
}

current_inputs_hash="$(compute_inputs_hash)"
previous_inputs_hash=""

if [ -f "$input_hash_file" ]; then
  previous_inputs_hash="$(cat "$input_hash_file")"
fi

if [ "$current_inputs_hash" != "$previous_inputs_hash" ]; then
  generate_api_artifacts
  printf '%s\n' "$current_inputs_hash" > "$input_hash_file"
fi

go build -o ./tmp/middleman ./cmd/middleman
