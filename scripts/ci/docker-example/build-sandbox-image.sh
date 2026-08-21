#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
fixture=
server_pid=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [ -n "$fixture" ]; then
    rm -rf -- "$fixture"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$REPO_ROOT"
nix develop -c goreleaser release --snapshot --clean
fixture=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-release-fixture.XXXXXX")
mkdir -p -- "$fixture/snapshot"
cp dist/checksums.txt dist/repowolf_linux_*.tar.gz "$fixture/snapshot/"
nix develop -c go build -o "$fixture/release-server" \
  "$SCRIPT_DIR/fixtures/release-server"
"$fixture/release-server" "$fixture" >"$fixture/server.log" 2>&1 &
server_pid=$!

fixture_ready=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  if (exec 3<>/dev/tcp/127.0.0.1/8765) 2>/dev/null; then
    fixture_ready=1
    break
  fi
  sleep 1
done
if [ "$fixture_ready" -ne 1 ]; then
  cat -- "$fixture/server.log" >&2
  exit 1
fi

docker build --network=host \
  --build-arg REPOWOLF_VERSION=snapshot \
  --build-arg REPOWOLF_RELEASE_ROOT=http://127.0.0.1:8765 \
  -t repowolf-sandbox:local examples/docker/sandbox
