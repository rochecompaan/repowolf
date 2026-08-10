#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
TEST_DIR="$EXAMPLE_DIR/state/test"
REPOWOLF_IMAGE=${REPOWOLF_IMAGE:-repowolf:mvp}
rendered_config=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  if [ -n "$rendered_config" ]; then
    rm -f -- "$rendered_config"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$REPO_ROOT"
mkdir -p -- "$TEST_DIR"
nix develop -c env CGO_ENABLED=0 go build \
  -o "$TEST_DIR/fake-ssh" "$SCRIPT_DIR/fixtures/fake-ssh"
: >"$TEST_DIR/ssh-argv"
rendered_config=$(mktemp "$EXAMPLE_DIR/state/config.smoke.XXXXXX")
awk '
  $0 == "  ssh: null" { print "  ssh: /run/repowolf/test/fake-ssh"; next }
  { print }
' "$EXAMPLE_DIR/state/config.yaml" >"$rendered_config"
mv -- "$rendered_config" "$EXAMPLE_DIR/state/config.yaml"
rendered_config=

# Expanded by the Alpine shell inside the container, not by this host shell.
# shellcheck disable=SC2016
docker run --rm --user 0:0 \
  -e HOST_UID="$(id -u)" \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml" \
  -v "$TEST_DIR:/test" \
  alpine:3 sh -eu -c '
    chown "$HOST_UID:65532" /config.yaml /test /test/fake-ssh /test/ssh-argv
    chmod 0640 /config.yaml
    chmod 0750 /test
    chmod 0550 /test/fake-ssh
    chmod 0660 /test/ssh-argv
  '
docker run --rm --user 65532:65532 \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml:ro" \
  "$REPOWOLF_IMAGE" config validate --config /config.yaml
