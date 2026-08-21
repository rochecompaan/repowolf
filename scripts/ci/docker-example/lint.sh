#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
bash_scripts=(
  "$SCRIPT_DIR/lint.sh"
  "$SCRIPT_DIR/build-sandbox-image.sh"
  "$SCRIPT_DIR/bootstrap-disposable-state.sh"
  "$SCRIPT_DIR/install-fake-ssh-fixture.sh"
  "$SCRIPT_DIR/assert-github-policy.sh"
  "$SCRIPT_DIR/assert-fake-ssh.sh"
  "$REPO_ROOT/examples/docker/bootstrap.sh"
  "$REPO_ROOT/examples/docker/wait-for-broker.sh"
)
posix_script="$SCRIPT_DIR/assert-sandbox-boundary.sh"

bash -n "${bash_scripts[@]}"
sh -n "$posix_script"
shellcheck "${bash_scripts[@]}" "$posix_script"
