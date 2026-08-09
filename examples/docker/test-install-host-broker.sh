#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TEST_ROOT=$(mktemp -d /tmp/repowolf-host-broker-test.XXXXXX)
ORIGINAL_PATH=$PATH
ORIGINAL_DOCKER=$(command -v docker || true)

cleanup() {
  chmod -R u+rwx -- "$TEST_ROOT" 2>/dev/null || true
  rm -rf -- "$TEST_ROOT"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

setup_case() {
  case_root=$(mktemp -d "$TEST_ROOT/case.XXXXXX")
  export case_root ORIGINAL_DOCKER
  mkdir -p "$case_root/bin" "$case_root/config"
  cp "$SCRIPT_DIR/install-host-broker.sh" "$case_root/install-host-broker.sh"
  cp "$SCRIPT_DIR/config/repowolf-host.yaml" "$case_root/config/repowolf-host.yaml"

  cat > "$case_root/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -eq 5 ] && [ "$1" = network ] && [ "$2" = inspect ] && \
   [ "$3" = --format ] && [ "$4" = '{{(index .IPAM.Config 0).Gateway}}' ] && \
   [ "$5" = bridge ]; then
  printf '%s\n' 172.18.0.1
  exit 0
fi
echo "unexpected docker arguments: $*" >&2
exit 99
EOF

  cat > "$case_root/bin/repowolf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case_root=${case_root:?}
case "$1 ${2:-}" in
  'cert init')
    output=
    shift 2
    while [ "$#" -gt 0 ]; do
      if [ "$1" = --output ]; then
        output=$2
        shift 2
      else
        shift
      fi
    done
    [ -n "$output" ] || exit 99
    : > "$case_root/repowolf-cert-init-called"
    mkdir -p "$output"
    touch "$output/ca.crt" "$output/ca.key" "$output/tls.crt" "$output/tls.key"
    ;;
  'config validate')
    config=
    shift 2
    while [ "$#" -gt 0 ]; do
      if [ "$1" = --config ]; then
        config=$2
        shift 2
      else
        shift
      fi
    done
    [ -n "$config" ] || exit 99
    printf 'config validate %q\n' "$config" >> "$case_root/repowolf.log"
    if grep -q '__REPOWOLF_' "$config"; then
      exit 99
    fi
    if [ -n "${REPOWOLF_TEST_VALIDATE_IMAGE:-}" ]; then
      [ -n "${ORIGINAL_DOCKER:-}" ] || exit 99
      "$ORIGINAL_DOCKER" run --rm --user "$(id -u):$(id -g)" \
        -v "$config:/config.yaml:ro" "$REPOWOLF_TEST_VALIDATE_IMAGE" \
        config validate --config /config.yaml
    fi
    ;;
  *)
    echo "unexpected repowolf arguments: $*" >&2
    exit 99
    ;;
esac
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
if [ "${1:-}" = -u ]; then
  shift 2
fi
if [ "${FAIL_INSTALLED_VALIDATE:-}" = 1 ] && [ "${1:-}" = "$case_root/bin/repowolf" ] && \
   [ "${2:-}" = config ] && [ "${3:-}" = validate ] && [[ ${*: -1} = */repowolf.yaml ]]; then
  exit 1
fi
case "${1:-}" in
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
  "$case_root/bin/repowolf"|test|mktemp|ln|chmod|rm)
    command "$@"
    ;;
  chown)
    exit 0
    ;;
  *)
    echo "unexpected sudo arguments: $*" >&2
    exit 99
    ;;
esac
EOF

  cat > "$case_root/bin/gh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  cat > "$case_root/bin/ssh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod 0755 "$case_root/install-host-broker.sh" "$case_root/bin"/*
}

run_installer() {
  env PATH="$case_root/bin:$ORIGINAL_PATH" \
    REPOWOLF_REPO=rochecompaan/repowolf \
    REPOWOLF_BROKER_USER="$(id -un)" \
    REPOWOLF_BROKER_GROUP="$(id -gn)" \
    REPOWOLF_CONFIG_DIR="$case_root/etc/repowolf" \
    REPOWOLF_STATE_DIR="$case_root/var/lib/repowolf" \
    REPOWOLF_RUNTIME_DIR="$case_root/run/repowolf" \
    "$@" "$case_root/install-host-broker.sh"
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
run_installer
grep -F "listen: '172.18.0.1:9443'" "$case_root/etc/repowolf/repowolf.yaml"
grep -F "owner: 'rochecompaan'" "$case_root/etc/repowolf/repowolf.yaml"
grep -F "name: 'repowolf'" "$case_root/etc/repowolf/repowolf.yaml"
test "$(stat -c %a "$case_root/etc/repowolf/repowolf.yaml")" = 640
test "$(stat -c %a "$case_root/var/lib/repowolf/tls/tls.key")" = 640
test "$(stat -c %a "$case_root/var/lib/repowolf/tls/ca.key")" = 600
test "$(grep -c 'config validate' "$case_root/repowolf.log")" -eq 2
grep -F 'install -o root' "$case_root/sudo.log"
grep -F 'chown root:' "$case_root/sudo.log"

setup_case
expect_status 2 invalid-repository run_installer REPOWOLF_REPO=owner/name/extra
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 wildcard-listen run_installer REPOWOLF_LISTEN=0.0.0.0:9443
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 relative-gh run_installer REPOWOLF_GH_PATH=bin/gh
test ! -s "$case_root/sudo.log"

setup_case
expect_status 2 relative-config run_installer REPOWOLF_CONFIG_DIR=etc/repowolf
test ! -s "$case_root/sudo.log"

setup_case
mkdir -p "$case_root/var/lib/repowolf/tls"
expect_status 1 existing-tls run_installer
test ! -e "$case_root/repowolf-cert-init-called"

setup_case
rm -rf "$case_root/var/lib/repowolf/tls"
mkdir -p "$case_root/var/lib/repowolf" "$case_root/etc/repowolf"
touch "$case_root/var/lib/repowolf/.preexisting" \
  "$case_root/etc/repowolf/.preexisting"
expect_status 1 cleanup run_installer FAIL_INSTALLED_VALIDATE=1
test ! -e "$case_root/var/lib/repowolf/tls"
test ! -e "$case_root/etc/repowolf/repowolf.yaml"
test -e "$case_root/var/lib/repowolf/.preexisting"
test -e "$case_root/etc/repowolf/.preexisting"

setup_case
quoted_gh="$case_root/bin/gh'quoted"
printf '#!/usr/bin/env bash\nexit 0\n' > "$quoted_gh"
chmod 0755 "$quoted_gh"
run_installer REPOWOLF_GH_PATH="$quoted_gh"
grep -F "gh: '$case_root/bin/gh''quoted'" "$case_root/etc/repowolf/repowolf.yaml"
