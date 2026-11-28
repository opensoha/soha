#!/usr/bin/env bash
set -euo pipefail

base="$1"
patch="$2"
mode="${3:-}"
[[ $# -eq 2 || ($# -eq 3 && "$mode" == --worktree) ]] || exit 1
[[ "$base" =~ ^[0-9a-f]{40}$ ]] || exit 1
git cat-file -e "${base}^{commit}"
git merge-base --is-ancestor "$base" HEAD

patch_diff() {
  git --no-optional-locks -c core.quotePath=false diff --no-ext-diff --no-textconv \
    --no-color --binary --src-prefix=a/ --dst-prefix=b/ "$@"
}

lint_paths() {
  # The lint filter reads the raw path after "+++ b/". Git appends a tab for spaces.
  LC_ALL=C awk '
    /^\+\+\+ b\// {sub(/\t$/, "")}
    /^\+\+\+ "/ && /\.go"$/ {
      print "Cannot safely filter Go lint for a quoted Git path: " $0 > "/dev/stderr"
      exit 1
    }
    {print}
  '
}

if [[ "$mode" == --worktree ]]; then
  patch_diff --relative "$base" -- | lint_paths > "$patch"
  git ls-files --others --exclude-standard -z -- '*.go' | while IFS= read -r -d '' file; do
    status=0
    diff=$(patch_diff --no-index -- /dev/null "$file") || status=$?
    # A successful --no-index diff for a new file exits 1 and writes a patch.
    if [[ "$status" -ne 1 || -z "$diff" ]]; then
      printf 'Could not prepare a lint patch for %s\n' "$file" >&2
      exit 1
    fi
    printf '%s\n' "$diff" | lint_paths >> "$patch"
  done
  exit 0
fi
patch_diff --relative "$base" HEAD | lint_paths > "$patch"
if [[ ! -s "$patch" ]]; then
  # Shared changes can select a module whose own files are unchanged.
  diff_status=0
  git diff --no-ext-diff --no-textconv --no-relative --quiet "$base" HEAD || diff_status=$?
  test "$diff_status" -eq 1
fi
