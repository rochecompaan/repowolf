# RepoWolf Docker Example Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe, copy-pasteable Docker example with one restricted sandbox image, host-broker and compose run modes, readiness handling, service-side SSH support, documentation, and behavioral CI coverage.

**Architecture:** The sandbox image downloads the pinned client archive, verifies one filtered checksum with Alpine BusyBox, and contains no provider tools or credentials. `bootstrap.sh` uses fixed paths, strict `owner/name` validation, portable Bash rendering, and an Alpine permission helper so the UID 65532 broker can read only the TLS/SSH files it needs. Compose and CI assert stable broker audit events for granted/denied GitHub calls and a brokered Git upload-pack attempt.

**Tech Stack:** Docker/buildx, Docker Compose v2+, Bash 3.2-compatible syntax, Nix, goreleaser, GitHub Actions, Go (temporary local HTTP/YAML validators only).

## Global constraints

- Approved/revised spec: `docs/specs/2026-08-07-docker-example-design.md`.
- No RepoWolf Go-code or config-schema changes.
- Linux is the verified target. The compose path requires Docker **and Bash**; macOS needs a Unix shell, Windows needs WSL2. Do not claim native PowerShell/cmd support or Docker-only host tooling.
- Dockerfile public args: `REPOWOLF_VERSION` (default `v0.1.0`) and `REPOWOLF_RELEASE_ROOT` (default GitHub releases/download root).
- Release artifacts are **checksum-verified**, not signed.
- Fixed bootstrap paths only: `examples/docker/state` and `examples/docker/.env`; no disconnected `STATE_DIR`/`ENV_FILE` overrides.
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
    owner: __OWNER__
    name: __NAME__
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
  ! env | grep -q "^GH_TOKEN="
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

if [[ $REPOWOLF_REPO =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,37}[A-Za-z0-9])?)/([A-Za-z0-9._-]{1,100})$ ]]; then
  owner=${BASH_REMATCH[1]}
  name=${BASH_REMATCH[3]}
else
  echo "bootstrap: REPOWOLF_REPO must match owner/name using GitHub-safe characters" >&2
  exit 2
fi
if [ "$name" = "." ] || [ "$name" = ".." ]; then
  echo "bootstrap: repository name cannot be . or .." >&2
  exit 2
fi
if { [ -n "$REPOWOLF_SSH_KEY" ] && [ -z "$REPOWOLF_KNOWN_HOSTS" ]; } || \
   { [ -z "$REPOWOLF_SSH_KEY" ] && [ -n "$REPOWOLF_KNOWN_HOSTS" ]; }; then
  echo "bootstrap: set both REPOWOLF_SSH_KEY and REPOWOLF_KNOWN_HOSTS, or neither" >&2
  exit 2
fi
if [ -n "$REPOWOLF_SSH_KEY" ] && { [ ! -f "$REPOWOLF_SSH_KEY" ] || [ ! -f "$REPOWOLF_KNOWN_HOSTS" ]; }; then
  echo "bootstrap: SSH key and known-hosts inputs must be readable files" >&2
  exit 2
fi
if [ -e "$STATE_DIR" ]; then
  echo "bootstrap: $STATE_DIR already exists; back it up before resetting" >&2
  exit 1
fi

created_state=0
env_tmp="${ENV_FILE}.tmp.$$"
cleanup_failed_bootstrap() {
  status=$?
  trap - EXIT INT TERM
  rm -f "$env_tmp"
  if [ "$status" -ne 0 ] && [ "$created_state" -eq 1 ]; then
    rm -rf "$STATE_DIR" # only partial state created by this invocation
  fi
  exit "$status"
}
trap cleanup_failed_bootstrap EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$STATE_DIR"
chmod 0700 "$STATE_DIR"
created_state=1

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

rw_as_host config validate --config /state/config.yaml >/dev/null

: > "$env_tmp"
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
chmod 0600 "$env_tmp"
mv "$env_tmp" "$ENV_FILE"

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
if [[ ! $port =~ ^[0-9]+$ ]] || [ "$port" -eq 0 ] || [ "$port" -gt 65535 ] || \
   [[ ! $attempts =~ ^[1-9][0-9]*$ ]] || [ "$attempts" -gt 300 ]; then
  echo "usage: wait-for-broker.sh [host] [port] [attempts]" >&2
  exit 2
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
  if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "wait-for-broker: $host:$port not ready after $attempts attempts" >&2
exit 1
```

```bash
chmod +x examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh
```

- [ ] **Step 3: Run shell checks without masking findings**

```bash
bash -n examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh
else
  echo "shellcheck not installed; skipped"
fi
```

Expected: `bash -n` exit 0. If shellcheck exists, findings fail the step; only absence prints `skipped`.

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
cd ../..
```

Expected: each call exits 2; no `state/` created and no injected command output.

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
docker run --rm -v "$PWD/state/config.yaml:/config.yaml:ro" repowolf:mvp config validate --config /config.yaml
ssh_effective=$(mktemp /tmp/repowolf-ssh-effective.XXXXXX)
docker run --rm -v "$PWD/state/ssh:/tmp/.ssh:ro" --entrypoint ssh repowolf:mvp -G github.com > "$ssh_effective"
grep -E '^identityagent[[:space:]]+none$' "$ssh_effective"
grep -E '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' "$ssh_effective"
grep -E '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' "$ssh_effective"
rm "$ssh_effective"
grep -q '^REPOWOLF_TOKEN_AGENT=.\+' .env
git check-ignore -q state .env
```

Expected: all checks pass; the actual OCI user validates `config.yaml`, can read `tls.key`, and accepts the root-owned OpenSSH config with the intended effective values. `ca.key` remains host-private.

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
git add examples/docker/bootstrap.sh examples/docker/wait-for-broker.sh
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
docker run --rm -v "$PWD/state/config.yaml:/config.yaml:ro" repowolf:mvp config validate --config /config.yaml
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
if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh repo view --repo rochecompaan/repowolf; then
  echo "expected upstream failure with dummy GH_TOKEN" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep 'github.repository_view' | grep -E '"outcome":[[:space:]]*"accepted"'

if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh run list --repo rochecompaan/repowolf; then
  echo "expected policy denial" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep -E '"outcome":[[:space:]]*"denied"' | grep 'PermissionDenied'
if docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep 'github.run_list' | grep -qE '"outcome":[[:space:]]*"accepted"'; then
  echo "github.run_list must not be accepted" >&2
  exit 1
fi
```

- [ ] **Step 7: Prove broker launched SSH with the exact upload-pack argv**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
if REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git; then
  echo "fake SSH unexpectedly succeeded" >&2
  exit 1
fi
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf \
  | grep 'git.upload-pack' | grep -E '"outcome":[[:space:]]*"accepted"'
expected_argv=$(cat <<'EOF'
-T
-p
22
--
git@github.com
git-upload-pack 'rochecompaan/repowolf.git'
EOF
)
test "$(cat state/test/ssh-argv)" = "$expected_argv"
docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf \
  | grep 'git.upload-pack' | grep -E '"outcome":[[:space:]]*"failed"' \
  | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
```

Expected: fake SSH exits 42; its host-visible log exactly proves `Runner.Start` executed the configured provider process with the required upload-pack argv. Audit acceptance is a secondary policy assertion, not the process-launch proof.

- [ ] **Step 8: Assert sandbox boundary**

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example/examples/docker
REPOWOLF_IMAGE=repowolf:mvp docker compose -f compose.yaml -f compose.smoke.yaml run --rm --entrypoint sh sandbox -c '
  set -e
  ! env | grep -q "^GH_TOKEN="
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
        working-directory: examples/docker
    env:
      REPOWOLF_IMAGE: repowolf:mvp
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
      - uses: cachix/install-nix-action@b4b293eae0b79aac8a161bb32925a5508c9cca93 # v31
      - name: Check example shell syntax
        run: bash -n bootstrap.sh wait-for-broker.sh
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
          docker run --rm -v "$PWD/state/config.yaml:/config.yaml:ro" \
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
          docker run --rm -v "$PWD/examples/docker/state/config.yaml:/config.yaml:ro" \
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
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep 'github.repository_view' | grep -E '"outcome":[[:space:]]*"accepted"'
          if docker compose -f compose.yaml -f compose.smoke.yaml run --rm sandbox gh run list --repo rochecompaan/repowolf; then
            echo "expected policy denial" >&2
            exit 1
          fi
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep -E '"outcome":[[:space:]]*"denied"' | grep 'PermissionDenied'
          if docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf | grep 'github.run_list' | grep -qE '"outcome":[[:space:]]*"accepted"'; then
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
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf \
            | grep 'git.upload-pack' | grep -E '"outcome":[[:space:]]*"accepted"'
          expected_argv=$(cat <<'EOF'
          -T
          -p
          22
          --
          git@github.com
          git-upload-pack 'rochecompaan/repowolf.git'
          EOF
          )
          test "$(cat state/test/ssh-argv)" = "$expected_argv"
          docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf \
            | grep 'git.upload-pack' | grep -E '"outcome":[[:space:]]*"failed"' \
            | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
      - name: Assert sandbox boundary
        run: |
          docker compose -f compose.yaml -f compose.smoke.yaml run --rm --entrypoint sh sandbox -c '
            set -e
            ! env | grep -q "^GH_TOKEN="
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
- Linux is tested. On macOS run from a Unix shell. On Windows run from WSL2;
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

Install/bootstrap RepoWolf on the Linux host per the top-level README. Bind
it to the Docker bridge gateway (usually `172.17.0.1`), not `0.0.0.0`:

```yaml
listen: 172.17.0.1:9443
tools:
  gh: /absolute/path/from/command-v-gh
  ssh: /absolute/path/from/command-v-ssh
```

The absolute paths avoid startup failures from ambiguous/empty PATH entries.
Restart the broker, then:

```sh
docker run --rm -it \
  -e REPOWOLF_ENDPOINT=https://172.17.0.1:9443 \
  -e REPOWOLF_SERVER_NAME=localhost \
  -e REPOWOLF_TOKEN="$(cat /var/lib/repowolf/token)" \
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
docker compose logs repowolf | grep -E '"outcome":[[:space:]]*"denied"'
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
  ! env | grep -q "^GH_TOKEN="
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

## Plan self-review

- **Finding coverage:** BusyBox checksum (Task 2), broker-readable config/TLS permissions and OpenSSH-valid ownership (Task 3), sed injection (Task 3), platform scope/no `sed -i` (Tasks 3/6), Git auth+known-hosts (Tasks 3/6), removed path overrides (Task 3), broker/fixture readiness (Tasks 2–5), supported `--repo` and audit assertions (Tasks 4–6), observable fake-SSH process launch + exact upload-pack argv (Tasks 4–5), shellcheck status (Task 3), direct Go test (Task 8), no PyYAML (Task 5), matching `REPOWOLF_VERSION` contract (Tasks 2/4/5/6), checksum-not-signature wording (Task 6), accurate gitignore wording (Tasks 1/6), cleanup traps (Tasks 2/5).
- **Security:** CA key never mounted; private TLS/SSH files and broker config have verified narrow UID/GID modes; raw repository input is validated and never inserted into code; the fake-SSH fixture is broker-only; reset/destructive operations are explicit.
- **Consistency:** fixed `state/`/`.env`, service `repowolf`, image `repowolf-sandbox:local`, args `REPOWOLF_VERSION`/`REPOWOLF_RELEASE_ROOT`, and audit operations are identical across all tasks.
