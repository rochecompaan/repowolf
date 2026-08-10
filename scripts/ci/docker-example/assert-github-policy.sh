#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
broker_log=$(mktemp "${TMPDIR:-/tmp}/repowolf-broker.XXXXXX.log")
compose=(docker compose -f compose.yaml -f compose.smoke.yaml)

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -f -- "$broker_log"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$EXAMPLE_DIR"
if "${compose[@]}" run --rm sandbox gh repo view --repo rochecompaan/repowolf; then
  echo "expected upstream failure with dummy GH_TOKEN" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep 'github.repository_view' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"accepted"'

if "${compose[@]}" run --rm sandbox gh run list --repo rochecompaan/repowolf; then
  echo "expected policy denial" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep -E '"operation":[[:space:]]*"/repowolf\.v1\.GitHubService/Execute"' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"denied"' \
  | grep -E '"reason":[[:space:]]*"PermissionDenied"'
if grep 'github.run_list' "$broker_log" \
  | grep -qE '"outcome":[[:space:]]*"accepted"'; then
  echo "github.run_list must not be accepted" >&2
  exit 1
fi
