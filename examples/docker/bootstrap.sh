#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
STATE_DIR="$SCRIPT_DIR/state"
ENV_FILE="$SCRIPT_DIR/.env"
REPOWOLF_IMAGE=${REPOWOLF_IMAGE:-ghcr.io/rochecompaan/repowolf:v0.1.0}
REPOWOLF_REPO=${REPOWOLF_REPO:?set REPOWOLF_REPO to one owner/name repository}
REPOWOLF_SSH_KEY=${REPOWOLF_SSH_KEY:-}
REPOWOLF_KNOWN_HOSTS=${REPOWOLF_KNOWN_HOSTS:-}

if [[ $REPOWOLF_REPO =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?)/([A-Za-z0-9][A-Za-z0-9._-]{0,99})$ ]]; then
  owner=${BASH_REMATCH[1]}
  name=${BASH_REMATCH[3]}
else
  echo "bootstrap: REPOWOLF_REPO must match owner/name with alphanumeric first characters" >&2
  exit 2
fi
if { [ -n "$REPOWOLF_SSH_KEY" ] && [ -z "$REPOWOLF_KNOWN_HOSTS" ]; } || \
   { [ -z "$REPOWOLF_SSH_KEY" ] && [ -n "$REPOWOLF_KNOWN_HOSTS" ]; }; then
  echo "bootstrap: set both REPOWOLF_SSH_KEY and REPOWOLF_KNOWN_HOSTS, or neither" >&2
  exit 2
fi
if [ -n "$REPOWOLF_SSH_KEY" ] && { [ ! -f "$REPOWOLF_SSH_KEY" ] || [ ! -r "$REPOWOLF_SSH_KEY" ] || [ ! -f "$REPOWOLF_KNOWN_HOSTS" ] || [ ! -r "$REPOWOLF_KNOWN_HOSTS" ]; }; then
  echo "bootstrap: SSH key and known-hosts inputs must be readable files" >&2
  exit 2
fi
if { [ -e "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; } && \
   { [ ! -f "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; }; then
  echo "bootstrap: $ENV_FILE must be a regular file when it exists" >&2
  exit 1
fi

created_state=0
env_tmp=
cleanup_failed_bootstrap() {
  status=$?
  trap - EXIT INT TERM
  if [ -n "$env_tmp" ]; then
    rm -f -- "$env_tmp"
  fi
  if [ "$status" -ne 0 ] && [ "$created_state" -eq 1 ]; then
    rm -rf "$STATE_DIR" # only partial state created by this invocation
  fi
  exit "$status"
}
trap cleanup_failed_bootstrap EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if ! mkdir "$STATE_DIR"; then
  if [ -e "$STATE_DIR" ]; then
    echo "bootstrap: $STATE_DIR already exists; back it up before resetting" >&2
  else
    echo "bootstrap: could not create $STATE_DIR" >&2
  fi
  exit 1
fi
created_state=1
chmod 0700 "$STATE_DIR"

rw_as_host() {
  docker run --rm --user "$(id -u):$(id -g)" \
    -v "$STATE_DIR:/state" "$REPOWOLF_IMAGE" "$@"
}

rw_as_host cert init --output /state/tls --dns repowolf --dns localhost --ip 127.0.0.1 >/dev/null
(umask 077 && rw_as_host token generate > "$STATE_DIR/token")
mkdir -p "$STATE_DIR/ssh"

while IFS= read -r line || [ -n "$line" ]; do
  line=${line//__OWNER__/$owner}
  line=${line//__NAME__/$name}
  printf '%s\n' "$line"
done < "$SCRIPT_DIR/config/repowolf.yaml" > "$STATE_DIR/config.yaml"

if [ -n "$REPOWOLF_SSH_KEY" ]; then
  cat "$REPOWOLF_SSH_KEY" > "$STATE_DIR/ssh/id_ed25519"
  cat "$REPOWOLF_KNOWN_HOSTS" > "$STATE_DIR/ssh/known_hosts"
  cat > "$STATE_DIR/ssh/config" <<'EOF'
Host github.com
  IdentityAgent none
  IdentitiesOnly yes
  IdentityFile /tmp/.ssh/id_ed25519
  UserKnownHostsFile /tmp/.ssh/known_hosts
EOF
fi

# Narrow permission handoff to broker/sandbox GID 65532. ca.key stays
# host-owned 0600 and is never mounted into either container.
docker run --rm --user 0:0 \
  -e HOST_UID="$(id -u)" \
  -v "$STATE_DIR/config.yaml:/config.yaml" \
  -v "$STATE_DIR/tls:/tls" \
  -v "$STATE_DIR/ssh:/ssh" \
  alpine:3 sh -eu -c '
    chown "$HOST_UID:65532" /config.yaml /tls /tls/ca.crt /tls/tls.crt /tls/tls.key /ssh
    chmod 0640 /config.yaml
    chmod 0750 /tls /ssh
    chmod 0640 /tls/ca.crt /tls/tls.crt /tls/tls.key
    chmod 0600 /tls/ca.key
    if [ -f /ssh/id_ed25519 ]; then
      chown 65532:65532 /ssh/id_ed25519
      chmod 0600 /ssh/id_ed25519
      chown "$HOST_UID:65532" /ssh/known_hosts
      chmod 0640 /ssh/known_hosts
      chown 0:65532 /ssh/config
      chmod 0640 /ssh/config
    fi
  '

docker run --rm --user 65532:65532 \
  -v "$STATE_DIR/config.yaml:/config.yaml:ro" \
  "$REPOWOLF_IMAGE" config validate --config /config.yaml >/dev/null

if [ -n "$REPOWOLF_SSH_KEY" ]; then
  ssh_effective=$(docker run --rm \
    -v "$STATE_DIR/ssh:/tmp/.ssh:ro" \
    --entrypoint ssh "$REPOWOLF_IMAGE" -G github.com)
  grep -Eq '^identityagent[[:space:]]+none$' <<< "$ssh_effective"
  grep -Eq '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' <<< "$ssh_effective"
  grep -Eq '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' <<< "$ssh_effective"
fi

env_tmp=$(mktemp "${ENV_FILE}.tmp.XXXXXX")
chmod 0600 "$env_tmp"
found_token=0
if [ -f "$ENV_FILE" ]; then
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      REPOWOLF_TOKEN_AGENT=*)
        printf 'REPOWOLF_TOKEN_AGENT=%s\n' "$(cat "$STATE_DIR/token")" >> "$env_tmp"
        found_token=1
        ;;
      *) printf '%s\n' "$line" >> "$env_tmp" ;;
    esac
  done < "$ENV_FILE"
fi
if [ "$found_token" -eq 0 ]; then
  printf 'REPOWOLF_TOKEN_AGENT=%s\n' "$(cat "$STATE_DIR/token")" >> "$env_tmp"
fi
if { [ -e "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; } && \
   { [ ! -f "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; }; then
  echo "bootstrap: $ENV_FILE must be a regular file when it exists" >&2
  exit 1
fi
mv "$env_tmp" "$ENV_FILE"
if { [ ! -f "$ENV_FILE" ] || [ -L "$ENV_FILE" ]; }; then
  if [ -d "$ENV_FILE" ]; then
    rm -f -- "$ENV_FILE/${env_tmp##*/}"
  fi
  echo "bootstrap: $ENV_FILE must be a regular file when it exists" >&2
  exit 1
fi
env_tmp=

trap - EXIT INT TERM
cat <<EOF
bootstrap: state written to $STATE_DIR (keep token/private keys private)
next:
  1. set GH_TOKEN in $ENV_FILE (see .env.example)
  2. docker compose -f $SCRIPT_DIR/compose.yaml up -d repowolf
  3. $SCRIPT_DIR/wait-for-broker.sh 127.0.0.1 8443 30
  4. docker compose -f $SCRIPT_DIR/compose.yaml run --rm sandbox gh repo view --repo $REPOWOLF_REPO
EOF
