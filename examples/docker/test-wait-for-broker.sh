#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
WAIT="$SCRIPT_DIR/wait-for-broker.sh"
huge=999999999999999999999999999999999999999999999999
tmpdir=$(mktemp -d /tmp/repowolf-wait-test.XXXXXX)
cleanup() {
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT INT TERM

expect_usage() {
  label=$1
  shift
  output="$tmpdir/$label.out"
  set +e
  "$@" >"$output" 2>&1
  status=$?
  set -e
  if [ "$status" -ne 2 ]; then
    cat "$output" >&2
    echo "$label: expected usage status 2, got $status" >&2
    exit 1
  fi
  grep -q '^usage: wait-for-broker.sh' "$output"
}

expect_usage huge-port "$WAIT" 127.0.0.1 "$huge" 1
expect_usage huge-attempts timeout 2 "$WAIT" 127.0.0.1 1 "$huge"
expect_usage zero-port "$WAIT" 127.0.0.1 0 1
expect_usage leading-zero-port "$WAIT" 127.0.0.1 01 1
expect_usage leading-zero-attempts "$WAIT" 127.0.0.1 1 01

output="$tmpdir/timeout.out"
set +e
timeout 2 "$WAIT" 127.0.0.1 1 1 >"$output" 2>&1
status=$?
set -e
if [ "$status" -ne 1 ]; then
  cat "$output" >&2
  echo "timeout: expected status 1, got $status" >&2
  exit 1
fi
grep -F 'docker compose logs repowolf' "$output"
