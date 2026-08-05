#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
rm -rf dist
nix develop -c goreleaser release --snapshot --clean
for arch in amd64 arm64; do
  archive="dist/repowolf_linux_${arch}.tar.gz"
  test -s "$archive"
  tar -tzf "$archive" | grep -E '(^|/)repowolf$' >/dev/null
  tar -tzf "$archive" | grep -E '(^|/)repowolf-client$' >/dev/null
done
native="$(go env GOARCH)"
case "$native" in amd64|arm64) ;; *) echo "unsupported smoke architecture: $native" >&2; exit 1 ;; esac
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
tar -xzf "dist/repowolf_linux_${native}.tar.gz" -C "$tmp"
"$tmp/repowolf" --version
set +e
"$tmp/repowolf-client" >/dev/null 2>&1
status=$?
set -e
test "$status" -eq 2
