#!/usr/bin/env bash
set -euo pipefail

LINT_BASE_SHA="$1"
mode="${2:-}"
[[ $# -eq 1 || ($# -eq 2 && "$mode" == --worktree) ]] || exit 1
[[ "$LINT_BASE_SHA" =~ ^[0-9a-f]{40}$ ]] || exit 1
revisions=("$LINT_BASE_SHA")
if [[ "$mode" != --worktree ]]; then
  revisions+=(HEAD)
fi
{
  git --no-optional-locks diff --no-ext-diff --no-textconv --relative --name-only -z --diff-filter=ACMRT "${revisions[@]}" -- '*.go' || exit $?
  if [[ "$mode" == --worktree ]]; then
    git ls-files --others --exclude-standard -z -- '*.go'
  fi
} | (
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
