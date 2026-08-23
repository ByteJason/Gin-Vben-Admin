#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
PATCH="$ROOT/.dev-docs/current/evidence/v1.0-manual-acceptance.patch"
MODE="${1:---check}"

if [[ ! -s "$PATCH" ]]; then
  echo "ROLLBACK_PATCH_MISSING=$PATCH" >&2
  exit 1
fi

case "$MODE" in
  --check)
    git -C "$ROOT" apply --unidiff-zero --check --reverse "$PATCH"
    echo "ROLLBACK_MANUAL_ACCEPTANCE_CHECK_OK"
    ;;
  --apply)
    git -C "$ROOT" apply --unidiff-zero --check --reverse "$PATCH"
    git -C "$ROOT" apply --unidiff-zero --reverse "$PATCH"
    echo "ROLLBACK_MANUAL_ACCEPTANCE_APPLY_OK"
    ;;
  *)
    echo "usage: $0 --check|--apply" >&2
    exit 2
    ;;
esac
