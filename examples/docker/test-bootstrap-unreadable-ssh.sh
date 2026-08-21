#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
tmpdir=$(mktemp -d /tmp/repowolf-bootstrap-test.XXXXXX)
cleanup() {
  chmod -R u+rwx -- "$tmpdir" 2>/dev/null || true
  rm -rf -- "$tmpdir"
}
trap cleanup EXIT INT TERM

cp "$SCRIPT_DIR/bootstrap.sh" "$tmpdir/bootstrap.sh"
mkdir -p "$tmpdir/config" "$tmpdir/bin"
cp "$SCRIPT_DIR/config/repowolf.yaml" "$tmpdir/config/repowolf.yaml"
cat > "$tmpdir/bin/docker" <<EOF
#!/usr/bin/env bash
: > "$tmpdir/docker-called"
exit 99
EOF
chmod 0755 "$tmpdir/bootstrap.sh" "$tmpdir/config" "$tmpdir/config/repowolf.yaml" "$tmpdir/bin/docker"
printf 'private test key\n' > "$tmpdir/key"
printf 'github.com ssh-ed25519 test\n' > "$tmpdir/known_hosts"
chmod 000 "$tmpdir/key"
chmod 0644 "$tmpdir/known_hosts"

expect_unreadable() {
  label=$1
  key=$2
  known_hosts=$3
  output="$tmpdir/$label.out"
  set +e
  env PATH="$tmpdir/bin:$PATH" \
    REPOWOLF_REPO=rochecompaan/repowolf \
    REPOWOLF_SSH_KEY="$key" \
    REPOWOLF_KNOWN_HOSTS="$known_hosts" \
    "$tmpdir/bootstrap.sh" >"$output" 2>&1
  status=$?
  set -e
  if [ "$status" -ne 2 ]; then
    cat "$output" >&2
    echo "$label: expected status 2, got $status" >&2
    exit 1
  fi
  grep -F 'bootstrap: SSH key and known-hosts inputs must be readable files' "$output"
  test ! -e "$tmpdir/state"
  test ! -e "$tmpdir/docker-called"
}

expect_unreadable unreadable-key "$tmpdir/key" "$tmpdir/known_hosts"
chmod 0644 "$tmpdir/key"
chmod 000 "$tmpdir/known_hosts"
expect_unreadable unreadable-known-hosts "$tmpdir/key" "$tmpdir/known_hosts"
