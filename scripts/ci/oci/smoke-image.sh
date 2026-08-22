#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: smoke-image <amd64|arm64>" >&2
  exit 2
fi
expected_arch=$1
case "$expected_arch" in
  amd64|arm64) ;;
  *)
    echo "unsupported OCI architecture: $expected_arch" >&2
    exit 2
    ;;
esac

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
cd -- "$REPO_ROOT"

image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
actual_arch=$(docker image inspect --format '{{.Architecture}}' repowolf:mvp)
if [ "$actual_arch" != "$expected_arch" ]; then
  echo "OCI image architecture mismatch: expected $expected_arch, got $actual_arch" >&2
  exit 1
fi
docker run --rm repowolf:mvp --version
