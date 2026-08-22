#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
scripts=(
  "$SCRIPT_DIR/check-manifest-platforms.sh"
  "$SCRIPT_DIR/test-check-manifest-platforms.sh"
  "$SCRIPT_DIR/smoke-image.sh"
  "$SCRIPT_DIR/test-smoke-image.sh"
  "$SCRIPT_DIR/lint.sh"
)

bash -n "${scripts[@]}"
shellcheck "${scripts[@]}"
"$SCRIPT_DIR/test-check-manifest-platforms.sh"
"$SCRIPT_DIR/test-smoke-image.sh"
