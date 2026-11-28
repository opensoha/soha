#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
base="$1"
patch=$(mktemp "${OPENSOHA_VERIFY_DIR:-${TMPDIR:-/tmp}}/soha-pr-lint.XXXXXX")
trap 'rm -f -- "$patch"' EXIT

bash scripts/prepare-pr-lint-patch.sh "$base" "$patch" --worktree
bash scripts/check-pr-go-format.sh "$base" --worktree

version=$(golangci-lint version --short)
if [[ "$version" != 2.14.0 ]]; then
  printf 'Go lint expects golangci-lint 2.14.0, found %s\n' "$version" >&2
  exit 1
fi
golangci-lint run --timeout=5m --new-from-patch="$patch"
