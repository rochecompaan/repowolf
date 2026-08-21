#!/usr/bin/env bash
set -euo pipefail
umask 077

STATE_DIR_INPUT=${REPOWOLF_STATE_DIR:-/var/lib/repowolf}
RUNTIME_DIR_INPUT=${REPOWOLF_RUNTIME_DIR:-/run/repowolf}
BROKER_USER=${REPOWOLF_BROKER_USER:-repowolf}
BROKER_GROUP=${REPOWOLF_BROKER_GROUP:-repowolf}
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

require_absolute_path() {
  label=$1
  value=$2
  reject_control "$label" "$value"
  case $value in
    /*) ;;
    *) fail_usage "$label must be an absolute path" ;;
  esac
}

canonicalize_directory() {
  label=$1
  value=$2
  require_absolute_path "$label" "$value"
  local base=/ candidate component canonical last_index resolved_base
  local -a suffix=() components=()
  IFS=/ read -r -a components <<< "${value#/}" || true
  for component in "${components[@]}"; do
    case $component in
      ''|.) ;;
      ..)
        if [ "${#suffix[@]}" -gt 0 ]; then
          last_index=$((${#suffix[@]} - 1))
          unset "suffix[$last_index]"
        elif [ "$base" = / ]; then
          base=/
        else
          base=$(cd -P -- "$base/.." && pwd -P) || \
            fail_usage "$label must resolve to a directory"
        fi
        ;;
      *)
        if [ "${#suffix[@]}" -eq 0 ]; then
          if [ "$base" = / ]; then
            candidate=/$component
          else
            candidate=$base/$component
          fi
          if [ -e "$candidate" ] || [ -L "$candidate" ]; then
            if resolved_base=$(cd -P -- "$candidate" 2>/dev/null && pwd -P); then
              base=$resolved_base
            elif [ "${CANONICALIZE_ALLOW_UNRESOLVED:-}" = 1 ]; then
              suffix+=("$component")
            else
              fail_usage "$label must resolve to a directory"
            fi
          else
            suffix+=("$component")
          fi
        else
          suffix+=("$component")
        fi
        ;;
    esac
  done
  canonical=$base
  for component in "${suffix[@]}"; do
    if [ "$canonical" = / ]; then
      canonical=/$component
    else
      canonical=$canonical/$component
    fi
  done
  [ "$canonical" != / ] || fail_usage "$label must not resolve to /"
  printf '%s\n' "$canonical"
}

resolve_directory_privileged() {
  local label=$1 value=$2 resolver resolved
  resolver=$(declare -f fail_usage reject_control require_absolute_path canonicalize_directory)
  resolved=$("$SUDO_BIN" "$BASH" -c "$resolver
CANONICALIZE_ALLOW_UNRESOLVED=
canonicalize_directory \"\$1\" \"\$2\"" bash "$label" "$value") || \
    fail_usage "$label must resolve to a directory"
  printf '%s\n' "$resolved"
}

resolve_directories() {
  local resolved
  resolved=$(resolve_directory_privileged REPOWOLF_STATE_DIR "$STATE_DIR_INPUT")
  [ "$resolved" = "$EXPECTED_STATE_DIR" ] || \
    fail_usage 'configured directory changed during sudo validation'
  STATE_DIR=$resolved
  resolved=$(resolve_directory_privileged REPOWOLF_RUNTIME_DIR "$RUNTIME_DIR_INPUT")
  [ "$resolved" = "$EXPECTED_RUNTIME_DIR" ] || \
    fail_usage 'configured directory changed during sudo validation'
  RUNTIME_DIR=$resolved
  TOKEN_FILE=$STATE_DIR/token
  ENV_FILE=$RUNTIME_DIR/example-agent.env
}

directories_match() {
  local current
  current=$(resolve_directory_privileged REPOWOLF_STATE_DIR "$STATE_DIR_INPUT") || return 1
  [ "$current" = "$STATE_DIR" ] || return 1
  current=$(resolve_directory_privileged REPOWOLF_RUNTIME_DIR "$RUNTIME_DIR_INPUT") || return 1
  [ "$current" = "$RUNTIME_DIR" ] || return 1
}

recheck_directories() {
  directories_match || fail_usage 'configured directory changed during installation'
}

sudo_mutate() {
  recheck_directories
  "$SUDO_BIN" "$@"
}

cleanup_mutate() {
  if ! directories_match; then
    echo 'install-host-principal: configured directory changed; refusing cleanup' >&2
    return 1
  fi
  "$SUDO_BIN" "$@"
}

require_executable() {
  label=$1
  value=$2
  require_absolute_path "$label" "$value"
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

EXPECTED_STATE_DIR=$(CANONICALIZE_ALLOW_UNRESOLVED=1 canonicalize_directory REPOWOLF_STATE_DIR "$STATE_DIR_INPUT")
EXPECTED_RUNTIME_DIR=$(CANONICALIZE_ALLOW_UNRESOLVED=1 canonicalize_directory REPOWOLF_RUNTIME_DIR "$RUNTIME_DIR_INPUT")
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
    [ -z "$privileged_temp" ] || cleanup_mutate rm -f -- "$privileged_temp" || true
    [ "$token_owned" -eq 0 ] || cleanup_mutate rm -f -- "$TOKEN_FILE" || true
    [ "$env_owned" -eq 0 ] || cleanup_mutate rm -f -- "$ENV_FILE" || true
    [ "$runtime_dir_created" -eq 0 ] || cleanup_mutate rm -d -- "$RUNTIME_DIR" || true
    [ "$state_dir_created" -eq 0 ] || cleanup_mutate rm -d -- "$STATE_DIR" || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

"$SUDO_BIN" -v
resolve_directories
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
  sudo_mutate install -o root -g root -m 0700 -d "$STATE_DIR"
  state_dir_created=1
fi
if ! path_exists "$RUNTIME_DIR"; then
  sudo_mutate install -o root -g root -m 0700 -d "$RUNTIME_DIR"
  runtime_dir_created=1
fi

publish_root_file() {
  source_path=$1
  destination=$2
  destination_dir=${destination%/*}
  privileged_temp=$(sudo_mutate mktemp "$destination_dir/.repowolf-install.XXXXXX")
  sudo_mutate install -o root -g root -m 0600 "$source_path" "$privileged_temp"
  sudo_mutate ln "$privileged_temp" "$destination"
  case $destination in
    "$TOKEN_FILE") token_owned=1 ;;
    "$ENV_FILE") env_owned=1 ;;
  esac
  sudo_mutate rm -f -- "$privileged_temp"
  privileged_temp=
}

publish_root_file "$token_temp" "$TOKEN_FILE"
publish_root_file "$env_temp" "$ENV_FILE"

trap - EXIT INT TERM
rm -f -- "$token_temp" "$env_temp"
token_temp=
env_temp=
printf 'install-host-principal: installed %s and %s\n' "$TOKEN_FILE" "$ENV_FILE"
printf 'install-host-principal: load %s/service.env and %s before starting or restarting the broker\n' \
  "$RUNTIME_DIR" "$ENV_FILE"
