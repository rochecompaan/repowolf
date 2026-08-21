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
if "${compose[@]}" run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git; then
  echo "fake SSH unexpectedly succeeded" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep 'git.upload-pack' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"accepted"'
diff -u "$SCRIPT_DIR/fixtures/expected-ssh-argv.txt" state/test/ssh-argv
grep 'git.upload-pack' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"failed"' \
  | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
