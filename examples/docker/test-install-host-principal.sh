#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TEST_ROOT=$(mktemp -d /tmp/repowolf-host-principal-test.XXXXXX)
ORIGINAL_PATH=$PATH
TOKEN=rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA

cleanup() {
  chmod -R u+rwx -- "$TEST_ROOT" 2>/dev/null || true
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

setup_case() {
  case_root=$(mktemp -d "$TEST_ROOT/case.XXXXXX")
  export case_root
  mkdir -p "$case_root/bin"
  cp "$SCRIPT_DIR/install-host-principal.sh" "$case_root/install-host-principal.sh"

  cat > "$case_root/bin/repowolf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case_root=${case_root:?}
if [ "$#" -eq 2 ] && [ "$1" = token ] && [ "$2" = generate ]; then
  : > "$case_root/token-generated"
  if [ -n "${TOKEN_OUTPUT+x}" ]; then
    printf '%s' "$TOKEN_OUTPUT"
  else
    printf '%s\n' rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
  fi
  exit 0
fi
echo "unexpected repowolf arguments: $*" >&2
exit 99
EOF

  cat > "$case_root/bin/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case_root=${case_root:?}
printf '%q ' "$@" >> "$case_root/sudo.log"
printf '\n' >> "$case_root/sudo.log"
if [ "${1:-}" = -v ]; then
  exit 0
fi
case "${1:-}" in
  test|mktemp|rm)
    command "$@"
    ;;
  install)
    args=()
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -o)
          args+=("$1" "$(id -un)")
          shift 2
          ;;
        -g)
          args+=("$1" "$(id -gn)")
          shift 2
          ;;
        *)
          args+=("$1")
          shift
          ;;
      esac
    done
    command "${args[@]}"
    ;;
  ln)
    if [ "${FAIL_ENV_PUBLISH:-}" = 1 ] && [[ ${*: -1} = */example-agent.env ]]; then
      exit 1
    fi
    command "$@"
    ;;
  *)
    echo "unexpected sudo arguments: $*" >&2
    exit 99
    ;;
esac
EOF
  chmod 0755 "$case_root/install-host-principal.sh" "$case_root/bin"/*
}

run_principal() {
  env PATH="$case_root/bin:$ORIGINAL_PATH" \
    REPOWOLF_BROKER_USER="$(id -un)" \
    REPOWOLF_BROKER_GROUP="$(id -gn)" \
    REPOWOLF_STATE_DIR="$case_root/var/lib/repowolf" \
    REPOWOLF_RUNTIME_DIR="$case_root/run/repowolf" \
    "$@" "$case_root/install-host-principal.sh"
}

expect_status() {
  expected=$1
  label=$2
  shift 2
  set +e
  "$@" >"$case_root/$label.out" 2>&1
  actual=$?
  set -e
  if [ "$actual" -ne "$expected" ]; then
    cat "$case_root/$label.out" >&2
    echo "$label: expected $expected, got $actual" >&2
    exit 1
  fi
}

setup_case
mkdir -p "$case_root/run/repowolf"
printf 'GH_TOKEN=provider-marker\n' > "$case_root/run/repowolf/service.env"
service_before=$(sha256sum "$case_root/run/repowolf/service.env")
run_principal >"$case_root/success.out" 2>&1
test "$(cat "$case_root/var/lib/repowolf/token")" = \
  'rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
test "$(cat "$case_root/run/repowolf/example-agent.env")" = \
  'REPOWOLF_TOKEN_EXAMPLE_AGENT=rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
test "$(stat -c %a "$case_root/var/lib/repowolf/token")" = 600
test "$(stat -c %a "$case_root/run/repowolf/example-agent.env")" = 600
test "$service_before" = "$(sha256sum "$case_root/run/repowolf/service.env")"
if grep -F 'rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA' \
  "$case_root/success.out"; then
  echo 'principal token leaked to output' >&2
  exit 1
fi

setup_case
expect_status 2 relative-state run_principal REPOWOLF_STATE_DIR=var/lib/repowolf
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 relative-runtime run_principal REPOWOLF_RUNTIME_DIR=run/repowolf
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 missing-broker-user run_principal REPOWOLF_BROKER_USER=missing-repowolf-user
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 missing-broker-group run_principal REPOWOLF_BROKER_GROUP=missing-repowolf-group
test ! -s "$case_root/sudo.log"

for destination in token example-agent.env token-link env-link; do
  setup_case
  mkdir -p "$case_root/var/lib/repowolf" "$case_root/run/repowolf"
  case "$destination" in
    token) touch "$case_root/var/lib/repowolf/token" ;;
    example-agent.env) touch "$case_root/run/repowolf/example-agent.env" ;;
    token-link) ln -s missing "$case_root/var/lib/repowolf/token" ;;
    env-link) ln -s missing "$case_root/run/repowolf/example-agent.env" ;;
  esac
  expect_status 1 "existing-$destination" run_principal
  test ! -e "$case_root/token-generated"
done

setup_case
expect_status 1 malformed-token run_principal TOKEN_OUTPUT=not-a-token
test -e "$case_root/token-generated"
test ! -e "$case_root/var/lib/repowolf/token"
test ! -e "$case_root/run/repowolf/example-agent.env"
if grep -Eq '^install([[:space:]]|$)' "$case_root/sudo.log"; then
  echo 'malformed token reached privileged writes' >&2
  exit 1
fi

setup_case
expect_status 1 multiline-token run_principal $'TOKEN_OUTPUT=rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\nextra\n'
test ! -e "$case_root/var/lib/repowolf/token"
test ! -e "$case_root/run/repowolf/example-agent.env"
if grep -Eq '^install([[:space:]]|$)' "$case_root/sudo.log"; then
  echo 'multiline token reached privileged writes' >&2
  exit 1
fi

setup_case
mkdir -p "$case_root/var/lib/repowolf" "$case_root/run/repowolf"
touch "$case_root/var/lib/repowolf/.state-marker" "$case_root/run/repowolf/.runtime-marker"
printf 'GH_TOKEN=provider-marker\n' > "$case_root/run/repowolf/service.env"
service_before=$(sha256sum "$case_root/run/repowolf/service.env")
expect_status 1 publication-cleanup run_principal FAIL_ENV_PUBLISH=1
test ! -e "$case_root/var/lib/repowolf/token"
test ! -e "$case_root/run/repowolf/example-agent.env"
test -e "$case_root/var/lib/repowolf/.state-marker"
test -e "$case_root/run/repowolf/.runtime-marker"
test "$service_before" = "$(sha256sum "$case_root/run/repowolf/service.env")"
