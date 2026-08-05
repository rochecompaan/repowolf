#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
snapshot="$(mktemp -d)"
trap 'rm -rf "$snapshot"' EXIT
cp -R "$root/gen" "$snapshot/gen"
"$root/scripts/generate.sh"
diff -ru "$snapshot/gen" "$root/gen"
