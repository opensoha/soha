#!/usr/bin/env bash
set -euo pipefail

LINT_BASE_SHA="$1"
[[ "$LINT_BASE_SHA" =~ ^[0-9a-f]{40}$ ]] || exit 1
git diff --no-ext-diff --no-textconv --relative --name-only -z --diff-filter=ACMRT "$LINT_BASE_SHA" HEAD -- '*.go' | (
  files=(--)
  while IFS= read -r -d '' file; do
    [[ -f "./$file" && ! -L "./$file" ]] || continue
    # Generated files are validated by their existing generation checks.
    if awk '/^package[[:space:]]/ {exit} /^\/\/ Code generated .* DO NOT EDIT\.$/ {found=1} END {exit !found}' "./$file"; then
      continue
    fi
    files+=("./$file")
  done
  if ((${#files[@]} > 1)); then
    unformatted=$(gofmt -l "${files[@]}")
    if [[ -n "$unformatted" ]]; then
      printf 'Run gofmt on:\n%s\n' "$unformatted" >&2
      exit 1
    fi
  fi
)
