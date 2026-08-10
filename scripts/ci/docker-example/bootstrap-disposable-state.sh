#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
REPOWOLF_IMAGE=${REPOWOLF_IMAGE:-repowolf:mvp}
tmp_root=${TMPDIR:-/tmp}
ssh_test=$(mktemp -d "$tmp_root/repowolf-ci-ssh.XXXXXX")
ssh_effective=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$ssh_test"
  if [ -n "$ssh_effective" ]; then
    rm -f -- "$ssh_effective"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

ssh_effective=$(mktemp "$tmp_root/repowolf-ssh-effective.XXXXXX")
ssh-keygen -q -t ed25519 -N '' -f "$ssh_test/id_ed25519"
printf 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestOnly\n' \
  >"$ssh_test/known_hosts"
printf 'GH_TOKEN=dummy-ci-token\n' >"$EXAMPLE_DIR/.env"
(
  cd -- "$EXAMPLE_DIR"
  REPOWOLF_IMAGE="$REPOWOLF_IMAGE" \
  REPOWOLF_REPO=rochecompaan/repowolf \
  REPOWOLF_SSH_KEY="$ssh_test/id_ed25519" \
  REPOWOLF_KNOWN_HOSTS="$ssh_test/known_hosts" \
    ./bootstrap.sh
)

docker run --rm --user 65532:65532 \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml:ro" \
  "$REPOWOLF_IMAGE" config validate --config /config.yaml
docker run --rm \
  -v "$EXAMPLE_DIR/state/ssh:/tmp/.ssh:ro" \
  --entrypoint ssh "$REPOWOLF_IMAGE" -G github.com >"$ssh_effective"
grep -E '^identityagent[[:space:]]+none$' "$ssh_effective"
grep -E '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' "$ssh_effective"
grep -E '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' "$ssh_effective"
