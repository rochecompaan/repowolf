#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
smoke="$SCRIPT_DIR/smoke-image.sh"
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-image-smoke-test.XXXXXX")

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$tmpdir"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$tmpdir/bin"
cat >"$tmpdir/bin/nix" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'nix %s\n' "$*" >>"$FAKE_LOG"
if [ "$*" != 'build .#ociImage --no-link --print-out-paths' ]; then
  echo "unexpected nix arguments: $*" >&2
  exit 99
fi
printf '%s\n' "$FAKE_IMAGE"
EOF

cat >"$tmpdir/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >>"$FAKE_LOG"
case "${1:-}" in
  load)
    [ "$#" -eq 3 ]
    [ "$2" = -i ]
    [ "$3" = "$FAKE_IMAGE" ]
    ;;
  image)
    [ "${2:-}" = inspect ]
    printf '%s\n' "$FAKE_ARCH"
    ;;
  run)
    [ "$*" = 'run --rm repowolf:mvp --version' ]
    printf '%s\n' 'repowolf test'
    ;;
  *)
    echo "unexpected docker arguments: $*" >&2
    exit 99
    ;;
esac
EOF

chmod +x "$tmpdir/bin/nix" "$tmpdir/bin/docker"
export PATH="$tmpdir/bin:$PATH"
export FAKE_LOG="$tmpdir/commands.log"
export FAKE_IMAGE="$tmpdir/repowolf-image.tar"
: >"$FAKE_LOG"

FAKE_ARCH=amd64 "$smoke" amd64
FAKE_ARCH=arm64 "$smoke" arm64
grep -F 'nix build .#ociImage --no-link --print-out-paths' "$FAKE_LOG"
grep -F 'docker load -i' "$FAKE_LOG"
grep -F 'docker image inspect' "$FAKE_LOG"
grep -F 'docker run --rm repowolf:mvp --version' "$FAKE_LOG"

if FAKE_ARCH=arm64 "$smoke" amd64 >"$tmpdir/output" 2>&1; then
  echo "expected architecture mismatch" >&2
  exit 1
fi
grep -F 'OCI image architecture mismatch' "$tmpdir/output"

if "$smoke" windows-amd64 >"$tmpdir/output" 2>&1; then
  echo "expected unsupported architecture error" >&2
  exit 1
fi
grep -F 'unsupported OCI architecture' "$tmpdir/output"

if "$smoke" >"$tmpdir/output" 2>&1; then
  echo "expected usage error" >&2
  exit 1
fi
grep -F 'usage: smoke-image' "$tmpdir/output"
