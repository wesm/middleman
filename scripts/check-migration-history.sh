#!/usr/bin/env bash
set -euo pipefail

base_ref="${MIDDLEMAN_MIGRATION_BASE_REF:-origin/main}"
migration_dir="${MIDDLEMAN_MIGRATION_DIR:-internal/db/migrations}"

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "migration history check must run inside a git worktree" >&2
  exit 1
fi

if ! git rev-parse --verify --quiet "${base_ref}^{commit}" >/dev/null; then
  cat >&2 <<EOF
Cannot verify migration history because ${base_ref} is unavailable.
Fetch the main branch or set MIDDLEMAN_MIGRATION_BASE_REF to the main-branch ref to compare against.
EOF
  exit 1
fi

violations=()

while IFS=$'\t' read -r status path1 path2; do
  [[ -n "${status}" ]] || continue

  paths=("${path1}")
  case "${status}" in
    C* | R*)
      paths=("${path1}" "${path2}")
      ;;
  esac

  for path in "${paths[@]}"; do
    [[ -n "${path}" ]] || continue
    [[ "${path}" == "${migration_dir}"/* ]] || continue

    if git cat-file -e "${base_ref}:${path}" 2>/dev/null; then
      violations+=("${path}")
    fi
  done
done < <(git diff --cached --name-status -- "${migration_dir}")

if ((${#violations[@]} > 0)); then
  cat >&2 <<EOF
Refusing to commit edits to migrations that already exist on ${base_ref}.

Migrations are append-only once they land on main. Add a new numbered migration instead.

Blocked files:
EOF
  printf '  %s\n' "${violations[@]}" >&2
  exit 1
fi
