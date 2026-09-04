#!/usr/bin/env bash
set -euo pipefail
ROOT="/Users/jie/Code/Gin-Vben-Admin"
BASE="e62fe9b"
PATCH="$ROOT/docs/verification/system-settings-refactor-2026-09-05/patch.diff"
cd "$ROOT"
if [[ "${1:-}" == "--check" ]]; then
  /opt/homebrew/bin/git apply --reverse --check "$PATCH"
  echo "ROLLBACK_CHECK_OK"
  exit 0
fi
[[ "$(/opt/homebrew/bin/git rev-parse --show-toplevel)" == "$ROOT" ]]
[[ "$(/opt/homebrew/bin/git rev-parse HEAD^)" == "$(/opt/homebrew/bin/git rev-parse "$BASE")" ]]
[[ -z "$(/opt/homebrew/bin/git status --porcelain)" ]]
echo "Reverting the single system-settings refactor commit from HEAD."
/opt/homebrew/bin/git revert --no-edit HEAD
