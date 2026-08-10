#!/bin/sh
set -eu

if [ "${1-}" = "--inside" ]; then
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  if command -v ssh >/dev/null 2>&1; then
    exit 1
  fi
  test "$(id -u)" = "65532"
  exit 0
fi
if [ "$#" -ne 0 ]; then
  echo "usage: assert-sandbox-boundary.sh" >&2
  exit 2
fi

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
SCRIPT_PATH="$SCRIPT_DIR/assert-sandbox-boundary.sh"
REPO_ROOT=$(CDPATH='' cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
cd -- "$EXAMPLE_DIR"
docker compose -f compose.yaml -f compose.smoke.yaml run --rm -T \
  --entrypoint sh sandbox -s -- --inside <"$SCRIPT_PATH"
