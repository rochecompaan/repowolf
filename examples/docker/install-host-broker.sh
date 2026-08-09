#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TEMPLATE=$SCRIPT_DIR/config/repowolf-host.yaml
REPOSITORY=${REPOWOLF_REPO:-rochecompaan/repowolf}
CONFIG_DIR=${REPOWOLF_CONFIG_DIR:-/etc/repowolf}
STATE_DIR=${REPOWOLF_STATE_DIR:-/var/lib/repowolf}
RUNTIME_DIR=${REPOWOLF_RUNTIME_DIR:-/run/repowolf}
BROKER_USER=${REPOWOLF_BROKER_USER:-repowolf}
BROKER_GROUP=${REPOWOLF_BROKER_GROUP:-repowolf}
CONFIG_FILE=$CONFIG_DIR/repowolf.yaml
TLS_DIR=$STATE_DIR/tls

fail_usage() {
  echo "install-host-broker: $*" >&2
  exit 2
}

reject_control() {
  label=$1
  value=$2
  if [[ $value =~ [[:cntrl:]] ]]; then
    fail_usage "$label contains a control character"
  fi
}

require_absolute() {
  label=$1
  value=$2
  reject_control "$label" "$value"
  case $value in
    /*) ;;
    *) fail_usage "$label must be an absolute path" ;;
  esac
  [ "$value" != / ] || fail_usage "$label must not be /"
}

yaml_quote() {
  value=${1//\'/\'\'}
  printf "'%s'" "$value"
}

require_executable() {
  label=$1
  value=$2
  require_absolute "$label" "$value"
  [ -x "$value" ] || fail_usage "$label must be an executable path"
}

path_exists() {
  local exists=1
  if "$SUDO_BIN" test -e "$1"; then
    exists=0
  fi
  if "$SUDO_BIN" test -L "$1"; then
    exists=0
  fi
  return "$exists"
}

render_policy() {
  while IFS= read -r line || [[ -n $line ]]; do
    case $line in
      'listen: __REPOWOLF_LISTEN__')
        printf 'listen: %s\n' "$(yaml_quote "$LISTEN")"
        ;;
      '  certificate: __REPOWOLF_TLS_CERTIFICATE__')
        printf '  certificate: %s\n' "$(yaml_quote "$TLS_DIR/tls.crt")"
        ;;
      '  privateKey: __REPOWOLF_TLS_PRIVATE_KEY__')
        printf '  privateKey: %s\n' "$(yaml_quote "$TLS_DIR/tls.key")"
        ;;
      '  gh: __REPOWOLF_GH_PATH__')
        printf '  gh: %s\n' "$(yaml_quote "$GH_PATH")"
        ;;
      '  ssh: __REPOWOLF_SSH_PATH__')
        printf '  ssh: %s\n' "$(yaml_quote "$SSH_PATH")"
        ;;
      '    owner: __REPOWOLF_OWNER__')
        printf '    owner: %s\n' "$(yaml_quote "$OWNER")"
        ;;
      '    name: __REPOWOLF_NAME__')
        printf '    name: %s\n' "$(yaml_quote "$NAME")"
        ;;
      *) printf '%s\n' "$line" ;;
    esac
  done < "$TEMPLATE"
}

if [[ $REPOSITORY =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?)/([A-Za-z0-9][A-Za-z0-9._-]{0,99})$ ]]; then
  OWNER=${BASH_REMATCH[1]}
  NAME=${BASH_REMATCH[3]}
else
  fail_usage "REPOWOLF_REPO must match owner/name with alphanumeric first characters"
fi

require_absolute REPOWOLF_CONFIG_DIR "$CONFIG_DIR"
require_absolute REPOWOLF_STATE_DIR "$STATE_DIR"
require_absolute REPOWOLF_RUNTIME_DIR "$RUNTIME_DIR"
reject_control REPOWOLF_BROKER_USER "$BROKER_USER"
reject_control REPOWOLF_BROKER_GROUP "$BROKER_GROUP"
[ -n "$BROKER_USER" ] || fail_usage "REPOWOLF_BROKER_USER must not be empty"
[ -n "$BROKER_GROUP" ] || fail_usage "REPOWOLF_BROKER_GROUP must not be empty"

if [ -n "${REPOWOLF_LISTEN:-}" ]; then
  LISTEN=$REPOWOLF_LISTEN
else
  gateway=$(docker network inspect --format '{{(index .IPAM.Config 0).Gateway}}' bridge) || \
    fail_usage "could not resolve the Docker bridge gateway"
  if ! [[ $gateway =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    fail_usage "Docker bridge gateway must be an IPv4 address"
  fi
  IFS=. read -r -a octets <<< "$gateway"
  for octet in "${octets[@]}"; do
    ((10#$octet <= 255)) || fail_usage "Docker bridge gateway must be an IPv4 address"
  done
  LISTEN=$gateway:9443
fi
reject_control REPOWOLF_LISTEN "$LISTEN"
if [[ $LISTEN =~ ^([A-Za-z0-9.-]+|\[[0-9A-Fa-f:]+\]):([1-9][0-9]{0,4})$ ]]; then
  listen_host=${BASH_REMATCH[1]}
  listen_port=$((10#${BASH_REMATCH[2]}))
else
  fail_usage "REPOWOLF_LISTEN must be host:port or [ipv6]:port"
fi
((listen_port >= 1 && listen_port <= 65535)) || \
  fail_usage "REPOWOLF_LISTEN port must be between 1 and 65535"
case $listen_host in
  0.0.0.0|::|'[::]') fail_usage "REPOWOLF_LISTEN must not use a wildcard host" ;;
esac

REPOWOLF_BIN=$(command -v repowolf || true)
GH_PATH=${REPOWOLF_GH_PATH:-$(command -v gh || true)}
SSH_PATH=${REPOWOLF_SSH_PATH:-$(command -v ssh || true)}
SUDO_BIN=$(command -v sudo || true)
[ -n "$REPOWOLF_BIN" ] || fail_usage "repowolf was not found"
[ -n "$GH_PATH" ] || fail_usage "gh was not found"
[ -n "$SSH_PATH" ] || fail_usage "ssh was not found"
[ -n "$SUDO_BIN" ] || fail_usage "sudo was not found"
require_executable repowolf "$REPOWOLF_BIN"
require_executable REPOWOLF_GH_PATH "$GH_PATH"
require_executable REPOWOLF_SSH_PATH "$SSH_PATH"

id -u "$BROKER_USER" >/dev/null || fail_usage "REPOWOLF_BROKER_USER does not exist"
getent group "$BROKER_GROUP" >/dev/null || fail_usage "REPOWOLF_BROKER_GROUP does not exist"
group_member=0
for group in $(id -Gn "$BROKER_USER"); do
  if [ "$group" = "$BROKER_GROUP" ]; then
    group_member=1
    break
  fi
done
[ "$group_member" -eq 1 ] || \
  fail_usage "REPOWOLF_BROKER_USER is not a member of REPOWOLF_BROKER_GROUP"

rendered=
privileged_temp=
config_owned=0
tls_owned=0
config_dir_created=0
state_dir_created=0
cleanup() {
  status=$?
  trap - EXIT INT TERM
  [ -z "$rendered" ] || rm -f -- "$rendered"
  if [ "$status" -ne 0 ]; then
    [ -z "$privileged_temp" ] || "$SUDO_BIN" rm -f -- "$privileged_temp" || true
    [ "$config_owned" -eq 0 ] || "$SUDO_BIN" rm -f -- "$CONFIG_FILE" || true
    [ "$tls_owned" -eq 0 ] || "$SUDO_BIN" rm -rf -- "$TLS_DIR" || true
    [ "$config_dir_created" -eq 0 ] || "$SUDO_BIN" rm -d -- "$CONFIG_DIR" || true
    [ "$state_dir_created" -eq 0 ] || "$SUDO_BIN" rm -d -- "$STATE_DIR" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

rendered=$(mktemp)
render_policy > "$rendered"
if grep -q '__REPOWOLF_' "$rendered"; then
  echo "install-host-broker: template contains an unrendered placeholder" >&2
  exit 1
fi
"$REPOWOLF_BIN" config validate --config "$rendered"
"$SUDO_BIN" -v

if path_exists "$TLS_DIR"; then
  echo "install-host-broker: TLS directory already exists: $TLS_DIR" >&2
  exit 1
fi
if path_exists "$CONFIG_FILE"; then
  echo "install-host-broker: policy already exists: $CONFIG_FILE" >&2
  exit 1
fi

if ! path_exists "$CONFIG_DIR"; then
  "$SUDO_BIN" install -o root -g "$BROKER_GROUP" -m 0750 -d "$CONFIG_DIR"
  config_dir_created=1
fi
if ! path_exists "$STATE_DIR"; then
  "$SUDO_BIN" install -o root -g "$BROKER_GROUP" -m 0750 -d "$STATE_DIR"
  state_dir_created=1
fi

"$SUDO_BIN" "$REPOWOLF_BIN" cert init --output "$TLS_DIR" --dns repowolf.internal --ip 127.0.0.1 >/dev/null
tls_owned=1
"$SUDO_BIN" chown root:"$BROKER_GROUP" "$STATE_DIR" "$TLS_DIR" \
  "$TLS_DIR/tls.crt" "$TLS_DIR/tls.key"
"$SUDO_BIN" chmod 0750 "$STATE_DIR" "$TLS_DIR"
"$SUDO_BIN" chmod 0640 "$TLS_DIR/tls.crt" "$TLS_DIR/tls.key"
"$SUDO_BIN" chown root:root "$TLS_DIR/ca.crt" "$TLS_DIR/ca.key"
"$SUDO_BIN" chmod 0644 "$TLS_DIR/ca.crt"
"$SUDO_BIN" chmod 0600 "$TLS_DIR/ca.key"

privileged_temp=$("$SUDO_BIN" mktemp "$CONFIG_DIR/.repowolf.yaml.XXXXXX")
"$SUDO_BIN" install -o root -g "$BROKER_GROUP" -m 0640 "$rendered" "$privileged_temp"
"$SUDO_BIN" ln "$privileged_temp" "$CONFIG_FILE"
config_owned=1
"$SUDO_BIN" rm -f -- "$privileged_temp"
privileged_temp=

"$SUDO_BIN" -u "$BROKER_USER" "$REPOWOLF_BIN" config validate --config "$CONFIG_FILE"

trap - EXIT INT TERM
rm -f -- "$rendered"
rendered=
printf 'install-host-broker: installed %s and %s\n' "$CONFIG_FILE" "$TLS_DIR"
printf 'install-host-broker: load %s/service.env plus the principal environment before restarting the broker\n' "$RUNTIME_DIR"
