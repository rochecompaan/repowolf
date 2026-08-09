# RepoWolf Docker Example Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Maintain a safe, copy-pasteable Docker example with one restricted sandbox image, Compose-first documentation, focused host-broker/principal installers, readiness handling, service-side SSH support, and behavioral CI coverage.

**Architecture:** The sandbox image downloads the pinned client archive, verifies one filtered checksum with Alpine BusyBox, and contains no provider tools or credentials. `bootstrap.sh` retains fixed Compose paths, while `install-host-broker.sh` renders a dedicated host-policy template and `install-host-principal.sh` installs the principal token/environment without touching provider credentials. Compose and CI assert stable broker audit events, observable Git process launch, and the host installers' filesystem behavior.

**Tech Stack:** Docker/buildx, Docker Compose v2+, Bash 3.2-compatible syntax, Nix, goreleaser, GitHub Actions, Go (temporary local HTTP/YAML validators only).

## Global constraints

- Approved/revised spec: `docs/specs/2026-08-07-docker-example-design.md`.
- No RepoWolf Go-code or config-schema changes.
- Linux is the verified target. The compose path requires Docker **and Bash**; macOS needs a Unix shell, Windows needs WSL2. Do not claim native PowerShell/cmd support or Docker-only host tooling.
- Dockerfile public args: `REPOWOLF_VERSION` (default `v0.1.0`) and `REPOWOLF_RELEASE_ROOT` (default GitHub releases/download root).
- Release artifacts are **checksum-verified**, not signed.
- Fixed Compose bootstrap paths only: `examples/docker/state` and `examples/docker/.env`; no disconnected `STATE_DIR`/`ENV_FILE` overrides.
- Host installer defaults are `/etc/repowolf`, `/var/lib/repowolf`, `/run/repowolf`, `rochecompaan/repowolf`, and the detected Docker bridge gateway on port `9443`. The approved environment overrides are `REPOWOLF_REPO`, `REPOWOLF_LISTEN`, `REPOWOLF_GH_PATH`, `REPOWOLF_SSH_PATH`, `REPOWOLF_BROKER_USER`, `REPOWOLF_BROKER_GROUP`, `REPOWOLF_CONFIG_DIR`, `REPOWOLF_STATE_DIR`, and `REPOWOLF_RUNTIME_DIR`.
- Host installers run as a normal operator and invoke `sudo` internally. They never start/restart the broker, overwrite existing TLS/config/token/environment state, print token/key contents, or remove paths not created by the current invocation.
- Git provider operations (read and write) require broker-side SSH authentication **and** verified known-hosts state. No documentation may call public Git clone credential-free.
- Client output is intentionally opaque (`gh: GitHub operation failed` for provider failures and denials). Assert broker audit events instead.
- Testing Value Gate: no YAML-content tests. Use live compose behavior, `config validate`, `bash -n`, shellcheck when installed, a direct YAML syntax parser, and exact permission/ignore checks.
- Never hide failures behind `|| echo` or a pipeline without `pipefail`. Final `go test -race ./...` runs directly.
- `.gitignore` reduces accidental commits but does not prevent `git add -f`; wording must stay precise.
- Do not publish/tag `v0.1.0` during automated execution. After merge, offer it as a separate owner-confirmed rollout action.

## Empirical facts already verified

- Alpine BusyBox reports `sha256sum: unrecognized option: ignore-missing`; use a one-line filtered checksum file and plain `sha256sum -c`.
- `cert init` creates `tls/` mode `0700`, certs `0644`, keys `0600`, owned by the invoking UID. Without a permission handoff, broker UID 65532 cannot traverse/read them.
- Supported CLI form is `gh repo view --repo owner/name`; positional `gh repo view owner/name` is rejected.
- Granted GitHub request audit: `operation: github.repository_view`, `outcome: accepted`; dummy provider token then ends as RPC `failed`/`Unavailable`.
- Denied run-list audit: RPC `outcome: denied`, `reason: PermissionDenied`, with no accepted `github.run_list` event.
- Git read audit begins with `operation: git.upload-pack`, `outcome: accepted`, but that event is emitted before `Runner.Start`. Process launch must be proved by the fake-SSH argv side effect, not audit alone.

## Review-follow-up execution scope

Tasks 1–8 are the completed baseline through commit `ce94d61`. The approved host-installer refactor starts from spec commit `7c2f8b6`; execute Tasks 9–13 only. Preserve Tasks 1–8 as historical implementation context rather than repeating their commits or destructive verification steps.

---

### Task 1: Example policy and accidental-secret guards

**Files:**
- Create: `examples/docker/.gitignore`
- Create: `examples/docker/.env.example`
- Create: `examples/docker/config/repowolf.yaml`

**Interfaces:**
- `config/repowolf.yaml` exposes literal `__OWNER__`/`__NAME__` placeholders consumed by Task 3's Bash renderer.
- `.env` names consumed by compose are exactly `GH_TOKEN` and `REPOWOLF_TOKEN_AGENT`.

- [ ] **Step 1: Create `.gitignore`**

```gitignore
# Accidental-commit protection only; `git add -f` can bypass these rules.
state/
.env
```

- [ ] **Step 2: Create `.env.example`**

```dotenv
# Provider token used only by the broker container.
# Obtain on the host after `gh auth login`: gh auth token
GH_TOKEN=

# Generated and maintained by bootstrap.sh. Never commit the real value.
REPOWOLF_TOKEN_AGENT=
```

- [ ] **Step 3: Create `config/repowolf.yaml`**

```yaml
apiVersion: repowolf.dev/v1alpha1
listen: "0.0.0.0:8443"

tls:
  certificate: /run/repowolf/tls/tls.crt
  privateKey: /run/repowolf/tls/tls.key

tools:
  gh: null
  ssh: null

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    sshPort: 22

repositories:
  example:
    provider: github-public
    owner: "__OWNER__"
    name: "__NAME__"
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
      - repository: example
        capabilities:
          - repository:read
          - issues:read
          - issues:write
          - pull_requests:read
          - pull_requests:write
          - statuses:read
          - git:read
          - git:write
          # actions:read deliberately omitted: `gh run list` must be denied.

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
```

- [ ] **Step 4: Render and validate the policy without dynamic sed**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
while IFS= read -r line || [ -n "$line" ]; do
  line=${line//__OWNER__/rochecompaan}
  line=${line//__NAME__/repowolf}
  printf '%s\n' "$line"
done < examples/docker/config/repowolf.yaml > /tmp/repowolf-docker-policy.yaml
go run ./cmd/repowolf config validate --config /tmp/repowolf-docker-policy.yaml
rm /tmp/repowolf-docker-policy.yaml
```

Expected: `configuration valid`, exit 0.

- [ ] **Step 5: Verify ignore behavior (without overclaiming enforcement)**

```bash
git check-ignore -v examples/docker/state examples/docker/.env
```

Expected: two matches from `examples/docker/.gitignore`.

- [ ] **Step 6: Commit**

```bash
git add examples/docker/.gitignore examples/docker/.env.example examples/docker/config/repowolf.yaml
git commit -m "feat(examples): scaffold docker policy and secret guards"
```

---

### Task 2: BusyBox-compatible sandbox Dockerfile

**Files:**
- Create: `examples/docker/sandbox/Dockerfile`

**Interfaces:**
- Produces image `repowolf-sandbox:local` for Tasks 4–5.
- Runtime contract: UID/GID 65532, `gh` and `repowolf-git-ssh` symlink to `repowolf-client`, git present, real ssh absent, `GIT_SSH_COMMAND=repowolf-git-ssh`.

- [ ] **Step 1: Create `sandbox/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

FROM alpine:3 AS fetch
ARG REPOWOLF_VERSION=v0.1.0
ARG REPOWOLF_RELEASE_ROOT=https://github.com/rochecompaan/repowolf/releases/download
ARG TARGETARCH
RUN apk add --no-cache curl ca-certificates
WORKDIR /fetch
RUN set -eu; \
    archive="repowolf_linux_${TARGETARCH}.tar.gz"; \
    base="${REPOWOLF_RELEASE_ROOT}/${REPOWOLF_VERSION}"; \
    curl -fsSLo checksums.txt "${base}/checksums.txt"; \
    curl -fsSLo "$archive" "${base}/${archive}"; \
    awk -v archive="$archive" '$2 == archive { print }' checksums.txt > selected-checksum.txt; \
    test "$(wc -l < selected-checksum.txt)" -eq 1; \
    sha256sum -c selected-checksum.txt; \
    tar -xzf "$archive" repowolf-client

FROM alpine:3
RUN apk add --no-cache git ca-certificates \
    && addgroup -g 65532 agent \
    && adduser -D -u 65532 -G agent agent
COPY --from=fetch /fetch/repowolf-client /usr/local/bin/repowolf-client
RUN ln -s repowolf-client /usr/local/bin/gh \
    && ln -s repowolf-client /usr/local/bin/repowolf-git-ssh
ENV GIT_SSH_COMMAND=repowolf-git-ssh
USER 65532:65532
WORKDIR /home/agent
CMD ["sh"]
```

- [ ] **Step 2: Build through a current-commit snapshot fixture with guaranteed cleanup**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
nix develop -c goreleaser release --snapshot --clean
fixture=$(mktemp -d /tmp/repowolf-release-fixture.XXXXXX)
mkdir -p "$fixture/snapshot"
cp dist/checksums.txt dist/repowolf_linux_*.tar.gz "$fixture/snapshot/"
cat > "$fixture/server.go" <<'EOF'
package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: release-server <directory>")
	}
	log.Fatal(http.ListenAndServe("127.0.0.1:8765", http.FileServer(http.Dir(os.Args[1]))))
}
EOF
nix develop -c go build -o "$fixture/release-server" "$fixture/server.go"
"$fixture/release-server" "$fixture" >/tmp/repowolf-release-server.log 2>&1 &
server_pid=$!
cleanup_release_fixture() {
  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
  rm -rf "$fixture" /tmp/repowolf-release-server.log
}
trap cleanup_release_fixture EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
fixture_ready=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  if (exec 3<>/dev/tcp/127.0.0.1/8765) 2>/dev/null; then
    fixture_ready=1
    break
  fi
  sleep 1
done
if [ "$fixture_ready" -ne 1 ]; then
  cat /tmp/repowolf-release-server.log >&2
  exit 1
fi
docker build --network=host \
  --build-arg REPOWOLF_VERSION=snapshot \
  --build-arg REPOWOLF_RELEASE_ROOT=http://127.0.0.1:8765 \
  -t repowolf-sandbox:local examples/docker/sandbox
cleanup_release_fixture
trap - EXIT INT TERM
```

Expected: selected archive prints `repowolf_linux_<arch>.tar.gz: OK`; no unsupported-option error. The trap is installed immediately after start, so build failure/interruption cannot leave port 8765 occupied.

- [ ] **Step 3: Verify runtime contract**

```bash
docker run --rm repowolf-sandbox:local sh -c '
  set -e
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh >/dev/null
  git --version >/dev/null
  test "$(id -u)" = "65532"
  test "$(id -g)" = "65532"
  test "$GIT_SSH_COMMAND" = "repowolf-git-ssh"
  test -z "${GH_TOKEN+x}"
'
```

Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add examples/docker/sandbox/Dockerfile
git commit -m "feat(examples): add checksum-verified sandbox image"
```

---

### Task 3: Safe portable bootstrap and readiness helper

**Files:**
- Create: `examples/docker/bootstrap.sh`
- Create: `examples/docker/wait-for-broker.sh`
- Create: `examples/docker/test-wait-for-broker.sh`
- Create: `examples/docker/test-bootstrap-unreadable-ssh.sh`

**Interfaces:**
- Inputs: required `REPOWOLF_REPO`; optional `REPOWOLF_IMAGE`; optional pair `REPOWOLF_SSH_KEY`/`REPOWOLF_KNOWN_HOSTS`.
- Fixed outputs consumed by compose: `state/config.yaml`, `state/tls/{ca.crt,tls.crt,tls.key}`, `state/token`, optional `state/ssh/{id_ed25519,known_hosts,config}`, and `.env`.
- `wait-for-broker.sh <host> <port> <attempts>` is reused verbatim by README and CI.

- [ ] **Step 1: Create `bootstrap.sh`**

```bash
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
```

- [ ] **Step 2: Create `wait-for-broker.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

host=${1:-127.0.0.1}
port=${2:-8443}
attempts=${3:-30}
if [[ ! $port =~ ^[1-9][0-9]{0,4}$ ]] || [ "$port" -gt 65535 ] || \
   [[ ! $attempts =~ ^[1-9][0-9]{0,2}$ ]] || [ "$attempts" -gt 300 ]; then
  echo "usage: wait-for-broker.sh [host] [port] [attempts]" >&2
  exit 2
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
  if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "wait-for-broker: $host:$port not ready after $attempts attempts; run docker compose logs repowolf" >&2
exit 1
```

```bash
chmod +x examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh \
  examples/docker/test-wait-for-broker.sh examples/docker/test-bootstrap-unreadable-ssh.sh
```

- [ ] **Step 3: Run shell checks without masking findings**

```bash
bash -n examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh \
  examples/docker/test-wait-for-broker.sh examples/docker/test-bootstrap-unreadable-ssh.sh
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh \
    examples/docker/test-wait-for-broker.sh examples/docker/test-bootstrap-unreadable-ssh.sh
else
  echo "shellcheck not installed; skipped"
fi
examples/docker/test-wait-for-broker.sh
examples/docker/test-bootstrap-unreadable-ssh.sh
```

Expected: `bash -n`, the readiness timeout/argument regression test, and the unreadable SSH-input preflight regression test exit 0. The SSH regression isolates an unreadable key with readable known-hosts, then readable key with unreadable known-hosts; each fails before state creation or Docker. If shellcheck exists, findings fail the step; only absence prints `skipped`.

- [ ] **Step 4: Build/load current broker image**

```bash
image="$(nix build .#ociImage --no-link --print-out-paths)"
docker load -i "$image"
```

Expected: `Loaded image: repowolf:mvp`.

- [ ] **Step 5: Verify hostile repository inputs are rejected before state creation**

```bash
cd examples/docker
for invalid in \
  'owner/repo/extra' \
  'owner name/repo' \
  'owner/repo;e echo injected' \
  'owner/repo&bad' \
  '/repo' \
  'owner/'; do
  if REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO="$invalid" ./bootstrap.sh; then
    echo "accepted invalid repository: $invalid" >&2
    exit 1
  fi
  test ! -e state
done
newline_repo=$(printf 'owner/repo\necho injected')
if REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO="$newline_repo" ./bootstrap.sh; then
  echo "accepted newline repository" >&2
  exit 1
fi
test ! -e state
for edge in 'foo/null' 'foo/Null'; do
  REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO="$edge" ./bootstrap.sh
  docker run --rm --user 65532:65532 \
    -v "$PWD/state/config.yaml:/config.yaml:ro" \
    repowolf:mvp config validate --config /config.yaml
  rm -rf -- state .env
done
if REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO='foo/-' ./bootstrap.sh; then
  echo "accepted repository name with non-alphanumeric first character" >&2
  exit 1
fi
test ! -e state
mkdir .env
if REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO='foo/repo' ./bootstrap.sh; then
  echo "accepted a non-regular .env destination" >&2
  exit 1
fi
test ! -e state
rm -rf -- .env
cd ../..
```

Expected: invalid repository input exits 2 with no `state/` and no injected command output. `null` and `Null` render as YAML strings and validate as OCI UID/GID 65532; `-` is rejected because RepoWolf repository names must start alphanumeric. A directory `.env` is rejected before state creation.

- [ ] **Step 6: Functional bootstrap and permissions**

```bash
cd examples/docker
ssh_test=$(mktemp -d /tmp/repowolf-bootstrap-ssh.XXXXXX)
ssh-keygen -q -t ed25519 -N '' -f "$ssh_test/id_ed25519"
printf 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestOnly\n' > "$ssh_test/known_hosts"
cp .env.example .env
REPOWOLF_IMAGE=repowolf:mvp \
REPOWOLF_REPO=rochecompaan/repowolf \
REPOWOLF_SSH_KEY="$ssh_test/id_ed25519" \
REPOWOLF_KNOWN_HOSTS="$ssh_test/known_hosts" \
./bootstrap.sh
rm -rf "$ssh_test"
test "$(stat -c %a state/token)" = "600"
test "$(stat -c %a state/config.yaml)" = "640"
test "$(stat -c %g state/config.yaml)" = "65532"
test "$(stat -c %a state/tls)" = "750"
test "$(stat -c %a state/tls/tls.key)" = "640"
test "$(stat -c %g state/tls/tls.key)" = "65532"
test "$(stat -c %a state/tls/ca.key)" = "600"
test "$(stat -c %u state/ssh/config)" = "0"
test "$(stat -c %g state/ssh/config)" = "65532"
test "$(stat -c %a state/ssh/config)" = "640"
test "$(stat -c %u state/ssh/id_ed25519)" = "65532"
test "$(stat -c %a state/ssh/id_ed25519)" = "600"
docker run --rm --user 65532:65532 -v "$PWD/state/tls/tls.key:/key:ro" alpine:3 test -r /key
docker run --rm --user 65532:65532 -v "$PWD/state/config.yaml:/config.yaml:ro" repowolf:mvp config validate --config /config.yaml
ssh_effective=$(mktemp /tmp/repowolf-ssh-effective.XXXXXX)
docker run --rm -v "$PWD/state/ssh:/tmp/.ssh:ro" --entrypoint ssh repowolf:mvp -G github.com > "$ssh_effective"
grep -E '^identityagent[[:space:]]+none$' "$ssh_effective"
grep -E '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' "$ssh_effective"
grep -E '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' "$ssh_effective"
rm "$ssh_effective"
grep -q '^REPOWOLF_TOKEN_AGENT=.\+' .env
git check-ignore -q state && git check-ignore -q .env
```

Expected: all checks pass; bootstrap itself validates `config.yaml` as OCI UID/GID 65532 and validates the effective OpenSSH values before committing `.env`. The actual OCI user can read `tls.key` and validate `config.yaml`; it accepts the root-owned OpenSSH config with the intended values. `ca.key` remains host-private.

Verify duplicate refusal, then remove only the disposable dummy state from this test:

```bash
if REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO=rochecompaan/repowolf ./bootstrap.sh; then
  echo "bootstrap unexpectedly overwrote state" >&2
  exit 1
fi
# Destructive, but these paths contain only the dummy verification state above.
rm -rf -- state .env
cd ../..
```

- [ ] **Step 7: Commit**

```bash
git add examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh examples/docker/test-wait-for-broker.sh examples/docker/test-bootstrap-unreadable-ssh.sh
git commit -m "feat(examples): add safe docker bootstrap and readiness wait"
```

---

### Task 4: Compose stack and complete local smoke

**Files:**
- Create: `examples/docker/compose.yaml`
- Create: `examples/docker/compose.smoke.yaml` (test-only fake-SSH mount)

**Interfaces:**
- Consumes fixed outputs from Task 3.
- Broker receives individual TLS cert/key mounts and broker-only `.ssh`; sandbox receives only public CA/principal token.
- Produces the exact behavior asserted again by Task 5 CI.

- [ ] **Step 1: Create `compose.yaml`**

```yaml
name: repowolf-example

services:
  repowolf:
    image: ${REPOWOLF_IMAGE:-ghcr.io/rochecompaan/repowolf:v0.1.0}
    command: serve --config /etc/repowolf/repowolf.yaml
    environment:
      GH_TOKEN: ${GH_TOKEN:?copy .env.example to .env and set GH_TOKEN}
      REPOWOLF_TOKEN_AGENT: ${REPOWOLF_TOKEN_AGENT:?run ./bootstrap.sh first}
    volumes:
      - ./state/config.yaml:/etc/repowolf/repowolf.yaml:ro
      - ./state/tls/tls.crt:/run/repowolf/tls/tls.crt:ro
      - ./state/tls/tls.key:/run/repowolf/tls/tls.key:ro
      - ./state/ssh:/tmp/.ssh:ro
    ports:
      - "127.0.0.1:8443:8443"
    restart: unless-stopped

  sandbox:
    image: repowolf-sandbox:local
    build:
      context: ./sandbox
      args:
        REPOWOLF_VERSION: ${REPOWOLF_VERSION:-v0.1.0}
        REPOWOLF_RELEASE_ROOT: ${REPOWOLF_RELEASE_ROOT:-https://github.com/rochecompaan/repowolf/releases/download}
    environment:
      REPOWOLF_ENDPOINT: https://repowolf:8443
      REPOWOLF_TOKEN: ${REPOWOLF_TOKEN_AGENT:?run ./bootstrap.sh first}
      REPOWOLF_CA_FILE: /run/repowolf/ca.crt
    volumes:
      - ./state/tls/ca.crt:/run/repowolf/ca.crt:ro
    depends_on:
      - repowolf
```

- [ ] **Step 2: Create `compose.smoke.yaml`**

```yaml
# Test-only override: mounts an observable static fake-SSH fixture into the
# trusted broker. Never used by the user walkthrough.
services:
  repowolf:
    environment:
      FAKE_SSH_LOG: /run/repowolf/test/ssh-argv
    volumes:
      - ./state/test:/run/repowolf/test
```

- [ ] **Step 3: Validate compose interpolation**

```bash
cd examples/docker
printf 'GH_TOKEN=dummy\nREPOWOLF_TOKEN_AGENT=dummy\n' > .env
REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml config --quiet
rm .env
cd ../..
```

Expected: exit 0.

- [ ] **Step 4: Bootstrap disposable provider state and install observable fake SSH**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
printf 'GH_TOKEN=dummy-ci-token\n' > .env
REPOWOLF_IMAGE=repowolf:mvp REPOWOLF_REPO=rochecompaan/repowolf ./bootstrap.sh
mkdir -p state/test
cat > /tmp/repowolf-fake-ssh.go <<'EOF'
package main

import (
  "fmt"
  "os"
)

func main() {
  path := os.Getenv("FAKE_SSH_LOG")
  if path == "" {
    fmt.Fprintln(os.Stderr, "FAKE_SSH_LOG is unset")
    os.Exit(90)
  }
  output, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
  if err != nil {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(91)
  }
  for _, argument := range os.Args[1:] {
    fmt.Fprintln(output, argument)
  }
  if err := output.Close(); err != nil {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(92)
  }
  os.Exit(42)
}
EOF
nix develop -c env CGO_ENABLED=0 go build -o state/test/fake-ssh /tmp/repowolf-fake-ssh.go
rm /tmp/repowolf-fake-ssh.go
: > state/test/ssh-argv
awk '
  $0 == "  ssh: null" { print "  ssh: /run/repowolf/test/fake-ssh"; next }
  { print }
' state/config.yaml > state/config.smoke.yaml
mv state/config.smoke.yaml state/config.yaml
docker run --rm --user 0:0 \
  -e HOST_UID="$(id -u)" \
  -v "$PWD/state/config.yaml:/config.yaml" \
  -v "$PWD/state/test:/test" \
  alpine:3 sh -eu -c '
    chown "$HOST_UID:65532" /config.yaml /test /test/fake-ssh /test/ssh-argv
    chmod 0640 /config.yaml
    chmod 0750 /test
    chmod 0550 /test/fake-ssh
    chmod 0660 /test/ssh-argv
  '
docker run --rm --user 65532:65532 -v "$PWD/state/config.yaml:/config.yaml:ro" repowolf:mvp config validate --config /config.yaml
```

Expected: config validates as broker UID 65532; fake executable and pre-created argv log are accessible to broker GID 65532. The `awk` replacement is fixed test data and consumes no untrusted input.

- [ ] **Step 5: Start and wait (no readiness race)**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
```

Expected: wait exits 0. On timeout, print `docker compose logs repowolf` before debugging.

- [ ] **Step 6: Assert granted/denied GitHub policy outcomes via audit**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
set -euo pipefail
if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh repo view --repo rochecompaan/repowolf; then
  echo "expected upstream failure with dummy GH_TOKEN" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
grep 'github.repository_view' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"accepted"'

if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh run list --repo rochecompaan/repowolf; then
  echo "expected policy denial" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
grep -E '"operation":[[:space:]]*"/repowolf\.v1\.GitHubService/Execute"' /tmp/repowolf-broker.log \
  | grep -E '"outcome":[[:space:]]*"denied"' \
  | grep -E '"reason":[[:space:]]*"PermissionDenied"'
if grep 'github.run_list' /tmp/repowolf-broker.log | grep -qE '"outcome":[[:space:]]*"accepted"'; then
  echo "github.run_list must not be accepted" >&2
  exit 1
fi
```

- [ ] **Step 7: Prove broker launched SSH with the exact upload-pack argv**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
set -euo pipefail
if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git; then
  echo "fake SSH unexpectedly succeeded" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
grep 'git.upload-pack' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"accepted"'
diff -u <(cat <<'EOF'
-T
-p
22
--
git@github.com
git-upload-pack 'rochecompaan/repowolf.git'
EOF
) state/test/ssh-argv
grep 'git.upload-pack' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"failed"' \
  | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
```

Expected: fake SSH exits 42; its host-visible log exactly proves `Runner.Start` executed the configured provider process with the required upload-pack argv. Audit acceptance is a secondary policy assertion, not the process-launch proof.

- [ ] **Step 8: Assert sandbox boundary**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm --entrypoint sh sandbox -c '
  set -e
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh >/dev/null
  test "$(id -u)" = "65532"
'
```

Expected: exit 0.

- [ ] **Step 9: Tear down disposable verification state and commit**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
docker compose -f compose.yaml -f compose.smoke.yaml down -v
# Destructive only to disposable dummy values created in Steps 4–8.
rm -rf -- state .env
cd ../..
git add examples/docker/compose.yaml examples/docker/compose.smoke.yaml
git commit -m "feat(examples): add compose broker and observable SSH smoke"
```

---

### Task 5: CI smoke job (current-commit archives, Git included)

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes every script/path/audit contract from Tasks 1–4.
- No release tag required: builds snapshot archives and current broker image in-job.

- [ ] **Step 1: Add `docker-example-smoke` before `release-smoke`**

```yaml
  docker-example-smoke:
    needs: test
    runs-on: ubuntu-24.04
    defaults:
      run:
        shell: bash -euo pipefail {0}
        working-directory: examples/docker
    env:
      REPOWOLF_IMAGE: repowolf:mvp
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
      - uses: cachix/install-nix-action@b4b293eae0b79aac8a161bb32925a5508c9cca93 # v31
      - name: Check example shell syntax and behavior
        run: |
          bash -n bootstrap.sh wait-for-broker.sh test-wait-for-broker.sh test-bootstrap-unreadable-ssh.sh
          ./test-wait-for-broker.sh
          ./test-bootstrap-unreadable-ssh.sh
      - name: Build and load broker image from this commit
        working-directory: .
        run: |
          image="$(nix build .#ociImage --no-link --print-out-paths)"
          docker load -i "$image"
      - name: Build sandbox image from current-commit archives
        working-directory: .
        run: |
          set -euo pipefail
          nix develop -c goreleaser release --snapshot --clean
          fixture=$(mktemp -d /tmp/repowolf-release-fixture.XXXXXX)
          mkdir -p "$fixture/snapshot"
          cp dist/checksums.txt dist/repowolf_linux_*.tar.gz "$fixture/snapshot/"
          cat > "$fixture/server.go" <<'EOF'
          package main

          import (
            "log"
            "net/http"
            "os"
          )

          func main() {
            if len(os.Args) != 2 {
              log.Fatal("usage: release-server <directory>")
            }
            log.Fatal(http.ListenAndServe("127.0.0.1:8765", http.FileServer(http.Dir(os.Args[1]))))
          }
          EOF
          nix develop -c go build -o "$fixture/release-server" "$fixture/server.go"
          "$fixture/release-server" "$fixture" >/tmp/repowolf-release-server.log 2>&1 &
          server_pid=$!
          cleanup() {
            kill "$server_pid" 2>/dev/null || true
            wait "$server_pid" 2>/dev/null || true
            rm -rf "$fixture" /tmp/repowolf-release-server.log
          }
          trap cleanup EXIT
          trap 'exit 130' INT
          trap 'exit 143' TERM
          fixture_ready=0
          for ((attempt = 1; attempt <= 30; attempt++)); do
            if (exec 3<>/dev/tcp/127.0.0.1/8765) 2>/dev/null; then
              fixture_ready=1
              break
            fi
            sleep 1
          done
          if [ "$fixture_ready" -ne 1 ]; then
            cat /tmp/repowolf-release-server.log >&2
            exit 1
          fi
          docker build --network=host \
            --build-arg REPOWOLF_VERSION=snapshot \
            --build-arg REPOWOLF_RELEASE_ROOT=http://127.0.0.1:8765 \
            -t repowolf-sandbox:local examples/docker/sandbox
      - name: Bootstrap disposable state and verify broker ownership
        run: |
          ssh_test=$(mktemp -d /tmp/repowolf-ci-ssh.XXXXXX)
          ssh-keygen -q -t ed25519 -N '' -f "$ssh_test/id_ed25519"
          printf 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestOnly\n' > "$ssh_test/known_hosts"
          printf 'GH_TOKEN=dummy-ci-token\n' > .env
          REPOWOLF_REPO=rochecompaan/repowolf \
          REPOWOLF_SSH_KEY="$ssh_test/id_ed25519" \
          REPOWOLF_KNOWN_HOSTS="$ssh_test/known_hosts" \
          ./bootstrap.sh
          rm -rf "$ssh_test"
          docker run --rm --user 65532:65532 -v "$PWD/state/config.yaml:/config.yaml:ro" \
            repowolf:mvp config validate --config /config.yaml
          docker run --rm -v "$PWD/state/ssh:/tmp/.ssh:ro" \
            --entrypoint ssh repowolf:mvp -G github.com > /tmp/ssh-effective
          grep -E '^identityagent[[:space:]]+none$' /tmp/ssh-effective
          grep -E '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' /tmp/ssh-effective
          grep -E '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' /tmp/ssh-effective
      - name: Install observable fake SSH fixture
        working-directory: .
        run: |
          mkdir -p examples/docker/state/test
          cat > /tmp/repowolf-fake-ssh.go <<'EOF'
          package main

          import (
            "fmt"
            "os"
          )

          func main() {
            path := os.Getenv("FAKE_SSH_LOG")
            if path == "" {
              fmt.Fprintln(os.Stderr, "FAKE_SSH_LOG is unset")
              os.Exit(90)
            }
            output, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
            if err != nil {
              fmt.Fprintln(os.Stderr, err)
              os.Exit(91)
            }
            for _, argument := range os.Args[1:] {
              fmt.Fprintln(output, argument)
            }
            if err := output.Close(); err != nil {
              fmt.Fprintln(os.Stderr, err)
              os.Exit(92)
            }
            os.Exit(42)
          }
          EOF
          nix develop -c env CGO_ENABLED=0 go build -o examples/docker/state/test/fake-ssh /tmp/repowolf-fake-ssh.go
          rm /tmp/repowolf-fake-ssh.go
          : > examples/docker/state/test/ssh-argv
          awk '
            $0 == "  ssh: null" { print "  ssh: /run/repowolf/test/fake-ssh"; next }
            { print }
          ' examples/docker/state/config.yaml > examples/docker/state/config.smoke.yaml
          mv examples/docker/state/config.smoke.yaml examples/docker/state/config.yaml
          docker run --rm --user 0:0 \
            -e HOST_UID="$(id -u)" \
            -v "$PWD/examples/docker/state/config.yaml:/config.yaml" \
            -v "$PWD/examples/docker/state/test:/test" \
            alpine:3 sh -eu -c '
              chown "$HOST_UID:65532" /config.yaml /test /test/fake-ssh /test/ssh-argv
              chmod 0640 /config.yaml
              chmod 0750 /test
              chmod 0550 /test/fake-ssh
              chmod 0660 /test/ssh-argv
            '
          docker run --rm --user 65532:65532 -v "$PWD/examples/docker/state/config.yaml:/config.yaml:ro" \
            repowolf:mvp config validate --config /config.yaml
      - name: Start broker and wait for readiness
        run: |
          docker compose -f compose.yaml -f compose.smoke.yaml up -d repowolf
          if ! ./wait-for-broker.sh 127.0.0.1 8443 30; then
            docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf
            exit 1
          fi
      - name: Assert GitHub policy outcomes
        run: |
          if docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh repo view --repo rochecompaan/repowolf; then
            echo "expected upstream failure with dummy GH_TOKEN" >&2
            exit 1
          fi
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
          grep 'github.repository_view' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"accepted"'
          if docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh run list --repo rochecompaan/repowolf; then
            echo "expected policy denial" >&2
            exit 1
          fi
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
          grep -E '"operation":[[:space:]]*"/repowolf\.v1\.GitHubService/Execute"' /tmp/repowolf-broker.log \
            | grep -E '"outcome":[[:space:]]*"denied"' \
            | grep -E '"reason":[[:space:]]*"PermissionDenied"'
          if grep 'github.run_list' /tmp/repowolf-broker.log | grep -qE '"outcome":[[:space:]]*"accepted"'; then
            echo "github.run_list must not be accepted" >&2
            exit 1
          fi
      - name: Assert broker launched fake SSH with upload-pack argv
        run: |
          if docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox \
            git ls-remote git@github.com:rochecompaan/repowolf.git; then
            echo "fake SSH unexpectedly succeeded" >&2
            exit 1
          fi
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf > /tmp/repowolf-broker.log
          grep 'git.upload-pack' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"accepted"'
          diff -u <(cat <<'EOF'
          -T
          -p
          22
          --
          git@github.com
          git-upload-pack 'rochecompaan/repowolf.git'
          EOF
          ) state/test/ssh-argv
          grep 'git.upload-pack' /tmp/repowolf-broker.log | grep -E '"outcome":[[:space:]]*"failed"' \
            | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
      - name: Assert sandbox boundary
        run: |
          docker compose -f compose.yaml -f compose.smoke.yaml run --rm --entrypoint sh sandbox -c '
            set -e
            test -z "${GH_TOKEN+x}"
            test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
            test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
            ! command -v ssh >/dev/null
            test "$(id -u)" = "65532"
          '
      - name: Tear down
        if: always()
        run: docker compose -f compose.yaml -f compose.smoke.yaml down -v
```

- [ ] **Step 2: Validate workflow YAML with declared repository dependencies**

Use Go already declared by `go.mod`; no PyYAML assumption:

```bash
checker=/tmp/repowolf-check-workflow-$$.go
cleanup_checker() { rm -f "$checker"; }
trap cleanup_checker EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cat > "$checker" <<'EOF'
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	var value any
	if err := yaml.Unmarshal(data, &value); err != nil {
		panic(err)
	}
	fmt.Println("ci.yml parses")
}
EOF
go run "$checker" .github/workflows/ci.yml
cleanup_checker
trap - EXIT INT TERM
```

Expected: `ci.yml parses`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(examples): smoke-test docker GitHub and Git paths"
```

---

### Task 6: Canonical Docker example README

**Files:**
- Create: `examples/docker/README.md`

**Interfaces:**
- Commands must exactly match Tasks 2–4 (`--repo`, readiness helper, fixed paths, SSH requirements).
- Task 7 links here.

- [ ] **Step 1: Create README**

````markdown
# RepoWolf Docker example

This example keeps provider credentials and real provider tools on the broker
side. The sandbox gets only `repowolf-client` (as `gh` and
`repowolf-git-ssh`), its RepoWolf token, endpoint, and public CA.

Two run modes share the same sandbox image:

- **Host broker + sandbox container** — the production topology.
- **Compose broker + sandbox container** — no host RepoWolf install.

## Requirements and platform scope

- Docker with the Compose v2 plugin.
- Bash (the scripts are Bash 3.2-compatible).
- Linux is tested. The sandbox release build supports `linux/amd64` and
  `linux/arm64`. On macOS run from a Unix shell. On Windows run from WSL2;
  native PowerShell/cmd is not supported by this example.

## Build the sandbox

```sh
docker build -t repowolf-sandbox:local examples/docker/sandbox
```

The build downloads `v0.1.0` by default and verifies the selected archive
against the published checksum file. Override with
`--build-arg REPOWOLF_VERSION=<tag>` when upgrading. Release archives are
checksum-verified; they are not signed.

The runtime image has git and CA roots, runs as UID/GID 65532, and contains
no real `gh`, OpenSSH, provider token, key, or agent socket.

## Path A: host broker + sandbox

Install RepoWolf on the Linux host per the top-level README. The following
creates the matching certificate state and complete service configuration. It
binds only the Docker bridge gateway (usually `172.17.0.1`), not `0.0.0.0`,
and resolves the service-side tools to absolute paths:

```sh
GH_PATH="$(command -v gh)"
SSH_PATH="$(command -v ssh)"
case "$GH_PATH:$SSH_PATH" in
  /*:/*) ;;
  *) echo "gh and ssh must resolve to absolute paths" >&2; exit 1 ;;
esac

sudo install -d -o root -g repowolf -m 0750 /var/lib/repowolf /etc/repowolf
sudo repowolf cert init --output /var/lib/repowolf/tls \
  --dns repowolf.internal --ip 127.0.0.1
# The broker identity must traverse these directories and read the certificate
# and server key. The private key remains restricted to root:repowolf.
sudo chown root:repowolf /var/lib/repowolf /var/lib/repowolf/tls \
  /var/lib/repowolf/tls/tls.crt /var/lib/repowolf/tls/tls.key
sudo chmod 0750 /var/lib/repowolf /var/lib/repowolf/tls
sudo chmod 0640 /var/lib/repowolf/tls/tls.crt /var/lib/repowolf/tls/tls.key
sudo chown root:root /var/lib/repowolf/tls/ca.key
sudo chmod 0600 /var/lib/repowolf/tls/ca.key

sudo tee /etc/repowolf/repowolf.yaml >/dev/null <<EOF
apiVersion: repowolf.dev/v1alpha1
listen: "172.17.0.1:9443"

tls:
  certificate: /var/lib/repowolf/tls/tls.crt
  privateKey: /var/lib/repowolf/tls/tls.key

tools:
  gh: $GH_PATH
  ssh: $SSH_PATH

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git

repositories:
  example:
    provider: github-public
    owner: rochecompaan
    name: repowolf
    git:
      denyRefs:
        - refs/heads/main
      denyDeletes: true
      maxRefUpdates: 16

principals:
  example-agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_EXAMPLE_AGENT
    grants:
      - repository: example
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
EOF
sudo chown root:repowolf /etc/repowolf/repowolf.yaml
sudo chmod 0640 /etc/repowolf/repowolf.yaml
sudo -u repowolf repowolf config validate --config /etc/repowolf/repowolf.yaml
```

This assumes the broker runs as `repowolf` (or an identity in group
`repowolf`). Do not loosen the private-key mode or give the sandbox the key.

The example policy principal is `example-agent`, so the broker must load
`REPOWOLF_TOKEN_EXAMPLE_AGENT` from a dedicated principal environment file,
without overwriting provider settings in `/run/repowolf/service.env`. Generate and
store it once in a protected file:

```sh
sudo install -d -o root -g root -m 0700 /run/repowolf
sudo install -o root -g root -m 0600 /dev/null /var/lib/repowolf/token
sudo install -o root -g root -m 0600 /dev/null /run/repowolf/example-agent.env
BROKER_TOKEN="$(repowolf token generate)"
printf '%s\n' "$BROKER_TOKEN" | sudo tee /var/lib/repowolf/token >/dev/null
printf 'REPOWOLF_TOKEN_EXAMPLE_AGENT=%s\n' "$BROKER_TOKEN" | \
  sudo tee /run/repowolf/example-agent.env >/dev/null
unset BROKER_TOKEN
sudo chown root:root /var/lib/repowolf/token /run/repowolf/example-agent.env
sudo chmod 0600 /var/lib/repowolf/token /run/repowolf/example-agent.env
```

Both `/run/repowolf/service.env` and `/run/repowolf/example-agent.env` must be
loaded by the broker before restart (for example with systemd
`EnvironmentFile=/run/repowolf/service.env` and
`EnvironmentFile=/run/repowolf/example-agent.env`). Do not print its contents.

The absolute paths avoid startup failures from ambiguous/empty PATH entries.
Restart the broker, then:

```sh
docker run --rm -it \
  -e REPOWOLF_ENDPOINT=https://172.17.0.1:9443 \
  -e REPOWOLF_SERVER_NAME=repowolf.internal \
  -e REPOWOLF_TOKEN="$(sudo cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

`REPOWOLF_SERVER_NAME` must match the host certificate's DNS SAN. Use
`--add-host=host.docker.internal:host-gateway` and endpoint
`https://host.docker.internal:9443` as an alternative to the numeric gateway.

Git operations additionally require the **host broker** to have an SSH
identity/agent and verified known-hosts state. GitHub requires authentication
even for public SSH clones.

## Path B: compose broker + sandbox

```sh
cd examples/docker
cp .env.example .env
# Set GH_TOKEN in .env (obtain on the host with: gh auth token)
REPOWOLF_REPO=rochecompaan/repowolf ./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo rochecompaan/repowolf
```

`bootstrap.sh` writes fixed paths `state/` and `.env`, because those are the
paths Compose reads. It refuses existing state. The wait is required:
`docker compose up -d` and `depends_on` do not signal TLS readiness.

### Policy-denial demonstration

The sample policy deliberately omits `actions:read`:

```sh
docker compose run --rm sandbox gh run list --repo rochecompaan/repowolf
# gh: GitHub operation failed
docker compose logs repowolf > /tmp/repowolf-broker.log
grep -E '"outcome":[[:space:]]*"denied"' /tmp/repowolf-broker.log
```

Client diagnostics are intentionally identical for policy/provider failures;
the broker audit log is the stable source of the outcome. Add
`- actions:read` to `state/config.yaml` and restart the broker to allow the
operation.

### Enable Git in compose

Every Git read/write requires broker-side authentication and host
verification. Prepare a repository-scoped deploy key and a verified
known-hosts file. Verify GitHub's published SSH fingerprints before trusting
`ssh-keyscan` output:

<https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints>

Bootstrap from a fresh state directory:

```sh
ssh-keyscan -t ed25519 github.com > /tmp/github-known-hosts
REPOWOLF_REPO=rochecompaan/repowolf \
REPOWOLF_SSH_KEY=/secure/path/to/deploy-key \
REPOWOLF_KNOWN_HOSTS=/tmp/github-known-hosts \
./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git
```

The private key/known-hosts/config are mounted only into the broker. The
sandbox still contains no SSH identity or OpenSSH client.

## Boundary proof

```sh
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

## Reset and secret handling

`.gitignore` prevents ordinary accidental adds of `.env` and `state/`, but
`git add -f` bypasses it. Treat both paths as secret material.

Deleting `state/` destroys the CA private key, server key, principal token,
and optional SSH key; deleting `.env` may destroy the only local copy of the
provider token. Back them up outside the repository before reset:

```sh
docker compose down
backup="${HOME}/.local/state/repowolf-example/$(date +%Y%m%d%H%M%S)"
mkdir -p "$backup"
mv state .env "$backup/"
```

Only use `rm -rf state .env` when that destruction is intended.

## Troubleshooting

- **Connection refused after compose start:** run `./wait-for-broker.sh`; if it
  times out, inspect `docker compose logs repowolf`.
- **TLS name mismatch:** set `REPOWOLF_SERVER_NAME` to the cert DNS SAN.
- **Host broker cannot be reached:** it is still bound to `127.0.0.1`; bind
  the Docker bridge gateway instead.
- **Broker `service failed`:** check TLS ownership/modes and pin absolute
  `tools.gh`/`tools.ssh` paths.
- **Git host-key/authentication failure:** supply both a verified known-hosts
  file and a usable broker-side key/agent. Read-only Git is not anonymous.
````
- [ ] **Step 2: Cross-check README commands**

Confirm every repository invocation uses `--repo`; compose quick start calls `wait-for-broker.sh`; Git section requires both SSH inputs; reset warning names destroyed material; no Docker-only/native-Windows or signed-artifact claims remain.

- [ ] **Step 3: Commit**

```bash
git add examples/docker/README.md
git commit -m "docs(examples): document safe docker workflows"
```

---

### Task 7: Link the example from the top-level README

**Files:**
- Modify: `README.md` (OCI section)

- [ ] **Step 1: Append after the current OCI mount-security paragraph**

```markdown

For a complete sandbox-image example with host-broker and compose
walkthroughs, see [examples/docker](examples/docker/README.md).
```

- [ ] **Step 2: Verify and commit**

```bash
grep -n 'examples/docker' README.md
git diff --check
git add README.md
git commit -m "docs(readme): link docker example"
```

---

### Task 8: Final verification and rollout handoff

**Files:** none.

- [ ] **Step 1: Repeat Task 4 full compose smoke from a clean state**

Expected: readiness succeeds; GitHub accepted/denied audit assertions pass; fake SSH records the exact upload-pack argv and exits with provider-failure audit; boundary checks pass; teardown completes.

- [ ] **Step 2: Manually verify host-broker success with real credentials**

Use a scratch broker bound to the actual `docker0` gateway, absolute host `gh`/`ssh` paths, `GH_TOKEN=$(gh auth token)`, the host's real SSH auth/known-hosts, and a fresh RepoWolf token/CA. From `repowolf-sandbox:local`, verify both commands exit 0:

```bash
gh repo view --repo rochecompaan/repowolf
git ls-remote git@github.com:rochecompaan/repowolf.git
```

Record the commands/result in the PR description; do not record credentials.

- [ ] **Step 3: Direct final checks (no masked statuses)**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
git diff --check
git status --short
go test -race ./...
```

Expected: clean diff check, only intended files before final commit state, all Go packages `ok`; any `FAIL` returns non-zero directly.

- [ ] **Step 4: Review commits**

```bash
git log --oneline main..HEAD
```

Expected: spec/plan commits plus seven implementation commits, no generated `state/`, `.env`, `dist/`, or temporary fixture files.

- [ ] **Step 5: Rollout handoff**

Report implementation/CI/manual evidence. After merge, ask the owner whether to publish `v0.1.0`; tagging/pushing is irreversible and must not occur without explicit confirmation. Then use `superpowers:finishing-a-development-branch` for integration options.

---

### Task 9: Host broker policy template and installer

**Files:**
- Create: `examples/docker/config/repowolf-host.yaml`
- Create: `examples/docker/test-install-host-broker.sh`
- Create: `examples/docker/install-host-broker.sh`

**Interfaces:**
- Consumes: `repowolf`, `sudo`, `docker`, `gh`, `ssh`, `id`, `getent`, `install`, `mktemp`, `ln`, `chmod`, `chown`, `rm`, and the approved `REPOWOLF_*` overrides from Global Constraints.
- Produces: `${REPOWOLF_STATE_DIR:-/var/lib/repowolf}/tls`, `${REPOWOLF_CONFIG_DIR:-/etc/repowolf}/repowolf.yaml`, and no token or provider environment file.
- Template contract: `repowolf-host.yaml` contains the exact line tokens `__REPOWOLF_LISTEN__`, `__REPOWOLF_TLS_CERTIFICATE__`, `__REPOWOLF_TLS_PRIVATE_KEY__`, `__REPOWOLF_GH_PATH__`, `__REPOWOLF_SSH_PATH__`, `__REPOWOLF_OWNER__`, and `__REPOWOLF_NAME__`; the installer replaces each whole scalar line with YAML single-quoted output and doubles embedded single quotes.
- Test-only contract: `REPOWOLF_TEST_VALIDATE_IMAGE=repowolf:mvp` makes the test's fake `repowolf config validate` delegate to the loaded image. It is not read by production scripts.

- [ ] **Step 1: Add the complete host policy template**

Create `examples/docker/config/repowolf-host.yaml`:

```yaml
apiVersion: repowolf.dev/v1alpha1
listen: __REPOWOLF_LISTEN__

tls:
  certificate: __REPOWOLF_TLS_CERTIFICATE__
  privateKey: __REPOWOLF_TLS_PRIVATE_KEY__

tools:
  gh: __REPOWOLF_GH_PATH__
  ssh: __REPOWOLF_SSH_PATH__

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    sshPort: 22

repositories:
  example:
    provider: github-public
    owner: __REPOWOLF_OWNER__
    name: __REPOWOLF_NAME__
    git:
      denyRefs:
        - refs/heads/main
      denyDeletes: true
      maxRefUpdates: 16

principals:
  example-agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_EXAMPLE_AGENT
    grants:
      - repository: example
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
```

- [ ] **Step 2: Write the failing host-installer behavior test**

Create `examples/docker/test-install-host-broker.sh` with `set -euo pipefail`, a private `mktemp -d` root, and an EXIT/INT/TERM trap. Copy the installer and template into that root before each case so every case is isolated.

The test harness must provide these executable shims under `$case_root/bin`:

- `docker`: for exactly `network inspect --format '{{(index .IPAM.Config 0).Gateway}}' bridge`, print `172.18.0.1`; reject every other argv.
- `repowolf`: `cert init` creates `ca.crt`, `ca.key`, `tls.crt`, and `tls.key`; `config validate` rejects any remaining `__REPOWOLF_` token and records each invocation. When `REPOWOLF_TEST_VALIDATE_IMAGE` is set, invoke the real Docker binary captured before PATH replacement and mount the requested config at `/config.yaml` for `repowolf:mvp config validate --config /config.yaml`.
- `sudo`: append shell-escaped argv to `$case_root/sudo.log`; remove `-u USER` before execution; for `install`, replace requested owner/group with the current test user/group while preserving modes; execute `test`, `mktemp`, `ln`, `chmod`, and `rm`; treat `chown` as a logged no-op. `FAIL_CLOSED_SUDO=1` must refuse every sudo argv without executing it, so root-equivalent override regressions cannot touch the real root. If `FAIL_INSTALLED_VALIDATE=1`, fail only the `config validate` call whose path ends in `/repowolf.yaml`; `FAIL_POLICY_PUBLISH=1` must create a competing policy at the final destination then fail the hard-link publication.
- `gh` and `ssh`: exit zero so their absolute shim paths are executable configuration values.

Use helpers with these exact calling conventions:

```bash
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
```

Cover the following observable cases:

```bash
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

expect_status 2 invalid-repository run_installer REPOWOLF_REPO=owner/name/extra
expect_status 2 wildcard-listen run_installer REPOWOLF_LISTEN=0.0.0.0:9443
expect_status 2 relative-gh run_installer REPOWOLF_GH_PATH=bin/gh
expect_status 2 relative-config run_installer REPOWOLF_CONFIG_DIR=etc/repowolf

test ! -s "$case_root/sudo.log"

for directory in REPOWOLF_CONFIG_DIR REPOWOLF_STATE_DIR REPOWOLF_RUNTIME_DIR; do
  # `/tmp/..`, `/.`, `//`, and a test-owned symlink to `/` return 2.
  # Every case sets FAIL_CLOSED_SUDO=1 and leaves sudo.log empty.
done

# Existing regular and dangling TLS/policy destinations return 1 before cert init.
# A failed policy hard-link race preserves the competing policy and removes the
# invocation-owned TLS directory and staging file.
# A test-owned directory symlink switched to `/` by the `sudo -v` shim returns
# 2 before any subsequent privileged action. A second shim retargets it to a
# different non-root test directory; it also returns 2, leaves both destination
# trees untouched, and reaches no mutating sudo command.
# A cleanup-race shim switches a configured ancestor during the first cleanup
# sudo removal; the remaining invocation-owned state survives and no later
# raw cleanup mutation runs through the changed ancestor.

rm -rf "$case_root/var/lib/repowolf/tls"
mkdir -p "$case_root/var/lib/repowolf" "$case_root/etc/repowolf"
touch "$case_root/var/lib/repowolf/.preexisting" \
  "$case_root/etc/repowolf/.preexisting"
expect_status 1 cleanup run_installer FAIL_INSTALLED_VALIDATE=1
test ! -e "$case_root/var/lib/repowolf/tls"
test ! -e "$case_root/etc/repowolf/repowolf.yaml"
test -e "$case_root/var/lib/repowolf/.preexisting"
test -e "$case_root/etc/repowolf/.preexisting"
```

Implement each case with a fresh `case_root`; do not reuse the successful installation for rejection/cleanup cases. Add one successful case whose `REPOWOLF_GH_PATH` contains a literal single quote and assert YAML renders it as two adjacent single quotes inside the scalar.

- [ ] **Step 3: Run the test to verify RED**

Run:

```bash
cd examples/docker
chmod +x test-install-host-broker.sh
./test-install-host-broker.sh
```

Expected: non-zero because `install-host-broker.sh` does not exist. The failure must occur before any real `sudo` command or system-path write.

- [ ] **Step 4: Implement strict input validation and rendering**

Create `examples/docker/install-host-broker.sh` with these foundations:

```bash
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

require_absolute_path() {
  label=$1
  value=$2
  reject_control "$label" "$value"
  case $value in
    /*) ;;
    *) fail_usage "$label must be an absolute path" ;;
  esac
}

# canonicalize_directory first requires an absolute, control-character-free
# value and performs lexical `.`/`..` normalization. It resolves accessible
# existing components with Bash `cd -P` and `pwd -P`, rejecting `/tmp/..`,
# `/.`, `//`, and accessible symlinks to `/` before sudo. An inaccessible
# existing directory is retained as a lexical suffix so a normal sudo-capable
# operator outside the broker group can continue. Retain those pre-sudo
# canonical values as immutable expected values. After `sudo -v`, resolve all
# original directory values in the running Bash interpreter through sudo,
# reject a privileged canonical `/`, and require every privileged result to
# equal its expected value before assigning final paths or deriving a
# destination. A symlink switched during `sudo -v` to either root or a
# different non-root directory therefore returns status 2 before a mutation.
# Re-resolve before every mutating sudo call. The EXIT trap must re-resolve
# immediately before *each* cleanup mutation and refuse that mutation if any
# configured directory changed.

yaml_quote() {
  value=${1//\'/\'\'}
  printf "'%s'" "$value"
}
```

Parse `REPOWOLF_REPO` with `^([A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?)/([A-Za-z0-9][A-Za-z0-9._-]{0,99})$`, assigning `OWNER=${BASH_REMATCH[1]}` and `NAME=${BASH_REMATCH[3]}`. Parse `REPOWOLF_LISTEN` with `^([A-Za-z0-9.-]+|\[[0-9A-Fa-f:]+\]):([1-9][0-9]{0,4})$`, convert the captured port with base 10, require `1..65535`, and reject wildcard hosts `0.0.0.0`, `::`, and `[::]`. When unset, resolve the gateway with this exact command and require an IPv4 result:

```bash
docker network inspect --format '{{(index .IPAM.Config 0).Gateway}}' bridge
```

Resolve `repowolf`, `gh`, `ssh`, and `sudo` with `command -v`; require absolute executable paths for `repowolf`, `gh`, and `ssh`. Validate the broker user with `id -u`, the group with `getent group`, then loop over the whitespace-separated output of `id -Gn "$BROKER_USER"` and require one exact group-name match before invoking `sudo`.

Render by matching complete template lines, not by evaluating environment text or building a `sed` program:

```bash
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
```

After pre-sudo lexical root rejection and `sudo -v`, perform privileged Bash
resolution before rendering. Write the render to a private `mktemp` file,
reject any remaining `__REPOWOLF_` token, and run
`repowolf config validate --config "$rendered"` against the resolved paths.

- [ ] **Step 5: Implement no-clobber privileged publication and cleanup**

After preflight succeeds:

1. Use `sudo test -e` plus `sudo test -L` to refuse every existing TLS path or policy path, including dangling symlinks.
2. Record whether `CONFIG_DIR` and `STATE_DIR` already existed, then create only those parents with root:`$BROKER_GROUP` mode `0750`.
3. Run `sudo "$REPOWOLF_BIN" cert init --output "$TLS_DIR" --dns repowolf.internal --ip 127.0.0.1 >/dev/null`. RepoWolf's certificate publisher already refuses an existing destination and leaves no final path on prepublication failure; mark `TLS_DIR` as invocation-owned only after success.
4. Set `STATE_DIR` and `TLS_DIR` to root:`$BROKER_GROUP` mode `0750`; `tls.crt`/`tls.key` to root:`$BROKER_GROUP` mode `0640`; `ca.crt` to root:root mode `0644`; and `ca.key` to root:root mode `0600`.
5. Create a root-owned sibling with `sudo mktemp "$CONFIG_DIR/.repowolf.yaml.XXXXXX"`, install the rendered file to it as root:`$BROKER_GROUP` mode `0640`, and publish it without overwrite using `sudo ln "$privileged_temp" "$CONFIG_FILE"`. Remove the sibling link after the final hard link succeeds.
6. Run `sudo -u "$BROKER_USER" "$REPOWOLF_BIN" config validate --config "$CONFIG_FILE"`.

The EXIT trap must remove the private render unconditionally. On non-zero exit it must remove only an invocation-owned privileged sibling, final config, TLS directory, and parent directories that this invocation created and left empty. Recheck immediately before every individual privileged cleanup mutation; after a recheck fails, do not run that mutation. Disable cleanup before printing:

```text
install-host-broker: installed /etc/repowolf/repowolf.yaml and /var/lib/repowolf/tls
install-host-broker: load /run/repowolf/service.env plus the principal environment before restarting the broker
```

Paths in output must reflect overrides rather than hard-coded defaults.

- [ ] **Step 6: Run focused GREEN verification**

Run:

```bash
cd examples/docker
chmod +x install-host-broker.sh
bash -n install-host-broker.sh test-install-host-broker.sh
./test-install-host-broker.sh
git diff --check
```

Expected: syntax exits zero; all isolated cases pass; no `/etc/repowolf`, `/var/lib/repowolf`, or `/run/repowolf` path is created by the test.

- [ ] **Step 7: Commit the host broker installer**

```bash
git add examples/docker/config/repowolf-host.yaml \
  examples/docker/install-host-broker.sh \
  examples/docker/test-install-host-broker.sh
git commit -m "feat(examples): add host broker installer"
```

---

### Task 10: Host principal installer

**Files:**
- Create: `examples/docker/test-install-host-principal.sh`
- Create: `examples/docker/install-host-principal.sh`

**Interfaces:**
- Consumes: `repowolf`, `sudo`, `id`, `getent`, `install`, `mktemp`, `ln`, and `rm`; `REPOWOLF_BROKER_USER`, `REPOWOLF_BROKER_GROUP`, `REPOWOLF_STATE_DIR`, and `REPOWOLF_RUNTIME_DIR` use the same defaults and validation as Task 9.
- Produces: `${REPOWOLF_STATE_DIR:-/var/lib/repowolf}/token` and `${REPOWOLF_RUNTIME_DIR:-/run/repowolf}/example-agent.env`, both root:root mode `0600`.
- Preserves: `${REPOWOLF_RUNTIME_DIR:-/run/repowolf}/service.env` byte-for-byte and never emits the generated token to stdout/stderr.

- [ ] **Step 1: Write the failing principal-installer behavior test**

Create `examples/docker/test-install-host-principal.sh` with isolated case roots and fake `repowolf`/`sudo` shims. The fake token command must print exactly:

```text
rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
```

The fake `sudo` logs shell-escaped argv, translates requested ownership to the current test identity for actual temp-file operations, supports `test`, `install`, `mktemp`, `ln`, and `rm`, and fails the environment-file publication when `FAIL_ENV_PUBLISH=1`.

Use this success contract:

```bash
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
```

Also cover:

- relative state/runtime directories and nonexistent broker user/group return status `2` before the first fake `sudo` call;
- an existing non-traversable state directory still permits the principal
  installation through the sudo Bash resolver without loosening production
  state permissions;
- an existing token, existing principal environment, or dangling symlink at either destination returns status `1` before token generation;
- multiline or malformed token output returns status `1` before privileged writes;
- environment publication failure removes the invocation-created token and environment file but preserves pre-existing parent markers and `service.env`.

- [ ] **Step 2: Run the test to verify RED**

Run:

```bash
cd examples/docker
chmod +x test-install-host-principal.sh
./test-install-host-principal.sh
```

Expected: non-zero because `install-host-principal.sh` does not exist, with no token text in terminal output.

- [ ] **Step 3: Implement the principal installer**

Create `examples/docker/install-host-principal.sh` with strict mode, `umask 077`, and the same `fail_usage`, `reject_control`, absolute-directory, user/group existence, and group-membership validation contracts as Task 9. Resolve `repowolf` and `sudo` before privileged work.

After unprivileged lexical root-equivalence/input/identity/tool validation, call
`sudo -v`, resolve state/runtime through the running Bash interpreter under
sudo (including a non-traversable existing broker state directory), and reject
the resulting root path or changed resolution. Use `sudo test -e` plus
`sudo test -L` to refuse either existing destination before token generation.
Then generate into a private file and validate the exact token shape before any
privileged write:

```bash
"$REPOWOLF_BIN" token generate > "$token_temp"
IFS= read -r token < "$token_temp"
if ! [[ $token =~ ^rw1_[A-Za-z0-9_-]{43}$ ]] || \
   [ "$(wc -l < "$token_temp")" -ne 1 ]; then
  echo 'install-host-principal: repowolf returned an invalid token' >&2
  exit 1
fi
printf 'REPOWOLF_TOKEN_EXAMPLE_AGENT=%s\n' "$token" > "$env_temp"
unset token
```

Create only missing state/runtime parent directories as root:root mode `0700`; do not chmod or chown a pre-existing parent. Implement this no-clobber helper for each destination:

```bash
publish_root_file() {
  source_path=$1
  destination=$2
  destination_dir=${destination%/*}
  staged=$(sudo mktemp "$destination_dir/.repowolf-install.XXXXXX")
  sudo install -o root -g root -m 0600 "$source_path" "$staged"
  sudo ln "$staged" "$destination"
  sudo rm -f -- "$staged"
}
```

Track the staged path and each published destination outside the helper so the EXIT trap can clean a failed two-file transaction. Publish the token first and environment second. On failure, remove only invocation-owned paths and newly created empty parents. On success, print only the installed paths and the requirement for the broker to load both `service.env` and `example-agent.env` before it starts or restarts; do not print or read `service.env`.

- [ ] **Step 4: Run focused GREEN verification**

Run:

```bash
cd examples/docker
chmod +x install-host-principal.sh
bash -n install-host-principal.sh test-install-host-principal.sh
./test-install-host-principal.sh
git diff --check
```

Expected: every success/refusal/cleanup/non-disclosure case passes and no real system path changes.

- [ ] **Step 5: Commit the principal installer**

```bash
git add examples/docker/install-host-principal.sh \
  examples/docker/test-install-host-principal.sh
git commit -m "feat(examples): add host principal installer"
```

---

### Task 11: Run host-installer behavior in CI

**Files:**
- Modify: `.github/workflows/ci.yml` (`docker-example-smoke` shell checks and post-image behavior step)

**Interfaces:**
- Consumes: executable scripts/tests from Tasks 9–10 and the `repowolf:mvp` image loaded by the current-commit Nix build.
- Produces: unconditional PR/`main` evidence that both installers pass behavior tests and that the rendered host policy passes the current broker's real `config validate`.

- [ ] **Step 1: Extend shell syntax coverage**

Change the existing syntax command to:

```bash
bash -n bootstrap.sh install-host-broker.sh install-host-principal.sh \
  wait-for-broker.sh test-bootstrap-unreadable-ssh.sh \
  test-install-host-broker.sh test-install-host-principal.sh \
  test-wait-for-broker.sh
```

Keep `test-wait-for-broker.sh` and `test-bootstrap-unreadable-ssh.sh` in that pre-image step. Do not hide an unavailable command with `|| echo`.

- [ ] **Step 2: Add post-image host-installer behavior**

Immediately after `Build and load broker image from this commit`, add:

```yaml
      - name: Check host installer behavior
        run: |
          REPOWOLF_TEST_VALIDATE_IMAGE=repowolf:mvp ./test-install-host-broker.sh
          ./test-install-host-principal.sh
```

The first test must execute two real `config validate` calls for its successful case while retaining fake cert generation and temporary install roots. The principal test remains fully local and must not print its fake token.

- [ ] **Step 3: Run the workflow-equivalent checks locally**

Run:

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
bash -n examples/docker/bootstrap.sh \
  examples/docker/install-host-broker.sh \
  examples/docker/install-host-principal.sh \
  examples/docker/wait-for-broker.sh \
  examples/docker/test-bootstrap-unreadable-ssh.sh \
  examples/docker/test-install-host-broker.sh \
  examples/docker/test-install-host-principal.sh \
  examples/docker/test-wait-for-broker.sh

image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
(
  cd examples/docker
  ./test-wait-for-broker.sh
  ./test-bootstrap-unreadable-ssh.sh
  REPOWOLF_TEST_VALIDATE_IMAGE=repowolf:mvp ./test-install-host-broker.sh
  ./test-install-host-principal.sh
)
```

Expected: every command exits zero; the broker test's real validator accepts both the private render and installed policy; temporary roots are removed.

- [ ] **Step 4: Parse workflow YAML without PyYAML**

Run from the repository root:

```bash
checker=$(mktemp /tmp/repowolf-workflow-check.XXXXXX.go)
trap 'rm -f "$checker"' EXIT INT TERM
cat > "$checker" <<'EOF'
package main

import (
  "fmt"
  "os"

  "gopkg.in/yaml.v3"
)

func main() {
  if len(os.Args) != 2 {
    panic("usage: workflow-check <path>")
  }
  data, err := os.ReadFile(os.Args[1])
  if err != nil {
    panic(err)
  }
  var document yaml.Node
  if err := yaml.Unmarshal(data, &document); err != nil {
    panic(err)
  }
  fmt.Println("workflow YAML parsed")
}
EOF
nix develop -c go run "$checker" .github/workflows/ci.yml
rm -f "$checker"
trap - EXIT INT TERM
```

Expected: `workflow YAML parsed` and exit zero.

- [ ] **Step 5: Commit CI integration**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(examples): test host installers"
```

---

### Task 12: Compose-first README and concise host walkthrough

**Files:**
- Modify: `examples/docker/README.md`

**Interfaces:**
- Consumes: `bootstrap.sh`, `install-host-broker.sh`, `install-host-principal.sh`, `wait-for-broker.sh`, and existing Compose/client commands.
- Produces: one canonical walkthrough ordered build → Compose broker/sandbox → host broker/Docker sandbox → boundary/reset/troubleshooting.

- [ ] **Step 1: Move the Compose walkthrough before the host walkthrough**

Rename and order the headings exactly:

```markdown
## Path A: compose broker + sandbox
## Path B: host broker + sandbox
```

Move the complete existing Compose section without changing bootstrap, readiness, policy-denial, Git, or audit commands. Preserve `gh repo view --repo owner/name`, the deliberate `gh run list` denial, and the requirement for both broker-side SSH authentication and verified known-hosts state.

- [ ] **Step 2: Replace host setup heredocs with the two installers**

The host section must begin with:

````markdown
Install RepoWolf on the Linux host per the top-level README. Run the setup as a
normal sudo-capable operator; the scripts invoke `sudo` only for protected
filesystem changes and refuse existing state.

```sh
cd examples/docker
REPOWOLF_REPO=rochecompaan/repowolf ./install-host-broker.sh
./install-host-principal.sh
```
````

Follow it with a compact override list naming `REPOWOLF_REPO`, `REPOWOLF_LISTEN`, `REPOWOLF_GH_PATH`, `REPOWOLF_SSH_PATH`, `REPOWOLF_BROKER_USER`, `REPOWOLF_BROKER_GROUP`, `REPOWOLF_CONFIG_DIR`, `REPOWOLF_STATE_DIR`, and `REPOWOLF_RUNTIME_DIR`. State that tool/directory overrides must be absolute; `REPOWOLF_LISTEN` defaults to the Docker bridge gateway on port `9443`; a missing bridge requires an explicit listen override; and neither script starts/restarts the broker.

Retain the supervisor requirement that both `/run/repowolf/service.env` and `/run/repowolf/example-agent.env` load before restart. Do not show token generation, certificate commands, a YAML heredoc, token contents, or provider credentials.

- [ ] **Step 3: Make the default client command use the detected gateway**

Use this exact prelude and endpoint while preserving CA/token handling and the supported repository syntax:

```bash
DOCKER_GATEWAY="$(docker network inspect \
  --format '{{(index .IPAM.Config 0).Gateway}}' bridge)"
docker run --rm -it \
  -e REPOWOLF_ENDPOINT="https://$DOCKER_GATEWAY:9443" \
  -e REPOWOLF_SERVER_NAME=repowolf.internal \
  -e REPOWOLF_TOKEN="$(sudo cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

State that a custom `REPOWOLF_LISTEN` requires the reachable matching host/port in `REPOWOLF_ENDPOINT`. Keep the DNS SAN explanation and host-only SSH prerequisite.

- [ ] **Step 4: Update reset and troubleshooting text**

Distinguish Compose `state/`/`.env` from host paths. Explain that rerunning a host installer requires the operator to back up and deliberately remove the exact conflicting TLS/config/token/principal environment path; provide no wildcard or automatic reset command. Add failures for missing Docker bridge, invalid relative overrides, existing protected state, and failure to load both supervisor environment files.

The Testing Value Gate excludes a new README-content test. Verify directly instead:

```bash
awk '/^## / { print NR ":" $0 }' examples/docker/README.md
grep -n 'install-host-broker.sh\|install-host-principal.sh' examples/docker/README.md
grep -n 'REPOWOLF_SERVER_NAME=repowolf.internal' examples/docker/README.md
git diff --check
GH_TOKEN=dummy REPOWOLF_TOKEN_AGENT=dummy \
  docker compose -f examples/docker/compose.yaml config --quiet
```

Expected: Path A Compose precedes Path B host; each installer appears in one concise setup block; Compose interpolation and YAML parsing exit zero.

- [ ] **Step 5: Commit the README refactor**

```bash
git add examples/docker/README.md
git commit -m "docs(examples): simplify docker host setup"
```

---

### Task 13: Final verification, review, and PR update

**Files:** none unless review finds a scoped defect.

- [ ] **Step 1: Run all focused shell and real-schema checks fresh**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
bash -n examples/docker/*.sh
(
  cd examples/docker
  ./test-wait-for-broker.sh
  ./test-bootstrap-unreadable-ssh.sh
  REPOWOLF_TEST_VALIDATE_IMAGE=repowolf:mvp ./test-install-host-broker.sh
  ./test-install-host-principal.sh
)
git diff --check
```

Expected: all syntax/tests/schema validation exit zero with no real host-install paths touched.

- [ ] **Step 2: Run repository regression checks directly**

```bash
go test -race ./...
GH_TOKEN=dummy REPOWOLF_TOKEN_AGENT=dummy \
  docker compose -f examples/docker/compose.yaml config --quiet
git check-ignore -q examples/docker/state/token
git check-ignore -q examples/docker/.env
git status --short
```

Expected: all Go packages pass; Compose config parses; both generated secret paths are ignored; status contains only intended committed branch state and no generated `state/`, `.env`, `dist/`, or test fixture.

- [ ] **Step 3: Request fresh adversarial review**

Resolve the canonical `reviewer` role if the resolver is available, then dispatch `reviewer` with `context: "fresh"`. Include: approved spec path, this plan path, base SHA `7c2f8b6`, current head SHA, implemented files, test evidence, and the original request to extract two host setup blocks and make Compose first. Ask specifically about sudo/no-clobber/cleanup safety, YAML quoting, token disclosure, test realism, README command consistency, and CI failure propagation.

Apply `superpowers:receiving-code-review` to every finding. Reproduce each valid issue, add or adjust a behavior test before production fixes when the Testing Value Gate passes, rerun focused and full checks, and commit only scoped fixes.

- [ ] **Step 4: Push the reviewed branch and watch PR checks**

```bash
git push origin feat/docker-example
gh pr checks 2 --repo rochecompaan/repowolf --watch
```

Expected: PR #2 reports all required checks passing. If a check fails, inspect the failing job logs, reproduce locally, and follow `superpowers:systematic-debugging` before changing code.

- [ ] **Step 5: Record the final handoff without releasing**

Report commits, focused/full verification evidence, review disposition, PR URL, and residual platform limits. Do not create or push `v0.1.0`; after merge, ask the owner separately whether to publish it. Keep or remove the feature worktree only through the approved branch-completion workflow.

---

## Plan self-review

- **Baseline coverage:** Tasks 1–8 retain the implemented checksum, sandbox boundary, Compose bootstrap/readiness, SSH, audit/process-side-effect, CI, README, and rollout work through `ce94d61`.
- **Follow-up coverage:** Task 9 implements the host policy/template, exact overrides, YAML-safe rendering, pre/post-install validation, no-clobber publication, narrow ownership/modes, refusal, and invocation-owned cleanup. Task 10 implements transactional principal/token installation, provider-environment separation, and non-disclosure. Task 11 runs behavior plus real schema validation in unconditional CI. Task 12 makes Compose first and removes both large host setup blocks. Task 13 runs fresh verification, adversarial review, and PR checks.
- **Testing Value Gate:** New tests exercise reusable shell behavior and security-sensitive filesystem/error handling. README/workflow text receives direct syntax/interpolation verification rather than static content tests.
- **Security:** Provider credentials and real SSH material remain outside the sandbox; host inputs are validated and YAML-quoted; privileged publication refuses replacement; cleanup is limited to invocation-owned paths; token output is never logged; no reset command removes broad host paths.
- **Consistency:** Compose retains fixed `state/`/`.env`; host-only path overrides are explicit and absolute; `repowolf.internal`, detected bridge gateway, `example-agent`, `REPOWOLF_TOKEN_EXAMPLE_AGENT`, and `--repo owner/name` are consistent across template, installers, tests, docs, and CI.
