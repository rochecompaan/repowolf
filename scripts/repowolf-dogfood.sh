#!/usr/bin/env bash
# repowolf-dogfood manages this repository's local RepoWolf broker.
# All state lives under $DEVENV_ROOT/.devenv/repowolf and is never committed.
set -euo pipefail

LISTEN_HOST=127.0.0.1
LISTEN_PORT=9443

state_dir() {
  printf '%s/.devenv/repowolf' "${DEVENV_ROOT:?DEVENV_ROOT must be set; run inside devenv}"
}

real_gh() {
  printf '%s' "${REPOWOLF_DOGFOOD_REAL_GH:?REPOWOLF_DOGFOOD_REAL_GH must be set by devenv.nix}"
}

real_ssh() {
  printf '%s' "${REPOWOLF_DOGFOOD_REAL_SSH:?REPOWOLF_DOGFOOD_REAL_SSH must be set by devenv.nix}"
}

real_gh_token() {
  "$(real_gh)" auth token 2>/dev/null || true
}

render_config() {
  local state=$1
  cat >"$state/config.yaml" <<EOF
apiVersion: repowolf.dev/v1alpha1
listen: ${LISTEN_HOST}:${LISTEN_PORT}
tls:
  certificate: $state/tls/tls.crt
  privateKey: $state/tls/tls.key
tools:
  gh: $(real_gh)
  ssh: $(real_ssh)
providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    sshPort: 22
repositories:
  repowolf:
    provider: github-public
    owner: rochecompaan
    name: repowolf
    git:
      denyRefs:
        - refs/heads/main
      denyDeletes: true
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_AGENT
    grants:
      - repository: repowolf
        capabilities:
          - repository:read
          - issues:read
          - issues:write
          - pull_requests:read
          - pull_requests:write
          - actions:read
          - statuses:read
          - git:read
          - git:write
limits:
  maxConcurrentRequests: 16
  maxConcurrentRequestsPerPrincipal: 8
  maxMessageBytes: 1048576
  maxStreamChunkBytes: 65536
  maxPushPrefixBytes: 1048576
  maxGitBytesPerDirection: 1073741824
  initialStreamTimeout: 5s
  operationTimeout: 10m
  idleStreamTimeout: 2m
EOF
}

bootstrap() {
  local state
  state=$(state_dir)
  install -d -m 0700 "$state"
  if [ ! -f "$state/tls/ca.crt" ]; then
    rm -rf "$state/tls"
    repowolf cert init --output "$state/tls" --dns localhost --ip 127.0.0.1
    chmod 0700 "$state/tls"
  fi
  if [ ! -f "$state/token" ]; then
    (umask 077 && repowolf token generate >"$state/token")
  fi
  render_config "$state"
  repowolf config validate --config "$state/config.yaml" >/dev/null
  if [ -z "$(real_gh_token)" ]; then
    echo "repowolf-dogfood: $(real_gh) auth token returned empty; run 'gh auth login'" >&2
    return 1
  fi
  echo "repowolf-dogfood: bootstrap complete"
}

status() {
  local state
  state=$(state_dir)
  [ -f "$state/tls/ca.crt" ] &&
    [ -f "$state/tls/tls.crt" ] &&
    [ -f "$state/tls/tls.key" ] &&
    [ -f "$state/token" ] &&
    [ "$(stat -c %a "$state/token")" = 600 ] &&
    [ -f "$state/config.yaml" ] &&
    repowolf config validate --config "$state/config.yaml" >/dev/null 2>&1
}

serve() {
  local state
  state=$(state_dir)
  GH_TOKEN=$(real_gh_token)
  if [ -z "$GH_TOKEN" ]; then
    echo "repowolf-dogfood: $(real_gh) auth token returned empty; cannot start broker" >&2
    return 1
  fi
  export GH_TOKEN
  REPOWOLF_TOKEN_AGENT=$(cat "$state/token")
  export REPOWOLF_TOKEN_AGENT
  exec repowolf serve --config "$state/config.yaml"
}

reset() {
  local state
  state=$(state_dir)
  devenv processes stop repowolf >/dev/null 2>&1 || true
  rm -rf "$state"
  echo "repowolf-dogfood: removed $state; next 'devenv shell' re-bootstraps"
}

probe() {
  (exec 3<>"/dev/tcp/${LISTEN_HOST}/${LISTEN_PORT}") 2>/dev/null
}

case "${1:-}" in
  bootstrap) bootstrap ;;
  status) status ;;
  serve) serve ;;
  reset) reset ;;
  probe) probe ;;
  *)
    echo "usage: repowolf-dogfood <bootstrap|status|serve|reset|probe>" >&2
    exit 2
    ;;
esac
