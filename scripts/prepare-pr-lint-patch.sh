#!/usr/bin/env bash
set -euo pipefail

base="$1"
patch="$2"
[[ "$base" =~ ^[0-9a-f]{40}$ ]] || exit 1
git cat-file -e "${base}^{commit}"
git merge-base --is-ancestor "$base" HEAD
# golangci-lint matches patch filenames relative to its working directory.
git diff --no-ext-diff --no-textconv --no-color --binary --relative "$base" HEAD > "$patch"
if [[ ! -s "$patch" ]]; then
  # Shared changes can select a module whose own files are unchanged.
  diff_status=0
  git diff --no-ext-diff --no-textconv --no-relative --quiet "$base" HEAD || diff_status=$?
  test "$diff_status" -eq 1
fi
