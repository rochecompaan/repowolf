#!/usr/bin/env bash
set -euo pipefail

expected=$(printf '%s\n' linux/amd64 linux/arm64)
actual=$(
  jq -r '
    if (.manifests | type) != "array" then
      error("OCI index does not contain a manifests array")
    else
      [.manifests[].platform | "\(.os)/\(.architecture)"]
      | sort
      | .[]
    end
  '
)

if [ "$actual" != "$expected" ]; then
  printf '%s\n' 'OCI manifest platforms do not match' >&2
  printf 'expected:\n%s\n' "$expected" >&2
  printf 'actual:\n%s\n' "${actual:-<none>}" >&2
  exit 1
fi
