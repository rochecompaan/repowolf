#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
checker="$SCRIPT_DIR/check-manifest-platforms.sh"
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-manifest-test.XXXXXX")

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$tmpdir"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

valid='{"manifests":[{"platform":{"os":"linux","architecture":"arm64"}},{"platform":{"os":"linux","architecture":"amd64"}}]}'
missing_arm64='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}'
extra_platform='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}},{"platform":{"os":"windows","architecture":"amd64"}}]}'

printf '%s\n' "$valid" | "$checker"

for fixture in "$missing_arm64" "$extra_platform"; do
  if printf '%s\n' "$fixture" | "$checker" >"$tmpdir/output" 2>&1; then
    echo "expected manifest platform validation to fail" >&2
    exit 1
  fi
  grep -F 'OCI manifest platforms do not match' "$tmpdir/output"
done
