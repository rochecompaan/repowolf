#!/usr/bin/env bash
set -euo pipefail
umask 077

STATE_DIR=${REPOWOLF_STATE_DIR:-/var/lib/repowolf}
RUNTIME_DIR=${REPOWOLF_RUNTIME_DIR:-/run/repowolf}
BROKER_USER=${REPOWOLF_BROKER_USER:-repowolf}
BROKER_GROUP=${REPOWOLF_BROKER_GROUP:-repowolf}
TOKEN_FILE=$STATE_DIR/token
ENV_FILE=$RUNTIME_DIR/example-agent.env

fail_usage() {
  echo "install-host-principal: $*" >&2
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

require_absolute REPOWOLF_STATE_DIR "$STATE_DIR"
require_absolute REPOWOLF_RUNTIME_DIR "$RUNTIME_DIR"
reject_control REPOWOLF_BROKER_USER "$BROKER_USER"
reject_control REPOWOLF_BROKER_GROUP "$BROKER_GROUP"
[ -n "$BROKER_USER" ] || fail_usage "REPOWOLF_BROKER_USER must not be empty"
[ -n "$BROKER_GROUP" ] || fail_usage "REPOWOLF_BROKER_GROUP must not be empty"

REPOWOLF_BIN=$(command -v repowolf || true)
SUDO_BIN=$(command -v sudo || true)
[ -n "$REPOWOLF_BIN" ] || fail_usage "repowolf was not found"
[ -n "$SUDO_BIN" ] || fail_usage "sudo was not found"
require_executable repowolf "$REPOWOLF_BIN"

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

token_temp=
env_temp=
privileged_temp=
token_owned=0
env_owned=0
state_dir_created=0
runtime_dir_created=0
cleanup() {
  status=$?
  trap - EXIT INT TERM
  [ -z "$token_temp" ] || rm -f -- "$token_temp"
  [ -z "$env_temp" ] || rm -f -- "$env_temp"
  if [ "$status" -ne 0 ]; then
    [ -z "$privileged_temp" ] || "$SUDO_BIN" rm -f -- "$privileged_temp" || true
    [ "$token_owned" -eq 0 ] || "$SUDO_BIN" rm -f -- "$TOKEN_FILE" || true
    [ "$env_owned" -eq 0 ] || "$SUDO_BIN" rm -f -- "$ENV_FILE" || true
    [ "$runtime_dir_created" -eq 0 ] || "$SUDO_BIN" rm -d -- "$RUNTIME_DIR" || true
    [ "$state_dir_created" -eq 0 ] || "$SUDO_BIN" rm -d -- "$STATE_DIR" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"$SUDO_BIN" -v
if path_exists "$TOKEN_FILE"; then
  echo "install-host-principal: token already exists: $TOKEN_FILE" >&2
  exit 1
fi
if path_exists "$ENV_FILE"; then
  echo "install-host-principal: principal environment already exists: $ENV_FILE" >&2
  exit 1
fi

token_temp=$(mktemp)
"$REPOWOLF_BIN" token generate > "$token_temp"
IFS= read -r token < "$token_temp"
if ! [[ $token =~ ^rw1_[A-Za-z0-9_-]{43}$ ]] || \
   [ "$(wc -l < "$token_temp")" -ne 1 ]; then
  echo 'install-host-principal: repowolf returned an invalid token' >&2
  exit 1
fi
env_temp=$(mktemp)
printf 'REPOWOLF_TOKEN_EXAMPLE_AGENT=%s\n' "$token" > "$env_temp"
unset token

if ! path_exists "$STATE_DIR"; then
  "$SUDO_BIN" install -o root -g root -m 0700 -d "$STATE_DIR"
  state_dir_created=1
fi
if ! path_exists "$RUNTIME_DIR"; then
  "$SUDO_BIN" install -o root -g root -m 0700 -d "$RUNTIME_DIR"
  runtime_dir_created=1
fi

publish_root_file() {
  source_path=$1
  destination=$2
  destination_dir=${destination%/*}
  privileged_temp=$("$SUDO_BIN" mktemp "$destination_dir/.repowolf-install.XXXXXX")
  "$SUDO_BIN" install -o root -g root -m 0600 "$source_path" "$privileged_temp"
  "$SUDO_BIN" ln "$privileged_temp" "$destination"
  case $destination in
    "$TOKEN_FILE") token_owned=1 ;;
    "$ENV_FILE") env_owned=1 ;;
  esac
  "$SUDO_BIN" rm -f -- "$privileged_temp"
  privileged_temp=
}

publish_root_file "$token_temp" "$TOKEN_FILE"
publish_root_file "$env_temp" "$ENV_FILE"

trap - EXIT INT TERM
rm -f -- "$token_temp" "$env_temp"
token_temp=
env_temp=
printf 'install-host-principal: installed %s and %s\n' "$TOKEN_FILE" "$ENV_FILE"
printf 'install-host-principal: load %s/service.env and %s before starting the example agent\n' \
  "$RUNTIME_DIR" "$ENV_FILE"
