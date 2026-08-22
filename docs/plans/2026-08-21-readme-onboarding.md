# README Onboarding and Multi-Architecture Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give new users a Docker-first onboarding path and publish native amd64/arm64 broker images behind one release tag.

**Architecture:** Keep `nix/oci.nix` as the broker-image source. Native GitHub runners build and smoke-check each architecture, then a final release job creates the multi-architecture manifest. The README becomes a short entry point and moves reference material into focused documents.

**Tech Stack:** Markdown, Bash, jq, Docker Compose v2, Docker Buildx, Nix, GitHub Actions, GHCR.

## Global Constraints

- Lead with Docker Compose. Present native Linux as the lower-overhead alternative.
- Native packages support Linux amd64 and Linux arm64 only.
- The Docker path covers Linux, macOS Terminal with Docker Desktop, and Windows WSL2 with Docker Desktop.
- Do not document native PowerShell, cmd, Windows ARM, native macOS binaries, or native Windows binaries.
- Keep `nix/oci.nix` as the only broker-image definition. Do not add a broker Dockerfile.
- Publish `${GITHUB_REF_NAME}-amd64` and `${GITHUB_REF_NAME}-arm64` before the canonical release tag.
- Do not create the canonical tag unless both architecture jobs succeed.
- Do not add a `latest` tag or change release versioning.
- Do not claim that macOS or Windows received manual validation until that validation occurs.
- Treat macOS and WSL2 checks as non-blocking follow-up work.
- Use short sentences, plain words, active voice, and one configuration term throughout.
- Use test-driven development for the OCI helper scripts in Tasks 1 and 2.
- Use the `nix-config` skill before the `flake.nix` change in Task 1.
- Use direct validation for Markdown and workflow YAML. Do not add static-content tests.
- Do not add tests that assert workflow YAML text. Test built-image and manifest behavior instead.

## File Structure

**Create:**

- `scripts/ci/oci/check-manifest-platforms.sh` — validates the platform set in raw OCI index JSON.
- `scripts/ci/oci/test-check-manifest-platforms.sh` — tests valid, missing, and extra platform sets.
- `scripts/ci/oci/smoke-image.sh` — builds, loads, inspects, and runs one native Nix OCI image.
- `scripts/ci/oci/test-smoke-image.sh` — tests argument and architecture validation with fake tools.
- `scripts/ci/oci/lint.sh` — checks shell syntax, runs ShellCheck, and runs both helper tests.
- `docs/installation.md` — native packages and OCI artifact installation.
- `docs/configuration.md` — policy model, tokens, TLS, service configuration, and client configuration.
- `docs/deployment.md` — OCI runtime, systemd, Home Manager, and Kubernetes deployment.

**Modify:**

- `flake.nix` — provide jq to the OCI validation scripts through the development shell.
- `.github/workflows/ci.yml` — run OCI helper checks and smoke both native architectures.
- `.github/workflows/release.yml` — push architecture tags and assemble the canonical manifest.
- `examples/docker/README.md` — make Compose the first path and add host-specific selection guidance.
- `README.md` — replace the deployment manual with the Docker-first onboarding entry point.

**Do not modify:**

- `nix/oci.nix` — the existing derivation already builds for both flake systems.
- RepoWolf Go source — this work changes distribution and documentation, not runtime policy.

---

### Task 1: Add OCI manifest-platform validation

**Files:**
- Create: `scripts/ci/oci/check-manifest-platforms.sh`
- Create: `scripts/ci/oci/test-check-manifest-platforms.sh`
- Create: `scripts/ci/oci/lint.sh`
- Modify: `flake.nix:39`

**Interfaces:**
- Consumes: Raw OCI index JSON on standard input.
- Produces: `check-manifest-platforms.sh`, which exits zero only for exactly `linux/amd64` and `linux/arm64`.
- Produces: `lint.sh`, which later tasks extend with new OCI helper scripts.

- [ ] **Step 1: Create the failing manifest-validator test**

Create `scripts/ci/oci/test-check-manifest-platforms.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
checker="$SCRIPT_DIR/check-manifest-platforms.sh"
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-manifest-test.XXXXXX")

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$tmpdir"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

valid='{"manifests":[{"platform":{"os":"linux","architecture":"arm64"}},{"platform":{"os":"linux","architecture":"amd64"}}]}'
missing_arm64='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}}]}'
extra_platform='{"manifests":[{"platform":{"os":"linux","architecture":"amd64"}},{"platform":{"os":"linux","architecture":"arm64"}},{"platform":{"os":"windows","architecture":"amd64"}}]}'

printf '%s\n' "$valid" | "$checker"

for fixture in "$missing_arm64" "$extra_platform"; do
  if printf '%s\n' "$fixture" | "$checker" >"$tmpdir/output" 2>&1; then
    echo "expected manifest platform validation to fail" >&2
    exit 1
  fi
  grep -F 'OCI manifest platforms do not match' "$tmpdir/output"
done
```

- [ ] **Step 2: Run the test and observe the expected failure**

Run:

```bash
chmod +x scripts/ci/oci/test-check-manifest-platforms.sh
scripts/ci/oci/test-check-manifest-platforms.sh
```

Expected: exit 127 because `check-manifest-platforms.sh` does not exist.

- [ ] **Step 3: Implement the manifest validator**

Create `scripts/ci/oci/check-manifest-platforms.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

expected=$(printf '%s\n' linux/amd64 linux/arm64)
actual=$(
  jq -r '
    if (.manifests | type) != "array" then
      error("OCI index does not contain a manifests array")
    else
      [.manifests[].platform | "\(.os)/\(.architecture)"]
      | sort
      | .[]
    end
  '
)

if [ "$actual" != "$expected" ]; then
  printf '%s\n' 'OCI manifest platforms do not match' >&2
  printf 'expected:\n%s\n' "$expected" >&2
  printf 'actual:\n%s\n' "${actual:-<none>}" >&2
  exit 1
fi
```

Make the script executable.

- [ ] **Step 4: Add jq to the development shell**

In `flake.nix`, change the package list to:

```nix
packages = with pkgs; [ go goreleaser jq shellcheck skopeo ];
```

Do not change any flake input.

- [ ] **Step 5: Add the OCI lint entry point**

Create `scripts/ci/oci/lint.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
scripts=(
  "$SCRIPT_DIR/check-manifest-platforms.sh"
  "$SCRIPT_DIR/test-check-manifest-platforms.sh"
  "$SCRIPT_DIR/lint.sh"
)

bash -n "${scripts[@]}"
shellcheck "${scripts[@]}"
"$SCRIPT_DIR/test-check-manifest-platforms.sh"
```

Make the script executable.

- [ ] **Step 6: Run the helper checks**

Run:

```bash
nix develop -c jq --version
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
```

Expected: exit 0. The invalid fixture cases print their expected diagnostic matches.

- [ ] **Step 7: Commit the validator**

```bash
git add flake.nix scripts/ci/oci
git commit -m "test(ci): validate OCI manifest platforms"
```

---

### Task 2: Smoke-check native OCI images on both architectures

**Files:**
- Create: `scripts/ci/oci/smoke-image.sh`
- Create: `scripts/ci/oci/test-smoke-image.sh`
- Modify: `scripts/ci/oci/lint.sh`
- Modify: `.github/workflows/ci.yml:22-43,106-127`

**Interfaces:**
- Consumes: One argument, `amd64` or `arm64`.
- Produces: `smoke-image.sh ARCH`, which builds `.#ociImage`, loads `repowolf:mvp`, validates its architecture, and runs `--version`.
- Uses: `scripts/ci/oci/lint.sh` from Task 1.

- [ ] **Step 1: Write the smoke-helper tests**

Create `scripts/ci/oci/test-smoke-image.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
smoke="$SCRIPT_DIR/smoke-image.sh"
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/repowolf-image-smoke-test.XXXXXX")

cleanup() {
  rc=$?
  trap - EXIT INT TERM
  rm -rf -- "$tmpdir"
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$tmpdir/bin"
cat >"$tmpdir/bin/nix" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'nix %s\n' "$*" >>"$FAKE_LOG"
if [ "$*" != 'build .#ociImage --no-link --print-out-paths' ]; then
  echo "unexpected nix arguments: $*" >&2
  exit 99
fi
printf '%s\n' "$FAKE_IMAGE"
EOF

cat >"$tmpdir/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'docker %s\n' "$*" >>"$FAKE_LOG"
case "${1:-}" in
  load)
    [ "$#" -eq 3 ]
    [ "$2" = -i ]
    [ "$3" = "$FAKE_IMAGE" ]
    ;;
  image)
    [ "${2:-}" = inspect ]
    printf '%s\n' "$FAKE_ARCH"
    ;;
  run)
    [ "$*" = 'run --rm repowolf:mvp --version' ]
    printf '%s\n' 'repowolf test'
    ;;
  *)
    echo "unexpected docker arguments: $*" >&2
    exit 99
    ;;
esac
EOF

chmod +x "$tmpdir/bin/nix" "$tmpdir/bin/docker"
export PATH="$tmpdir/bin:$PATH"
export FAKE_LOG="$tmpdir/commands.log"
export FAKE_IMAGE="$tmpdir/repowolf-image.tar"
: >"$FAKE_LOG"

FAKE_ARCH=amd64 "$smoke" amd64
FAKE_ARCH=arm64 "$smoke" arm64
grep -F 'nix build .#ociImage --no-link --print-out-paths' "$FAKE_LOG"
grep -F 'docker load -i' "$FAKE_LOG"
grep -F 'docker image inspect' "$FAKE_LOG"
grep -F 'docker run --rm repowolf:mvp --version' "$FAKE_LOG"

if FAKE_ARCH=arm64 "$smoke" amd64 >"$tmpdir/output" 2>&1; then
  echo "expected architecture mismatch" >&2
  exit 1
fi
grep -F 'OCI image architecture mismatch' "$tmpdir/output"

if "$smoke" windows-amd64 >"$tmpdir/output" 2>&1; then
  echo "expected unsupported architecture error" >&2
  exit 1
fi
grep -F 'unsupported OCI architecture' "$tmpdir/output"

if "$smoke" >"$tmpdir/output" 2>&1; then
  echo "expected usage error" >&2
  exit 1
fi
grep -F 'usage: smoke-image' "$tmpdir/output"
```

- [ ] **Step 2: Run the test and observe the expected failure**

Run:

```bash
chmod +x scripts/ci/oci/test-smoke-image.sh
scripts/ci/oci/test-smoke-image.sh
```

Expected: exit 127 because `smoke-image.sh` does not exist.

- [ ] **Step 3: Implement the smoke helper**

Create `scripts/ci/oci/smoke-image.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: smoke-image <amd64|arm64>" >&2
  exit 2
fi
expected_arch=$1
case "$expected_arch" in
  amd64|arm64) ;;
  *)
    echo "unsupported OCI architecture: $expected_arch" >&2
    exit 2
    ;;
esac

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd -- "$SCRIPT_DIR/../../.." && pwd)
cd -- "$REPO_ROOT"

image=$(nix build .#ociImage --no-link --print-out-paths)
docker load -i "$image"
actual_arch=$(docker image inspect --format '{{.Architecture}}' repowolf:mvp)
if [ "$actual_arch" != "$expected_arch" ]; then
  echo "OCI image architecture mismatch: expected $expected_arch, got $actual_arch" >&2
  exit 1
fi
docker run --rm repowolf:mvp --version
```

Make the script executable.

- [ ] **Step 4: Extend the OCI lint script**

Add these paths to the `scripts` array in `scripts/ci/oci/lint.sh`:

```bash
"$SCRIPT_DIR/smoke-image.sh"
"$SCRIPT_DIR/test-smoke-image.sh"
```

Add this command after the manifest test:

```bash
"$SCRIPT_DIR/test-smoke-image.sh"
```

- [ ] **Step 5: Run the helper tests**

Run:

```bash
nix develop -c scripts/ci/oci/lint.sh
```

Expected: exit 0.

- [ ] **Step 6: Run the real smoke helper on the current Linux host**

Run:

```bash
scripts/ci/oci/smoke-image.sh "$(go env GOARCH)"
```

Expected: Nix builds the image, Docker loads it, and RepoWolf prints its version.

- [ ] **Step 7: Move OCI smoke coverage to the native CI matrix**

In `.github/workflows/ci.yml`:

1. Add this step to the `test` job before the Nix flake check:

```yaml
      - name: Check OCI release helpers
        run: nix develop -c scripts/ci/oci/lint.sh
```

2. Remove the amd64-only `Smoke-test OCI image` step from the `test` job.
3. Add this step after `Build and smoke-test release archives` in `release-smoke`:

```yaml
      - name: Build and smoke-test OCI image
        run: scripts/ci/oci/smoke-image.sh "${{ matrix.arch }}"
```

Keep the existing amd64 and arm64 runner matrix unchanged.

- [ ] **Step 8: Validate the workflow and run the local checks**

Run:

```bash
nix run nixpkgs#actionlint -- .github/workflows/ci.yml
nix develop -c scripts/ci/oci/lint.sh
scripts/ci/oci/smoke-image.sh "$(go env GOARCH)"
```

Expected: all commands exit 0.

- [ ] **Step 9: Commit native image smoke coverage**

```bash
git add .github/workflows/ci.yml scripts/ci/oci
git commit -m "ci: smoke-test OCI images on native architectures"
```

---

### Task 3: Publish the multi-architecture release manifest

**Files:**
- Modify: `.github/workflows/release.yml:12-58`

**Interfaces:**
- Consumes: `smoke-image.sh` and `check-manifest-platforms.sh` from Tasks 1 and 2.
- Produces: `${GITHUB_REF_NAME}-amd64`, `${GITHUB_REF_NAME}-arm64`, and a canonical `${GITHUB_REF_NAME}` OCI manifest.
- Preserves: GoReleaser archive publication and current action version pins.

- [ ] **Step 1: Add native image smoke checks to `release-smoke`**

After the existing archive smoke step, add:

```yaml
      - name: Build and smoke-test OCI image
        run: scripts/ci/oci/smoke-image.sh "${{ matrix.arch }}"
```

- [ ] **Step 2: Rename the current `publish` job to `publish-archives`**

Keep these properties:

```yaml
  publish-archives:
    needs: release-smoke
    runs-on: ubuntu-24.04
    permissions:
      contents: write
```

Keep checkout, Nix installation, and `goreleaser release --clean`. Remove the existing single-architecture OCI publication step.

- [ ] **Step 3: Add the architecture-image publication matrix**

Add this job. Reuse the existing pinned checkout and Nix action revisions.

```yaml
  publish-image:
    needs: release-smoke
    strategy:
      fail-fast: false
      matrix:
        include:
          - runner: ubuntu-24.04
            arch: amd64
          - runner: ubuntu-24.04-arm
            arch: arm64
    runs-on: ${{ matrix.runner }}
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
      - uses: cachix/install-nix-action@b4b293eae0b79aac8a161bb32925a5508c9cca93 # v31
      - name: Build and smoke-test OCI image
        run: scripts/ci/oci/smoke-image.sh "${{ matrix.arch }}"
      - name: Publish architecture image
        env:
          GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          image="$(nix build .#ociImage --no-link --print-out-paths)"
          target="ghcr.io/rochecompaan/repowolf:$GITHUB_REF_NAME-${{ matrix.arch }}"
          printf '%s' "$GHCR_TOKEN" \
            | nix develop -c skopeo login \
                --username "$GITHUB_ACTOR" \
                --password-stdin ghcr.io
          nix develop -c skopeo copy \
            docker-archive:"$image" \
            docker://"$target"
```

Keep these action revisions unchanged. Do not upgrade actions in this change.

- [ ] **Step 4: Add the canonical manifest job**

Add this job after `publish-image`:

```yaml
  publish-image-manifest:
    needs: publish-image
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
      - uses: cachix/install-nix-action@b4b293eae0b79aac8a161bb32925a5508c9cca93 # v31
      - name: Publish and validate multi-architecture manifest
        env:
          GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          image="ghcr.io/rochecompaan/repowolf"
          tag="$GITHUB_REF_NAME"
          printf '%s' "$GHCR_TOKEN" \
            | docker login ghcr.io \
                --username "$GITHUB_ACTOR" \
                --password-stdin
          docker buildx imagetools create \
            --tag "$image:$tag" \
            "$image:$tag-amd64" \
            "$image:$tag-arm64"
          docker buildx imagetools inspect --raw "$image:$tag" \
            | nix develop -c scripts/ci/oci/check-manifest-platforms.sh
```

Keep the checkout revision unchanged. Do not use Buildx to build either image.

- [ ] **Step 5: Validate workflow syntax and helper behavior**

Run:

```bash
nix run nixpkgs#actionlint -- \
  .github/workflows/ci.yml \
  .github/workflows/release.yml
nix develop -c scripts/ci/oci/lint.sh
docker buildx version
docker buildx imagetools create --help >/dev/null
docker buildx imagetools inspect --help >/dev/null
```

Expected: all commands exit 0.

A workflow-text unit test has no Testing Value Gate value. The native image smoke jobs and manifest parser test cover behavior.

- [ ] **Step 6: Review release failure behavior**

Make sure that the job graph has these properties:

```text
release-smoke ──> publish-image[amd64,arm64] ──> publish-image-manifest
             └─> publish-archives
```

Make sure that `publish-image-manifest` depends on the complete matrix job. Make sure that no other job writes the canonical GHCR tag.

- [ ] **Step 7: Commit multi-architecture publication**

```bash
git add .github/workflows/release.yml
git commit -m "ci(release): publish multi-architecture OCI image"
```

---

### Task 4: Create focused installation, configuration, and deployment documents

**Files:**
- Create: `docs/installation.md`
- Create: `docs/configuration.md`
- Create: `docs/deployment.md`
- Read: `README.md:13-211`

**Interfaces:**
- Consumes: Existing README reference content.
- Produces: Stable link targets for the README rewrite in Task 6.
- Preserves: All security warnings, configuration examples, and supervision examples.

- [ ] **Step 1: Create `docs/installation.md`**

Start the file with this text:

```markdown
# Installation

RepoWolf publishes native Linux packages and an OCI image. Use the
[setup table](../README.md#choose-your-setup) to select Docker or native Linux.

Native archives, Nix packages, and OCI images support Linux amd64 and arm64.
macOS and Windows users run the OCI image through Docker Desktop.
```

Then make these exact content moves:

1. Add `## Native release archives`. Copy README lines 19-36 beneath it.
2. Add `## Nix`. Copy README lines 40-47 beneath it.
3. Add `## OCI image`. Copy the image facts from README line 51 beneath it.
4. Add a code fence that contains only the `docker pull` command from README line 54.
5. Add the existing Docker-example link from README lines 65-66.
6. End with `Next, [configure RepoWolf](configuration.md).`

Do not copy the `docker run` command or mount requirements into this file.
Task 4 Step 3 moves those deployment instructions.

- [ ] **Step 2: Create `docs/configuration.md`**

Start the file with this text:

```markdown
# Configuration

RepoWolf loads one strict YAML configuration at startup. A configuration
change requires a service restart.

## Policy model

A provider defines the GitHub API and Git hosts. A repository maps one policy
name to an owner, repository name, provider, and Git limits. A principal names
one or more token environment variables. A grant gives that principal an
explicit capability set for one repository.
```

Then make these exact content moves:

1. Add `## Tokens and TLS`. Copy README lines 70-85 beneath it.
2. Add `## Service configuration`. Copy README lines 89-153 beneath it.
3. Add `## Client configuration`. Copy README lines 157-164 beneath it.
4. End with `After configuration, [select a deployment](deployment.md).`

Preserve the full YAML example and each secret-handling warning.

- [ ] **Step 3: Create `docs/deployment.md`**

Start the file with this text:

```markdown
# Deployment

Run RepoWolf under a supervisor. Keep provider credentials, policy, and TLS
private keys outside every agent sandbox.
```

Then make these exact content moves:

1. Add `## OCI container`.
2. Add a shell code fence with the `docker run` command from README lines 55-60.
3. Copy the mount and service-credential warning from README line 63.
4. Add `## Systemd`. Copy README lines 168-190 beneath it.
5. Add `## Home Manager`. Copy README lines 191-205 beneath it.
6. Add `## Kubernetes`. Copy README lines 209-211 beneath it.

Preserve the warning that secret environment files must remain outside the
Nix store.

- [ ] **Step 4: Check the new documents for accidental omissions**

Compare all headings and fenced blocks in the old README with the new files:

```bash
python3 - <<'PY'
from pathlib import Path
for name in [
    'docs/installation.md',
    'docs/configuration.md',
    'docs/deployment.md',
]:
    text = Path(name).read_text()
    assert text.count('```') % 2 == 0, f'unclosed code fence: {name}'
    print(name, len(text.splitlines()), 'lines')
PY
```

Then read each new file from start to finish. Make sure that each command has the context that it needs.

- [ ] **Step 5: Commit the reference documents**

Do not remove the old README sections in this task. Temporary duplication keeps this commit independently useful.

```bash
git add docs/installation.md docs/configuration.md docs/deployment.md
git commit -m "docs: split installation and deployment reference"
```

---

### Task 5: Align the Docker guide with cross-platform onboarding

**Files:**
- Modify: `examples/docker/README.md:1-150`

**Interfaces:**
- Consumes: Existing Compose files, `bootstrap.sh`, and `wait-for-broker.sh`.
- Produces: The detailed Docker target linked from the top-level README.
- Preserves: Policy-denial, deploy-key, host-broker, reset, and troubleshooting material.

- [ ] **Step 1: Replace the platform-scope bullets with a host table**

Use this text under `## Requirements and platform scope`:

```markdown
You need Git, Bash, Docker, and the Compose v2 plugin.

| Host | Docker environment | Command environment |
| --- | --- | --- |
| Linux amd64 or arm64 | Docker Engine | Bash |
| macOS | Docker Desktop | Terminal |
| Windows 11 | Docker Desktop with WSL2 integration | WSL2 Bash |

Native PowerShell and cmd are not supported. The native host-broker path later
in this guide requires Linux.
```

Do not call macOS or Windows manually tested.

- [ ] **Step 2: Make the Compose path the quickstart**

Rename `Path A: compose broker + sandbox` to:

```markdown
## Quickstart: Compose broker and sandbox
```

Place these commands in one ordered flow:

```bash
docker compose build sandbox
cp .env.example .env
# Set GH_TOKEN in .env. Leave REPOWOLF_TOKEN_AGENT empty.
export REPOWOLF_REPO=rochecompaan/repowolf
./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo "$REPOWOLF_REPO"
```

Keep the explanation that Compose readiness is not TLS readiness.

- [ ] **Step 3: Move the boundary proof next to the first success**

Move the existing `Boundary proof` section to immediately follow the quickstart.
Keep this command unchanged:

```bash
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

Explain that the sandbox still receives a RepoWolf token and public CA. It does
not receive a GitHub token, SSH key, SSH client, or agent socket.

- [ ] **Step 4: Clarify the Git milestone and Linux-only host path**

Keep `Enable Git in compose` after the boundary proof. Preserve the deploy-key
and verified `known_hosts` requirements.

Rename `Path B: host broker + sandbox` to:

```markdown
## Native Linux broker and sandbox container
```

Start that section with:

```markdown
Choose this path when the broker host runs Linux and a service manager controls
RepoWolf. macOS and Windows users use the Compose broker path instead.
```

- [ ] **Step 5: Run direct Docker-guide checks**

Run:

```bash
nix develop -c scripts/ci/docker-example/lint.sh
GH_TOKEN=dummy REPOWOLF_TOKEN_AGENT=dummy \
  docker compose -f examples/docker/compose.yaml config >/dev/null
```

Expected: both commands exit 0.

- [ ] **Step 6: Commit the Docker guide update**

```bash
git add examples/docker/README.md
git commit -m "docs(docker): clarify cross-platform onboarding"
```

---

### Task 6: Rewrite the README as the onboarding entry point

**Files:**
- Modify: `README.md:1-211`
- Read: `docs/installation.md`
- Read: `docs/configuration.md`
- Read: `docs/deployment.md`
- Read: `examples/docker/README.md`

**Interfaces:**
- Consumes: The stable documentation targets from Tasks 4 and 5.
- Produces: A short Docker-first README with no duplicated reference manual.

- [ ] **Step 1: Keep the logo and add the CI badge**

Keep the existing centered `<picture>` markup and `width="500"`.

Add this badge below the logo:

```markdown
[![CI](https://github.com/rochecompaan/repowolf/actions/workflows/ci.yml/badge.svg)](https://github.com/rochecompaan/repowolf/actions/workflows/ci.yml)
```

If the existing HTML block permits valid Markdown nesting, center the badge.

- [ ] **Step 2: Write the plain-language introduction**

Use this meaning and keep each sentence short:

```markdown
RepoWolf lets an AI coding agent use GitHub without putting GitHub credentials
or SSH keys in the agent sandbox. A broker that you control holds the provider
credentials and enforces repository policy. The sandbox receives only the
RepoWolf client, a scoped RepoWolf token, and the public CA certificate.
```

Keep the existing product-boundary statement: RepoWolf does not create,
inspect, register, or attest sandboxes.

- [ ] **Step 3: Add the architecture flow and feature list**

Use a compact flow with these actors and data:

```text
Agent sandbox                  RepoWolf broker                    GitHub
--------------                 ---------------                    ------
gh / repowolf-git-ssh --TLS--> repository policy --provider auth--> API / Git
RepoWolf token + CA            audit JSON Lines
no GitHub token or SSH key
```

Add short feature bullets for:

- Explicit capabilities per principal and repository.
- Restricted GitHub operations through the `gh` compatibility client.
- Git over SSH through `repowolf-git-ssh`.
- Protected refs, protected deletes, and bounded ref updates.
- Strict startup configuration and JSON Lines audit output.

- [ ] **Step 4: Add the setup decision table**

Use this exact table under `## Choose your setup`:

```markdown
| Host | Recommended setup | Notes |
| --- | --- | --- |
| Linux | Docker for the quickest start. Native for a long-running broker. | Native packages support amd64 and arm64 |
| macOS | Docker Desktop from Terminal | No native RepoWolf package |
| Windows | Docker Desktop through WSL2 | Run commands inside WSL2; PowerShell and cmd are not supported |
```

Follow the table with one sentence: Docker Compose is the recommended first
setup because it needs no host RepoWolf installation.

- [ ] **Step 5: Add the Docker Compose quickstart**

State these requirements: Git, Bash, Docker with Compose v2, and a GitHub token.
Then use this command flow:

```bash
git clone https://github.com/rochecompaan/repowolf.git
cd repowolf/examples/docker
docker compose build sandbox
cp .env.example .env
# Edit .env. Set GH_TOKEN and leave REPOWOLF_TOKEN_AGENT empty.
export REPOWOLF_REPO=rochecompaan/repowolf
./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo "$REPOWOLF_REPO"
```

Tell the reader to replace `rochecompaan/repowolf` with a repository that the
GitHub token can read.

Add the boundary-proof command from Task 5. Then explain this result:

```text
The sandbox contains a RepoWolf token and the public CA. It does not contain
the GitHub token, an SSH key, OpenSSH, or an SSH agent socket.
```

Link to `examples/docker/README.md` for policy-denial and troubleshooting steps.

- [ ] **Step 6: Add Git and native Linux next steps**

Add `## Enable Git access`. Explain that GitHub SSH needs a broker-side key and
verified host fingerprints. Link to:

```markdown
[Enable Git in the Docker guide](examples/docker/README.md#enable-git-in-compose)
```

Add `## Native Linux`. Explain the use cases from the spec. Link to:

```markdown
- [Install RepoWolf](docs/installation.md)
- [Configure policy, tokens, and TLS](docs/configuration.md)
- [Deploy and supervise the broker](docs/deployment.md)
```

- [ ] **Step 7: Add the final navigation section**

Add `## Learn more` with these links:

```markdown
- [Complete Docker walkthrough](examples/docker/README.md)
- [Configuration reference](docs/configuration.md)
- [Deployment options](docs/deployment.md)
- [Approved MVP design](docs/specs/2026-08-01-repowolf-mvp-design.md)
```

Remove the old installation, configuration, supervision, and Kubernetes
sections after all moved content has a destination.

- [ ] **Step 8: Check local links and code fences**

Run:

```bash
python3 - <<'PY'
from pathlib import Path
import re

files = [
    Path('README.md'),
    Path('docs/installation.md'),
    Path('docs/configuration.md'),
    Path('docs/deployment.md'),
    Path('examples/docker/README.md'),
]
errors = []
for source in files:
    text = source.read_text()
    if text.count('```') % 2:
        errors.append(f'{source}: unclosed code fence')
    targets = re.findall(r'\[[^]]*\]\(([^)]+)\)', text)
    targets += re.findall(r'<img[^>]+src="([^"]+)"', text)
    for target in targets:
        path = target.split('#', 1)[0]
        if not path or '://' in path or path.startswith('mailto:'):
            continue
        resolved = (source.parent / path).resolve()
        if not resolved.exists():
            errors.append(f'{source}: missing {target}')
if errors:
    raise SystemExit('\n'.join(errors))
print('local documentation links and code fences are valid')
PY
```

Expected: `local documentation links and code fences are valid`.

- [ ] **Step 9: Read the README as a new user**

Make sure that these facts appear before the first command:

1. What provider credentials stay outside the sandbox.
2. Which host path the reader must choose.
3. Why Docker is the default.
4. Which tools the quickstart requires.

Make sure that no paragraph sends the reader to configuration before the first
`gh repo view` success.

- [ ] **Step 10: Commit the README rewrite**

```bash
git add README.md
git commit -m "docs(readme): add Docker-first onboarding"
```

---

### Task 7: Run final behavior and documentation verification

**Files:**
- Verify: all files changed in Tasks 1-6
- Do not create: platform-validation claims for macOS or Windows

**Interfaces:**
- Consumes: All implementation tasks.
- Produces: Completion evidence for the PR description and reviewer.

- [ ] **Step 1: Run shell, workflow, Go, and Nix checks**

Run:

```bash
nix develop -c scripts/ci/oci/lint.sh
nix develop -c scripts/ci/docker-example/lint.sh
nix run nixpkgs#actionlint -- \
  .github/workflows/ci.yml \
  .github/workflows/release.yml
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
scripts/ci/oci/smoke-image.sh "$(go env GOARCH)"
```

Expected: every command exits 0.

- [ ] **Step 2: Prepare disposable Docker verification state**

Use these prerequisites:

```text
VERIFY_REPO       A GitHub owner/repository that the token can read.
VERIFY_GH_TOKEN   A GitHub token for the broker. Do not print this value.
VERIFY_SSH_KEY    A disposable read-only deploy key already registered on VERIFY_REPO.
VERIFY_KNOWN_HOSTS A file made from GitHub host keys after fingerprint verification.
```

Do not create or register a deploy key without operator approval.

CAUTION: Make sure that `examples/docker/state` and `examples/docker/.env`
contain only disposable verification state. The cleanup step removes them.

- [ ] **Step 3: Run the API quickstart on Linux**

From `examples/docker`:

```bash
docker compose down -v >/dev/null 2>&1 || true
rm -rf -- state .env
cp .env.example .env
chmod 0600 .env
printf 'GH_TOKEN=%s\nREPOWOLF_TOKEN_AGENT=\n' "$VERIFY_GH_TOKEN" >.env
export REPOWOLF_REPO="$VERIFY_REPO"
./bootstrap.sh
docker compose build sandbox
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo "$VERIFY_REPO"
```

Expected: `gh repo view` returns repository data through RepoWolf.

- [ ] **Step 4: Run the sandbox boundary proof**

Run the exact command from the README:

```bash
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

Expected: exit 0.

- [ ] **Step 5: Run the documented Git milestone**

Reset the disposable state. Then bootstrap with the approved deploy key and
verified `known_hosts` file:

```bash
docker compose down -v
rm -rf -- state
REPOWOLF_REPO="$VERIFY_REPO" \
REPOWOLF_SSH_KEY="$VERIFY_SSH_KEY" \
REPOWOLF_KNOWN_HOSTS="$VERIFY_KNOWN_HOSTS" \
  ./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox \
  git ls-remote "git@github.com:$VERIFY_REPO.git"
```

Expected: Git prints remote refs through `repowolf-git-ssh`.

- [ ] **Step 6: Remove disposable secrets and state**

Run:

```bash
docker compose down -v
rm -rf -- state .env
unset VERIFY_GH_TOKEN VERIFY_SSH_KEY VERIFY_KNOWN_HOSTS VERIFY_REPO
```

If the operator created a deploy key for this test, ask the operator to remove
it from GitHub.

- [ ] **Step 7: Run final repository checks**

Return to the repository root. Run:

```bash
git diff --check
git status --short
git log --oneline --decorate -10
```

Expected: no uncommitted implementation files and no disposable Docker state.

- [ ] **Step 8: Record non-blocking platform follow-up**

Add this unchecked list to the PR description or a follow-up issue:

```markdown
### External Docker validation (non-blocking)

- [ ] macOS Apple Silicon with Docker Desktop
- [ ] macOS Intel with Docker Desktop
- [ ] Windows 11 amd64 with WSL2 and Docker Desktop integration

For each host: bootstrap, broker readiness, `gh repo view`, boundary proof,
Git `ls-remote`, and cleanup.
```

Do not change the README to say that these checks passed until a developer
reports successful results.

- [ ] **Step 9: If verification changed files, commit the fixes**

If a command exposed a documentation or script defect, fix the defect and run
the affected checks again. Then commit the fix with a specific Conventional
Commits subject. If no file changed, do not create an empty commit.
