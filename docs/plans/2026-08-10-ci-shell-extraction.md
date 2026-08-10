# CI Shell Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the six substantial inline Docker-smoke shell programs with focused, locally runnable, linted scripts while continuing to test the documented Docker example interfaces.

**Architecture:** Keep one primary script per existing qualifying GitHub Actions step under `scripts/ci/docker-example/`. Move embedded Go and expected-output fixtures into normal source files, reuse `examples/docker/bootstrap.sh`, `wait-for-broker.sh`, and Compose only through their executable/declarative interfaces, and add a pinned ShellCheck entry point without creating a sourced cross-boundary shell library.

**Tech Stack:** Bash 3.2-compatible host scripts where example compatibility applies, POSIX shell for the sandbox boundary, ShellCheck, Nix flakes, devenv, Go fixture binaries, Docker, Docker Compose, GitHub Actions, Ruby YAML parser, and existing Go/Nix test suites.

**Design:** `docs/specs/2026-08-10-ci-shell-extraction-design.md`

## Global Constraints

- Work only in `/home/roche/projects/pi/repowolf/.worktrees/docker-example` on `feat/docker-example`.
- Extract only inline shell programs containing more than five semantic shell commands.
- Count a command split across physical lines once; count commands inside nested shell programs; do not count heredoc content written in another language as shell commands.
- Preserve the six current GitHub Actions step names, ordering, logs, and failure boundaries.
- Leave multiline blocks containing at most five semantic commands inline.
- Put CI-only scripts under `scripts/ci/docker-example/`; each primary script must locate the repository from its own path.
- Continue invoking `examples/docker/bootstrap.sh`, `examples/docker/wait-for-broker.sh`, Compose definitions, and installer behavior tests through their existing interfaces.
- Do not create a sourced shell library shared by CI and the user-facing example.
- Add ShellCheck to both `flake.nix` and `devenv.nix`; lint all new CI shell scripts plus `bootstrap.sh` and `wait-for-broker.sh`.
- Preserve credential boundaries, audit assertions, expected-failure semantics, non-disclosure, bounded readiness, and always-run Compose teardown.
- Do not change RepoWolf production Go behavior or configuration schemas.
- Apply the Testing Value Gate: do not add tests that merely assert workflow YAML, dependency-list text, command counts, file names, or script structure. Verify those with syntax parsing, linting, direct inspection, and the real smoke job.
- The documented platform boundary remains Linux verified, macOS through a Unix shell, and Windows through WSL2 only.
- Do not create or publish a `v0.1.0` tag or release.
- End each implementation task with a fresh reviewer gate and commit only the files named by that task.

---

### Task 1: Pin ShellCheck in the development toolsets

**Files:**
- Modify: `flake.nix:35-41`
- Modify: `devenv.nix:7-15`

**Interfaces:**
- Consumes: the existing pinned `nixpkgs` flake input and devenv `pkgs` argument.
- Produces: `shellcheck` on `PATH` under both `nix develop` and `devenv shell`; later tasks invoke the exact executable name `shellcheck`.

- [ ] **Step 1: Confirm the pinned flake shell does not yet provide ShellCheck**

Run:

```bash
cd /home/roche/projects/pi/repowolf/.worktrees/docker-example
if nix develop -c sh -c 'command -v shellcheck'; then
  echo "expected shellcheck to be absent before Task 1" >&2
  exit 1
fi
```

Expected: the command succeeds as a negative characterization check after printing no ShellCheck path.

- [ ] **Step 2: Add ShellCheck to the flake development shell**

Change the package list to:

```nix
packages = with pkgs; [ go goreleaser shellcheck skopeo ];
```

- [ ] **Step 3: Add ShellCheck to devenv**

Insert `pkgs.shellcheck` with the existing developer tools:

```nix
packages = [
  pkgs.go
  pkgs.goreleaser
  pkgs.shellcheck
  pkgs.skopeo
  repowolfPackages.repowolf
  repowolfPackages."repowolf-client"
];
```

- [ ] **Step 4: Verify both environments resolve ShellCheck**

Run:

```bash
nix develop -c shellcheck --version
devenv shell shellcheck --version
nix eval .#devShells.x86_64-linux.default.drvPath >/dev/null
```

Expected: both version commands identify ShellCheck and the flake dev shell evaluates successfully. This is direct dependency/configuration verification; do not add a test that asserts the package-list text or version.

- [ ] **Step 5: Review and commit**

Run:

```bash
git diff --check
git diff -- flake.nix devenv.nix
git add flake.nix devenv.nix
git commit -m "build(ci): add ShellCheck tooling"
```

Expected: one commit containing only the two toolset declarations.

---

### Task 2: Extract the snapshot sandbox-image build

**Files:**
- Create: `scripts/ci/docker-example/build-sandbox-image.sh`
- Create: `scripts/ci/docker-example/fixtures/release-server/main.go`
- Modify: `.github/workflows/ci.yml:74-124`

**Interfaces:**
- Consumes: repository-root `.goreleaser.yaml`, `examples/docker/sandbox/Dockerfile`, the pinned Nix development shell, Docker, and localhost TCP port `8765`.
- Produces: Docker image `repowolf-sandbox:local`; all release-server processes, temporary directories, and logs are removed before the script exits.

- [ ] **Step 1: Create the release fixture as normal Go source**

Create `scripts/ci/docker-example/fixtures/release-server/main.go`:

```go
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
```

Run:

```bash
gofmt -w scripts/ci/docker-example/fixtures/release-server/main.go
nix develop -c go test ./scripts/ci/docker-example/fixtures/release-server
```

Expected: the standalone fixture package compiles and reports no test files.

- [ ] **Step 2: Create the image-build script with owned cleanup**

Create `scripts/ci/docker-example/build-sandbox-image.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
fixture=
server_pid=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  if [ -n "$server_pid" ]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  if [ -n "$fixture" ]; then
    rm -rf -- "$fixture"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$REPO_ROOT"
nix develop -c goreleaser release --snapshot --clean
fixture=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-release-fixture.XXXXXX")
mkdir -p -- "$fixture/snapshot"
cp dist/checksums.txt dist/repowolf_linux_*.tar.gz "$fixture/snapshot/"
nix develop -c go build -o "$fixture/release-server" \
  "$SCRIPT_DIR/fixtures/release-server"
"$fixture/release-server" "$fixture" >"$fixture/server.log" 2>&1 &
server_pid=$!

fixture_ready=0
for ((attempt = 1; attempt <= 30; attempt++)); do
  if (exec 3<>/dev/tcp/127.0.0.1/8765) 2>/dev/null; then
    fixture_ready=1
    break
  fi
  sleep 1
done
if [ "$fixture_ready" -ne 1 ]; then
  cat -- "$fixture/server.log" >&2
  exit 1
fi

docker build --network=host \
  --build-arg REPOWOLF_VERSION=snapshot \
  --build-arg REPOWOLF_RELEASE_ROOT=http://127.0.0.1:8765 \
  -t repowolf-sandbox:local examples/docker/sandbox
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/build-sandbox-image.sh
```

- [ ] **Step 3: Run syntax, lint, and the extracted behavior**

Run:

```bash
tmpdir=$(mktemp -d)
trap 'rm -rf -- "$tmpdir"' EXIT
bash -n scripts/ci/docker-example/build-sandbox-image.sh
nix develop -c shellcheck scripts/ci/docker-example/build-sandbox-image.sh
TMPDIR="$tmpdir" scripts/ci/docker-example/build-sandbox-image.sh
docker image inspect repowolf-sandbox:local >/dev/null
test -z "$(find "$tmpdir" -mindepth 1 -print -quit)"
rm -rf -- "$tmpdir"
trap - EXIT
```

Expected: syntax and ShellCheck pass, the local sandbox image exists, and no release fixture remains. `dist/` may remain as an ignored goreleaser output and must not be staged.

- [ ] **Step 4: Replace only the matching workflow block**

Keep the existing step name and root working directory:

```yaml
      - name: Build sandbox image from current-commit archives
        working-directory: .
        run: scripts/ci/docker-example/build-sandbox-image.sh
```

Do not change any shorter `run:` block.

- [ ] **Step 5: Parse and inspect the workflow**

Run:

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git diff -- .github/workflows/ci.yml scripts/ci/docker-example/build-sandbox-image.sh \
  scripts/ci/docker-example/fixtures/release-server/main.go
```

Expected: YAML parsing succeeds and the diff is a move from one named step into one script plus one Go fixture.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/ci.yml \
  scripts/ci/docker-example/build-sandbox-image.sh \
  scripts/ci/docker-example/fixtures/release-server/main.go
git commit -m "refactor(ci): extract sandbox image build"
```

---

### Task 3: Extract disposable bootstrap and broker checks

**Files:**
- Create: `scripts/ci/docker-example/bootstrap-disposable-state.sh`
- Modify: `.github/workflows/ci.yml:125-142`

**Interfaces:**
- Consumes: loaded broker image from the workflow environment variable `REPOWOLF_IMAGE` (default `repowolf:mvp`), `examples/docker/bootstrap.sh`, Docker, and `ssh-keygen`.
- Produces: ignored `examples/docker/.env` and `examples/docker/state/` for later smoke steps; temporary SSH input and effective-config output are always removed.

- [ ] **Step 1: Create the bootstrap orchestration script**

Create `scripts/ci/docker-example/bootstrap-disposable-state.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
umask 077

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
REPOWOLF_IMAGE=${REPOWOLF_IMAGE:-repowolf:mvp}
tmp_root=${TMPDIR:-/tmp}
ssh_test=$(mktemp -d "$tmp_root/repowolf-ci-ssh.XXXXXX")
ssh_effective=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$ssh_test"
  if [ -n "$ssh_effective" ]; then
    rm -f -- "$ssh_effective"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

ssh_effective=$(mktemp "$tmp_root/repowolf-ssh-effective.XXXXXX")
ssh-keygen -q -t ed25519 -N '' -f "$ssh_test/id_ed25519"
printf 'github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestOnly\n' \
  >"$ssh_test/known_hosts"
printf 'GH_TOKEN=dummy-ci-token\n' >"$EXAMPLE_DIR/.env"
(
  cd -- "$EXAMPLE_DIR"
  REPOWOLF_IMAGE="$REPOWOLF_IMAGE" \
  REPOWOLF_REPO=rochecompaan/repowolf \
  REPOWOLF_SSH_KEY="$ssh_test/id_ed25519" \
  REPOWOLF_KNOWN_HOSTS="$ssh_test/known_hosts" \
    ./bootstrap.sh
)

docker run --rm --user 65532:65532 \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml:ro" \
  "$REPOWOLF_IMAGE" config validate --config /config.yaml
docker run --rm \
  -v "$EXAMPLE_DIR/state/ssh:/tmp/.ssh:ro" \
  --entrypoint ssh "$REPOWOLF_IMAGE" -G github.com >"$ssh_effective"
grep -E '^identityagent[[:space:]]+none$' "$ssh_effective"
grep -E '^identityfile[[:space:]]+/tmp/.ssh/id_ed25519$' "$ssh_effective"
grep -E '^userknownhostsfile[[:space:]]+/tmp/.ssh/known_hosts$' "$ssh_effective"
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/bootstrap-disposable-state.sh
```

- [ ] **Step 2: Verify syntax, lint, cleanup, and generated state**

Require an unused example state path, then run the script. Stop instead of deleting an existing ignored `.env` or `state/` because either could contain operator credentials:

```bash
test ! -e examples/docker/state
test ! -e examples/docker/.env
tmpdir=$(mktemp -d)
trap 'rm -rf -- "$tmpdir"' EXIT
bash -n scripts/ci/docker-example/bootstrap-disposable-state.sh
nix develop -c shellcheck scripts/ci/docker-example/bootstrap-disposable-state.sh \
  examples/docker/bootstrap.sh
TMPDIR="$tmpdir" REPOWOLF_IMAGE=repowolf:mvp \
  scripts/ci/docker-example/bootstrap-disposable-state.sh
test -f examples/docker/state/config.yaml
test -f examples/docker/state/ssh/id_ed25519
test -f examples/docker/.env
test -z "$(find "$tmpdir" -mindepth 1 -print -quit)"
rm -rf -- "$tmpdir"
trap - EXIT
```

Expected: checks pass, required persistent smoke state exists, and script-owned temporary files are absent. Confirm `git status --short` does not show `.env`, `state/`, or private-key artifacts.

- [ ] **Step 3: Replace only the matching workflow block**

```yaml
      - name: Bootstrap disposable state and verify broker ownership
        working-directory: .
        run: scripts/ci/docker-example/bootstrap-disposable-state.sh
```

- [ ] **Step 4: Parse, inspect, and commit**

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git add .github/workflows/ci.yml scripts/ci/docker-example/bootstrap-disposable-state.sh
git commit -m "refactor(ci): extract Docker bootstrap checks"
```

---

### Task 4: Extract the observable fake-SSH fixture setup

**Files:**
- Create: `scripts/ci/docker-example/install-fake-ssh-fixture.sh`
- Create: `scripts/ci/docker-example/fixtures/fake-ssh/main.go`
- Modify: `.github/workflows/ci.yml:143-196`

**Interfaces:**
- Consumes: generated state from Task 3, loaded broker image `REPOWOLF_IMAGE` (default `repowolf:mvp`), the pinned Go toolchain, Docker, and Alpine image `alpine:3`.
- Produces: `examples/docker/state/test/fake-ssh`, writable `ssh-argv`, and a validated smoke broker config that points `tools.ssh` at `/run/repowolf/test/fake-ssh`.

- [ ] **Step 1: Create the fake-SSH Go fixture**

Create `scripts/ci/docker-example/fixtures/fake-ssh/main.go`:

```go
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
```

Run:

```bash
gofmt -w scripts/ci/docker-example/fixtures/fake-ssh/main.go
nix develop -c go test ./scripts/ci/docker-example/fixtures/fake-ssh
```

Expected: the standalone fixture package compiles.

- [ ] **Step 2: Create the fixture-install script**

Create `scripts/ci/docker-example/install-fake-ssh-fixture.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
TEST_DIR="$EXAMPLE_DIR/state/test"
REPOWOLF_IMAGE=${REPOWOLF_IMAGE:-repowolf:mvp}
rendered_config=

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  if [ -n "$rendered_config" ]; then
    rm -f -- "$rendered_config"
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$REPO_ROOT"
mkdir -p -- "$TEST_DIR"
nix develop -c env CGO_ENABLED=0 go build \
  -o "$TEST_DIR/fake-ssh" "$SCRIPT_DIR/fixtures/fake-ssh"
: >"$TEST_DIR/ssh-argv"
rendered_config=$(mktemp "$EXAMPLE_DIR/state/config.smoke.XXXXXX")
awk '
  $0 == "  ssh: null" { print "  ssh: /run/repowolf/test/fake-ssh"; next }
  { print }
' "$EXAMPLE_DIR/state/config.yaml" >"$rendered_config"
mv -- "$rendered_config" "$EXAMPLE_DIR/state/config.yaml"
rendered_config=

# Expanded by the Alpine shell inside the container, not by this host shell.
# shellcheck disable=SC2016
docker run --rm --user 0:0 \
  -e HOST_UID="$(id -u)" \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml" \
  -v "$TEST_DIR:/test" \
  alpine:3 sh -eu -c '
    chown "$HOST_UID:65532" /config.yaml /test /test/fake-ssh /test/ssh-argv
    chmod 0640 /config.yaml
    chmod 0750 /test
    chmod 0550 /test/fake-ssh
    chmod 0660 /test/ssh-argv
  '
docker run --rm --user 65532:65532 \
  -v "$EXAMPLE_DIR/state/config.yaml:/config.yaml:ro" \
  "$REPOWOLF_IMAGE" config validate --config /config.yaml
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/install-fake-ssh-fixture.sh
```

- [ ] **Step 3: Verify the extracted setup**

Run after Task 3 state exists:

```bash
bash -n scripts/ci/docker-example/install-fake-ssh-fixture.sh
nix develop -c shellcheck scripts/ci/docker-example/install-fake-ssh-fixture.sh
REPOWOLF_IMAGE=repowolf:mvp scripts/ci/docker-example/install-fake-ssh-fixture.sh
test -x examples/docker/state/test/fake-ssh
test -f examples/docker/state/test/ssh-argv
grep -q '^  ssh: /run/repowolf/test/fake-ssh$' examples/docker/state/config.yaml
test -z "$(find examples/docker/state -maxdepth 1 -name 'config.smoke.*' -print -quit)"
```

Expected: fixture/config assertions pass and no render temporary remains.

- [ ] **Step 4: Replace the workflow block and commit**

Use:

```yaml
      - name: Install observable fake SSH fixture
        working-directory: .
        run: scripts/ci/docker-example/install-fake-ssh-fixture.sh
```

Then run:

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git add .github/workflows/ci.yml \
  scripts/ci/docker-example/install-fake-ssh-fixture.sh \
  scripts/ci/docker-example/fixtures/fake-ssh/main.go
git commit -m "refactor(ci): extract fake SSH fixture setup"
```

---

### Task 5: Extract GitHub policy assertions

**Files:**
- Create: `scripts/ci/docker-example/assert-github-policy.sh`
- Modify: `.github/workflows/ci.yml:204-223`

**Interfaces:**
- Consumes: running broker/sandbox Compose topology, dummy provider token, and broker audit logs.
- Produces: a passing check only when repository view is accepted then fails upstream, run listing is denied at `/repowolf.v1.GitHubService/Execute` with `PermissionDenied`, and no `github.run_list` operation is accepted; temporary broker log is removed.

- [ ] **Step 1: Create the policy assertion script**

Create `scripts/ci/docker-example/assert-github-policy.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
broker_log=$(mktemp "${TMPDIR:-/tmp}/repowolf-broker.XXXXXX.log")
compose=(docker compose -f compose.yaml -f compose.smoke.yaml)

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -f -- "$broker_log"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$EXAMPLE_DIR"
if "${compose[@]}" run --rm sandbox gh repo view --repo rochecompaan/repowolf; then
  echo "expected upstream failure with dummy GH_TOKEN" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep 'github.repository_view' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"accepted"'

if "${compose[@]}" run --rm sandbox gh run list --repo rochecompaan/repowolf; then
  echo "expected policy denial" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep -E '"operation":[[:space:]]*"/repowolf\.v1\.GitHubService/Execute"' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"denied"' \
  | grep -E '"reason":[[:space:]]*"PermissionDenied"'
if grep 'github.run_list' "$broker_log" \
  | grep -qE '"outcome":[[:space:]]*"accepted"'; then
  echo "github.run_list must not be accepted" >&2
  exit 1
fi
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/assert-github-policy.sh
```

- [ ] **Step 2: Verify syntax and lint**

```bash
bash -n scripts/ci/docker-example/assert-github-policy.sh
nix develop -c shellcheck scripts/ci/docker-example/assert-github-policy.sh
```

Expected: both checks pass without broad ShellCheck suppression.

- [ ] **Step 3: Run the behavioral assertion against the prepared topology**

Ensure Tasks 2-4 have produced images/state, then run:

```bash
cd examples/docker
docker compose -f compose.yaml -f compose.smoke.yaml up -d repowolf
if ! ./wait-for-broker.sh 127.0.0.1 8443 30; then
  docker compose -f compose.yaml -f compose.smoke.yaml logs repowolf
  exit 1
fi
cd ../..
tmpdir=$(mktemp -d)
trap 'rm -rf -- "$tmpdir"' EXIT
TMPDIR="$tmpdir" scripts/ci/docker-example/assert-github-policy.sh
test -z "$(find "$tmpdir" -mindepth 1 -print -quit)"
rm -rf -- "$tmpdir"
trap - EXIT
```

Expected: the repository-view command fails upstream, the run-list command is denied, audit assertions pass, and the temporary log is absent.

- [ ] **Step 4: Replace the workflow block and commit**

```yaml
      - name: Assert GitHub policy outcomes
        working-directory: .
        run: scripts/ci/docker-example/assert-github-policy.sh
```

Run:

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git add .github/workflows/ci.yml scripts/ci/docker-example/assert-github-policy.sh
git commit -m "refactor(ci): extract GitHub policy assertions"
```

---

### Task 6: Extract fake-SSH execution assertions

**Files:**
- Create: `scripts/ci/docker-example/assert-fake-ssh.sh`
- Create: `scripts/ci/docker-example/fixtures/expected-ssh-argv.txt`
- Modify: `.github/workflows/ci.yml:224-243`

**Interfaces:**
- Consumes: running topology from Task 5, fake SSH binary/log from Task 4, and broker audit logs.
- Produces: a passing check only when the broker launches the fixture with the expected upload-pack argv and records accepted then provider-failed `git.upload-pack`; temporary broker log is removed.

- [ ] **Step 1: Create the expected argv fixture**

Create `scripts/ci/docker-example/fixtures/expected-ssh-argv.txt` with a trailing newline:

```text
-T
-p
22
--
git@github.com
git-upload-pack 'rochecompaan/repowolf.git'
```

- [ ] **Step 2: Create the fake-SSH assertion script**

Create `scripts/ci/docker-example/assert-fake-ssh.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
broker_log=$(mktemp "${TMPDIR:-/tmp}/repowolf-broker.XXXXXX.log")
compose=(docker compose -f compose.yaml -f compose.smoke.yaml)

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -f -- "$broker_log"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd -- "$EXAMPLE_DIR"
if "${compose[@]}" run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git; then
  echo "fake SSH unexpectedly succeeded" >&2
  exit 1
fi
"${compose[@]}" logs repowolf >"$broker_log"
grep 'git.upload-pack' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"accepted"'
diff -u "$SCRIPT_DIR/fixtures/expected-ssh-argv.txt" state/test/ssh-argv
grep 'git.upload-pack' "$broker_log" \
  | grep -E '"outcome":[[:space:]]*"failed"' \
  | grep 'GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE'
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/assert-fake-ssh.sh
```

- [ ] **Step 3: Run syntax, lint, and behavior checks**

```bash
bash -n scripts/ci/docker-example/assert-fake-ssh.sh
nix develop -c shellcheck scripts/ci/docker-example/assert-fake-ssh.sh
tmpdir=$(mktemp -d)
trap 'rm -rf -- "$tmpdir"' EXIT
TMPDIR="$tmpdir" scripts/ci/docker-example/assert-fake-ssh.sh
diff -u scripts/ci/docker-example/fixtures/expected-ssh-argv.txt \
  examples/docker/state/test/ssh-argv
test -z "$(find "$tmpdir" -mindepth 1 -print -quit)"
rm -rf -- "$tmpdir"
trap - EXIT
```

Expected: fake SSH exits with its expected provider failure, argv and audit checks pass, and no temporary broker log remains.

- [ ] **Step 4: Replace the workflow block and commit**

```yaml
      - name: Assert broker launched fake SSH with upload-pack argv
        working-directory: .
        run: scripts/ci/docker-example/assert-fake-ssh.sh
```

Run:

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git add .github/workflows/ci.yml \
  scripts/ci/docker-example/assert-fake-ssh.sh \
  scripts/ci/docker-example/fixtures/expected-ssh-argv.txt
git commit -m "refactor(ci): extract fake SSH assertions"
```

---

### Task 7: Extract the dual-mode sandbox-boundary check

**Files:**
- Create: `scripts/ci/docker-example/assert-sandbox-boundary.sh`
- Modify: `.github/workflows/ci.yml:244-253`

**Interfaces:**
- Consumes: built sandbox image and Compose configuration.
- Produces: a passing check only when the sandbox has no `GH_TOKEN`, both tool shims target `repowolf-client`, real SSH is absent, and UID is `65532`.

- [ ] **Step 1: Create one POSIX script with host and inner modes**

Create `scripts/ci/docker-example/assert-sandbox-boundary.sh`:

```sh
#!/bin/sh
set -eu

if [ "${1-}" = "--inside" ]; then
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh >/dev/null
  test "$(id -u)" = "65532"
  exit 0
fi
if [ "$#" -ne 0 ]; then
  echo "usage: assert-sandbox-boundary.sh" >&2
  exit 2
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
SCRIPT_PATH="$SCRIPT_DIR/assert-sandbox-boundary.sh"
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/../../.." && pwd)
EXAMPLE_DIR="$REPO_ROOT/examples/docker"
cd -- "$EXAMPLE_DIR"
docker compose -f compose.yaml -f compose.smoke.yaml run --rm -T \
  --entrypoint sh sandbox -s -- --inside <"$SCRIPT_PATH"
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/assert-sandbox-boundary.sh
```

- [ ] **Step 2: Verify both parser and linter dialects**

Run:

```bash
sh -n scripts/ci/docker-example/assert-sandbox-boundary.sh
nix develop -c shellcheck -s sh scripts/ci/docker-example/assert-sandbox-boundary.sh
```

Expected: POSIX syntax and ShellCheck pass.

- [ ] **Step 3: Verify usage and container behavior**

```bash
set +e
scripts/ci/docker-example/assert-sandbox-boundary.sh unexpected
rc=$?
set -e
test "$rc" -eq 2
scripts/ci/docker-example/assert-sandbox-boundary.sh
```

Expected: unsupported arguments return `2`; host mode streams the same linted script into the sandbox and all boundary assertions pass.

- [ ] **Step 4: Replace the workflow block and commit**

```yaml
      - name: Assert sandbox boundary
        working-directory: .
        run: scripts/ci/docker-example/assert-sandbox-boundary.sh
```

Run:

```bash
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
git diff --check
git add .github/workflows/ci.yml scripts/ci/docker-example/assert-sandbox-boundary.sh
git commit -m "refactor(ci): extract sandbox boundary check"
```

---

### Task 8: Add the focused shell lint entry point and complete local verification

**Files:**
- Create: `scripts/ci/docker-example/lint.sh`
- Modify: `.github/workflows/ci.yml:55-64`
- Verify only: all files changed in Tasks 1-7 and existing Docker example behavior scripts

**Interfaces:**
- Consumes: ShellCheck from Task 1 and all shell scripts from Tasks 2-7.
- Produces: one local/CI command, `nix develop -c scripts/ci/docker-example/lint.sh`, that syntax-checks and lints every extracted CI shell script plus `bootstrap.sh` and `wait-for-broker.sh`.

- [ ] **Step 1: Create the explicit lint entry point**

Create `scripts/ci/docker-example/lint.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
bash_scripts=(
  "$SCRIPT_DIR/lint.sh"
  "$SCRIPT_DIR/build-sandbox-image.sh"
  "$SCRIPT_DIR/bootstrap-disposable-state.sh"
  "$SCRIPT_DIR/install-fake-ssh-fixture.sh"
  "$SCRIPT_DIR/assert-github-policy.sh"
  "$SCRIPT_DIR/assert-fake-ssh.sh"
  "$REPO_ROOT/examples/docker/bootstrap.sh"
  "$REPO_ROOT/examples/docker/wait-for-broker.sh"
)
posix_script="$SCRIPT_DIR/assert-sandbox-boundary.sh"

bash -n "${bash_scripts[@]}"
sh -n "$posix_script"
shellcheck "${bash_scripts[@]}" "$posix_script"
```

Make it executable:

```bash
chmod 0755 scripts/ci/docker-example/lint.sh
```

- [ ] **Step 2: Add the workflow lint step**

Immediately after checkout and Nix installation in `docker-example-smoke`, add:

```yaml
      - name: Lint Docker example shell
        working-directory: .
        run: nix develop -c scripts/ci/docker-example/lint.sh
```

Keep the existing `Check example shell syntax and behavior` step unchanged so the remaining installer/test scripts retain `bash -n` and behavioral execution.

- [ ] **Step 3: Run focused lint and fixture checks**

```bash
nix develop -c scripts/ci/docker-example/lint.sh
test -z "$(gofmt -l scripts/ci/docker-example/fixtures)"
go test ./scripts/ci/docker-example/fixtures/...
go vet ./scripts/ci/docker-example/fixtures/...
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
```

Expected: all shell, Go fixture, and YAML checks pass.

- [ ] **Step 4: Run existing example behavior tests**

```bash
cd examples/docker
bash -n bootstrap.sh install-host-broker.sh install-host-principal.sh \
  wait-for-broker.sh test-bootstrap-unreadable-ssh.sh \
  test-install-host-broker.sh test-install-host-principal.sh \
  test-wait-for-broker.sh
./test-wait-for-broker.sh
./test-bootstrap-unreadable-ssh.sh
cd ../..
```

Expected: syntax and both focused behavior tests pass. No new automated workflow-structure test is added.

- [ ] **Step 5: Run the full Docker-example path from clean generated state**

Run from the repository root:

```bash
set -euo pipefail
export REPOWOLF_IMAGE=repowolf:mvp
export REPOWOLF_SANDBOX_IMAGE=repowolf-sandbox:local
docker compose -f examples/docker/compose.yaml \
  -f examples/docker/compose.smoke.yaml down -v || true
backup=$(mktemp -d "$PWD/.superpowers/sdd/2026-08-10-ci-shell-extraction/task-8-backup.XXXXXX")
cleanup() {
  rc=$?
  trap - EXIT INT TERM
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml down -v || true
  rm -rf -- examples/docker/state examples/docker/.env dist
  if [ -e "$backup/example-env" ] || [ -L "$backup/example-env" ]; then
    mv -- "$backup/example-env" examples/docker/.env
  fi
  if [ -e "$backup/example-state" ] || [ -L "$backup/example-state" ]; then
    mv -- "$backup/example-state" examples/docker/state
  fi
  if [ -e "$backup/dist" ] || [ -L "$backup/dist" ]; then
    mv -- "$backup/dist" dist
  fi
  rm -rf -- "$backup"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -e examples/docker/.env ] || [ -L examples/docker/.env ]; then
  mv -- examples/docker/.env "$backup/example-env"
fi
if [ -e examples/docker/state ] || [ -L examples/docker/state ]; then
  mv -- examples/docker/state "$backup/example-state"
fi
if [ -e dist ] || [ -L dist ]; then
  mv -- dist "$backup/dist"
fi

image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
REPOWOLF_TEST_VALIDATE_IMAGE=repowolf:mvp \
  examples/docker/test-install-host-broker.sh
examples/docker/test-install-host-principal.sh
scripts/ci/docker-example/build-sandbox-image.sh
REPOWOLF_IMAGE=repowolf:mvp \
  scripts/ci/docker-example/bootstrap-disposable-state.sh
REPOWOLF_IMAGE=repowolf:mvp \
  scripts/ci/docker-example/install-fake-ssh-fixture.sh
docker compose -f examples/docker/compose.yaml \
  -f examples/docker/compose.smoke.yaml up -d repowolf
if ! examples/docker/wait-for-broker.sh 127.0.0.1 8443 30; then
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml logs repowolf
  exit 1
fi
scripts/ci/docker-example/assert-github-policy.sh
scripts/ci/docker-example/assert-fake-ssh.sh
scripts/ci/docker-example/assert-sandbox-boundary.sh
```

Expected: every extracted script executes its real behavior, all assertions pass, Compose is torn down, and any pre-existing ignored `.env`, `state/`, or `dist/` content is restored without being printed.

- [ ] **Step 6: Run repository-wide verification**

```bash
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
GH_TOKEN=dummy-ci-token \
REPOWOLF_TOKEN_AGENT=dummy-agent-token \
REPOWOLF_IMAGE=repowolf:mvp \
REPOWOLF_SANDBOX_IMAGE=repowolf-sandbox:local \
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml config >/dev/null
git diff --check
git status --short
```

Expected: Go race tests, the full flake, and Compose config pass. `git status` shows only intended source changes; ignored `dist/`, `.env`, and `state/` are not staged.

- [ ] **Step 7: Directly inspect the extraction threshold and reuse boundary**

Review `.github/workflows/ci.yml` and confirm:

- The six approved step names each invoke one script from `scripts/ci/docker-example/`.
- No other multiline block was extracted.
- A continued command line counts once.
- The sandbox inner commands are in `assert-sandbox-boundary.sh`, not YAML.
- CI still invokes `bootstrap.sh`, `wait-for-broker.sh`, Compose files, and installer tests.
- No CI file is sourced by an example script and no example private helper is sourced by CI.

This is a direct static verification required by the Testing Value Gate; do not encode it as a test that greps workflow text.

- [ ] **Step 8: Review and commit**

```bash
git diff --check
git diff -- .github/workflows/ci.yml scripts/ci/docker-example/lint.sh
git add .github/workflows/ci.yml scripts/ci/docker-example/lint.sh
git commit -m "ci: lint extracted Docker smoke scripts"
```

---

### Task 9: Final review, push, and exact-head PR verification

**Files:**
- Review: `docs/specs/2026-08-10-ci-shell-extraction-design.md`
- Review: `docs/plans/2026-08-10-ci-shell-extraction.md`
- Review: all commits after `f95a2a4bd40f59e7942237397822739179447d1e`
- Update through todo tool: `TODO-c54e9a00`

**Interfaces:**
- Consumes: the complete reviewed branch from Tasks 1-8.
- Produces: pushed `origin/feat/docker-example`, updated PR #2, successful exact-head CI, completed handoff todo, and no tag/release.

- [ ] **Step 1: Run a fresh whole-change reviewer gate**

Give a fresh reviewer the approved design, this plan, base `f95a2a4bd40f59e7942237397822739179447d1e`, current head SHA, and a full contextual diff. Require findings grouped as Critical, Important, and Minor with file:line evidence. Fix all Critical/Important findings through focused implementer/re-review loops before continuing.

Expected: no open Critical or Important findings. Explicitly triage Minor findings for merge.

- [ ] **Step 2: Re-run final verification after the last review fix**

First run the real extracted path from isolated generated state while preserving any ignored operator state:

```bash
set -euo pipefail
export REPOWOLF_IMAGE=repowolf:mvp
export REPOWOLF_SANDBOX_IMAGE=repowolf-sandbox:local
docker compose -f examples/docker/compose.yaml \
  -f examples/docker/compose.smoke.yaml down -v || true
backup=$(mktemp -d "$PWD/.superpowers/sdd/2026-08-10-ci-shell-extraction/task-8-backup.XXXXXX")
cleanup() {
  rc=$?
  trap - EXIT INT TERM
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml down -v || true
  rm -rf -- examples/docker/state examples/docker/.env dist
  if [ -e "$backup/example-env" ] || [ -L "$backup/example-env" ]; then
    mv -- "$backup/example-env" examples/docker/.env
  fi
  if [ -e "$backup/example-state" ] || [ -L "$backup/example-state" ]; then
    mv -- "$backup/example-state" examples/docker/state
  fi
  if [ -e "$backup/dist" ] || [ -L "$backup/dist" ]; then
    mv -- "$backup/dist" dist
  fi
  rm -rf -- "$backup"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
if [ -e examples/docker/.env ] || [ -L examples/docker/.env ]; then
  mv -- examples/docker/.env "$backup/example-env"
fi
if [ -e examples/docker/state ] || [ -L examples/docker/state ]; then
  mv -- examples/docker/state "$backup/example-state"
fi
if [ -e dist ] || [ -L dist ]; then
  mv -- dist "$backup/dist"
fi
image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
scripts/ci/docker-example/build-sandbox-image.sh
REPOWOLF_IMAGE=repowolf:mvp \
  scripts/ci/docker-example/bootstrap-disposable-state.sh
REPOWOLF_IMAGE=repowolf:mvp \
  scripts/ci/docker-example/install-fake-ssh-fixture.sh
docker compose -f examples/docker/compose.yaml \
  -f examples/docker/compose.smoke.yaml up -d repowolf
if ! examples/docker/wait-for-broker.sh 127.0.0.1 8443 30; then
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml logs repowolf
  exit 1
fi
scripts/ci/docker-example/assert-github-policy.sh
scripts/ci/docker-example/assert-fake-ssh.sh
scripts/ci/docker-example/assert-sandbox-boundary.sh
```

Then run the complete static and repository verification set:

```bash
nix develop -c scripts/ci/docker-example/lint.sh
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
ruby -e 'require "yaml"; YAML.parse_file(".github/workflows/ci.yml")'
GH_TOKEN=dummy-ci-token \
REPOWOLF_TOKEN_AGENT=dummy-agent-token \
REPOWOLF_IMAGE=repowolf:mvp \
REPOWOLF_SANDBOX_IMAGE=repowolf-sandbox:local \
  docker compose -f examples/docker/compose.yaml \
    -f examples/docker/compose.smoke.yaml config >/dev/null
git diff --check
git status --short --branch
```

Expected: all six scripts execute successfully, ignored operator state is restored, every static/repository command passes, and the worktree is clean.

- [ ] **Step 3: Push the reviewed branch without force**

```bash
git push origin feat/docker-example
head_sha=$(git rev-parse HEAD)
test "$head_sha" = "$(git rev-parse origin/feat/docker-example)"
printf 'pushed head: %s\n' "$head_sha"
```

Expected: local and remote feature heads match. Do not force-push.

- [ ] **Step 4: Watch the CI run for the exact pushed SHA**

```bash
head_sha=$(git rev-parse HEAD)
run_id=$(gh run list --repo rochecompaan/repowolf \
  --branch feat/docker-example --workflow CI --limit 20 \
  --json databaseId,headSha \
  --jq ".[] | select(.headSha == \"$head_sha\") | .databaseId" \
  | head -n1)
test -n "$run_id"
gh run watch "$run_id" --repo rochecompaan/repowolf --exit-status
gh pr checks 2 --repo rochecompaan/repowolf
```

Expected: the exact-head CI run succeeds, including `test`, `docker-example-smoke`, amd64 release smoke, and arm64 release smoke. If a check fails, use systematic debugging on the failing step; do not retry blindly or change unrelated code.

- [ ] **Step 5: Record completion without merging or releasing**

Append to `TODO-c54e9a00`:

- Final head SHA and commit range.
- PR URL: `https://github.com/rochecompaan/repowolf/pull/2`.
- Exact CI run ID and four successful checks.
- Shell lint command and full verification results.
- Reuse conclusion: executable/declarative interfaces are shared; no sourced cross-boundary library was introduced.
- Residual platform limits and any deferred Minor review notes.
- Confirmation that no `v0.1.0` tag or release was created.

Mark the todo complete only after exact-head CI succeeds. Keep the feature worktree and PR open unless the user separately selects a branch-completion option.
