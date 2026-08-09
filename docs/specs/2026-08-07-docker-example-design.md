# RepoWolf Docker Example Design

Status: approved design, revised after review (2026-08-09). Audience: developers evaluating or adopting RepoWolf who want a copy-pasteable Docker setup.

## Goal

Provide `examples/docker/`, a copy-pasteable example that demonstrates RepoWolf's security boundary in two run modes sharing one sandbox image:

1. **Compose quick start (documented first).** Docker Compose runs the published broker image next to the sandbox. This removes the need to install RepoWolf on the host, but still requires Docker and Bash.
2. **Host broker (production topology, documented second).** RepoWolf runs on the developer's Linux host; a sandbox container connects to it. Provider credentials and real provider tools stay on the trusted host.

The documented/tested host is Linux; macOS requires a Unix shell, and Windows users must run the commands from WSL2. Native PowerShell/cmd portability is not promised.

The canonical walkthrough lives in `examples/docker/README.md`. A behavioral CI smoke job guards image construction, `gh` policy outcomes, the brokered Git upload-pack path, and the sandbox boundary.

## Non-goals

- Publishing a sandbox/client image. Developers build it locally.
- Kubernetes manifests (the top-level README already covers Kubernetes).
- Native Windows shell support or a portability guarantee outside Linux.
- Rootless Docker, Podman, or alternative-runtime specifics.
- Installing or restarting the host broker's systemd or other supervisor unit.
- Multi-principal or multi-repository examples.
- RepoWolf Go-code or configuration-schema changes.
- Automating release tags. Publishing `v0.1.0` remains a separate, explicit owner action after merge.

## Release rollout

The example defaults pin `v0.1.0`, but that release does not exist yet. Implementation and CI do **not** require publishing it: CI builds current-commit archives with goreleaser, serves them from a temporary local fixture, and overrides `REPOWOLF_VERSION`/`REPOWOLF_RELEASE_ROOT`; it also overrides the broker image with the current Nix-built `repowolf:mvp` image.

After the example merges, an owner may explicitly tag `v0.1.0`. The release workflow then publishes the archives/checksum file and `ghcr.io/rochecompaan/repowolf:v0.1.0`, activating the documented defaults. No plan step pushes a tag autonomously.

## Decisions

- **Two run modes, one sandbox image.** The client artifact is identical in both modes; only broker placement differs.
- **Release archive verification works on Alpine BusyBox.** The Dockerfile downloads `checksums.txt`, extracts exactly the line for `repowolf_linux_${TARGETARCH}.tar.gz`, requires exactly one match, and runs BusyBox `sha256sum -c` on that filtered file. It does not use unsupported GNU-only `--ignore-missing`.
- **Public build-argument contract is `REPOWOLF_VERSION`.** It defaults to `v0.1.0`. `REPOWOLF_RELEASE_ROOT` defaults to GitHub's release-download root and exists only for mirrors/local CI fixtures.
- **Artifacts are checksum-verified, not signed.** The release workflow has no signature mechanism; documentation does not claim otherwise.
- **Fixed state paths.** Bootstrap always writes `examples/docker/state` and `examples/docker/.env`, which are exactly the paths Compose reads. Unsupported `STATE_DIR`/`ENV_FILE` overrides are removed.
- **Repository input is data, never program text.** `REPOWOLF_REPO` must match a conservative GitHub `owner/name` grammar (one slash; owner `[A-Za-z0-9-]`, repository `[A-Za-z0-9._-]`, bounded lengths). Rendering and `.env` updates use Bash string operations/line loops, not dynamically built `sed` programs or GNU `sed -i`.
- **Broker-readable configuration/TLS without broad permissions.** Cert generation and config rendering run as the host user. A root `alpine:3` helper sets `config.yaml`, the TLS directory, and the public/server TLS files to host owner + GID 65532 with modes `0640`/`0750`; `ca.key` stays host-owned `0600` and is never mounted into the broker or sandbox. Compose mounts `config.yaml`, `tls.crt`, and `tls.key` individually into the broker, and only `ca.crt` into the sandbox. Verification runs `config validate` as UID/GID 65532.
- **Git always needs service-side SSH setup.** Public GitHub SSH clones still require authentication and host verification. Compose supports optional `REPOWOLF_SSH_KEY` plus `REPOWOLF_KNOWN_HOSTS` inputs at bootstrap. Both must be supplied together. The private key becomes UID/GID 65532 mode `0600`; OpenSSH user config becomes root:GID-65532 mode `0640` (an ownership pattern accepted by OpenSSH), and known-hosts is broker-group readable. An `ssh -G` check under the actual OCI user verifies the effective identity/agent/known-hosts settings. No SSH material enters the sandbox.
- **CI proves the Git provider process actually starts.** Audit `git.upload-pack`/`accepted` is emitted before `Runner.Start`, so it is insufficient alone. CI builds a small static fake-SSH fixture, mounts it only into the broker, points `tools.ssh` at it in the disposable rendered config, runs `git ls-remote`, and asserts the fixture's host-visible argv log contains `-T`, port `22`, `git@github.com`, and `git-upload-pack 'owner/name.git'`. The audit event remains a secondary policy assertion. Real OpenSSH key/known-host handling is verified separately with `ssh -G`; successful cloning is manually verified with real service-side credentials.
- **Audit events are the stable assertion surface.** The client intentionally reports both policy denials and provider failures as `gh: GitHub operation failed`; CI distinguishes them with broker audit events (`accepted`, `denied`/`PermissionDenied`, `git.upload-pack`).
- **Readiness is explicit.** A bounded `wait-for-broker.sh` TCP probe is shared by the README and CI. `depends_on` is not treated as a readiness signal.
- **CI runs unconditionally.** Broker/client changes outside `examples/docker/**` can break the example.
- **Gitignore is accidental-commit protection only.** `.gitignore` cannot prevent `git add -f`; docs state that limitation rather than promising secrets can never be committed.
- **Host installation has two focused entry points.** `install-host-broker.sh` owns host TLS/config installation; `install-host-principal.sh` owns the example principal token and environment file. Both run from a normal operator shell and invoke `sudo` only for privileged filesystem operations. Neither starts or restarts the broker.
- **Host policy is declarative.** `config/repowolf-host.yaml` contains the complete host policy with validated placeholders. The broker installer renders the template into a private temporary file, validates it, and installs it atomically instead of embedding YAML in a README or shell heredoc.
- **Host installers are configurable and testable.** Example defaults target `rochecompaan/repowolf`, the Docker bridge gateway on port `9443`, and `/etc`, `/var/lib`, and `/run` RepoWolf paths. The exact overrides are `REPOWOLF_REPO`, `REPOWOLF_LISTEN`, `REPOWOLF_GH_PATH`, `REPOWOLF_SSH_PATH`, `REPOWOLF_BROKER_USER`, `REPOWOLF_BROKER_GROUP`, `REPOWOLF_CONFIG_DIR`, `REPOWOLF_STATE_DIR`, and `REPOWOLF_RUNTIME_DIR`. Tool and directory overrides must be absolute. Tests redirect install paths into temporary directories and shim privileged commands; they never write real system state.
- **Host installation refuses implicit replacement.** Existing TLS, policy, token, or principal environment state causes a clear failure. Partial-failure cleanup removes only paths created by that invocation; token/key contents are never printed.

## Directory layout

```
examples/docker/
├── README.md
├── sandbox/
│   └── Dockerfile
├── compose.yaml
├── compose.smoke.yaml  # CI/local test-only fake-SSH mount
├── bootstrap.sh
├── install-host-broker.sh
├── install-host-principal.sh
├── wait-for-broker.sh
├── test-bootstrap-unreadable-ssh.sh
├── test-install-host-broker.sh
├── test-install-host-principal.sh
├── test-wait-for-broker.sh
├── config/
│   ├── repowolf.yaml
│   └── repowolf-host.yaml
├── .env.example
└── .gitignore
```

Generated `state/` holds Compose TLS material, the principal token, rendered config, and optional service-side SSH material. `.env` holds the Compose provider and principal environment values. Both are gitignored. Host installers use their configured absolute system directories instead.

## Sandbox image

The multi-stage Dockerfile:

1. Fetch stage (`alpine:3`): accepts `REPOWOLF_VERSION` and `REPOWOLF_RELEASE_ROOT`, downloads the selected Linux archive and co-hosted checksum file, filters one exact checksum line, verifies it with BusyBox `sha256sum -c`, and extracts only `repowolf-client`.
2. Runtime stage (`alpine:3`): adds `git` and CA roots, creates UID/GID `65532:65532`, installs `repowolf-client`, symlinks `gh` and `repowolf-git-ssh`, and sets `GIT_SSH_COMMAND=repowolf-git-ssh`.

Deliberately absent: the real `gh`, OpenSSH, provider tokens, SSH keys, and `SSH_AUTH_SOCK`.

## Host-broker path

The README keeps this walkthrough short by delegating privileged setup to two scripts:

```sh
REPOWOLF_REPO=rochecompaan/repowolf ./install-host-broker.sh
./install-host-principal.sh
```

The installer interface is:

- `REPOWOLF_REPO` defaults to `rochecompaan/repowolf`;
- `REPOWOLF_LISTEN` defaults to the detected Docker bridge gateway on port `9443`;
- `REPOWOLF_GH_PATH` and `REPOWOLF_SSH_PATH` default to the absolute results of `command -v gh` and `command -v ssh`;
- `REPOWOLF_BROKER_USER` and `REPOWOLF_BROKER_GROUP` both default to `repowolf`; and
- `REPOWOLF_CONFIG_DIR`, `REPOWOLF_STATE_DIR`, and `REPOWOLF_RUNTIME_DIR` default to `/etc/repowolf`, `/var/lib/repowolf`, and `/run/repowolf`.

A missing Docker bridge is an error unless `REPOWOLF_LISTEN` is supplied. Listen, repository, user/group, tool, and directory values reject newlines and control characters. Tool and directory paths must be absolute. Template rendering YAML-quotes every substituted scalar rather than treating environment input as YAML syntax.

`install-host-broker.sh`:

1. validates repository, listen-address, broker-user/group, directory, and tool-path inputs before invoking `sudo`;
2. defaults the listener to the detected Docker bridge gateway on port `9443` and resolves host `gh`/`ssh` to absolute paths;
3. refuses existing TLS or policy destinations;
4. renders `config/repowolf-host.yaml` into a private temporary file with YAML-safe scalar encoding and validates it before privileged writes;
5. creates TLS state, installs policy/TLS ownership and modes for the broker identity, and validates the installed policy as that identity; and
6. removes only invocation-created partial state if installation fails.

`install-host-principal.sh`:

1. validates the same absolute state/runtime directory and broker-identity contract;
2. refuses an existing token or principal environment file;
3. generates the `example-agent` token once;
4. installs the root-owned mode-`0600` token and separate `REPOWOLF_TOKEN_EXAMPLE_AGENT` environment file without modifying provider settings; and
5. reports the two environment files the broker supervisor must load, but never prints the token.

Both scripts run as the normal operator and invoke `sudo` internally. Neither starts or restarts the broker. The README retains only the short supervisor step, client invocation, and SSH prerequisite explanation.

The canonical client command uses the supported repository syntax:

```sh
DOCKER_GATEWAY="$(docker network inspect bridge \
  --format '{{(index .IPAM.Config 0).Gateway}}')"
docker run --rm -it \
  -e REPOWOLF_ENDPOINT="https://$DOCKER_GATEWAY:9443" \
  -e REPOWOLF_SERVER_NAME=repowolf.internal \
  -e REPOWOLF_TOKEN="$(sudo cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

The default client command derives the same Docker bridge gateway as the installer. An operator who overrides `REPOWOLF_LISTEN` must use its reachable host and port in `REPOWOLF_ENDPOINT`. The boundary proof checks that `GH_TOKEN` and `ssh` are absent, `gh` resolves to `repowolf-client`, and brokered `gh` works. Git clone is demonstrated only after confirming the host broker has an SSH identity and verified known-hosts state.

## Compose bootstrap

`bootstrap.sh` accepts:

- required `REPOWOLF_REPO=owner/name`;
- optional `REPOWOLF_IMAGE` (default `ghcr.io/rochecompaan/repowolf:v0.1.0`);
- optional pair `REPOWOLF_SSH_KEY` and `REPOWOLF_KNOWN_HOSTS` for Git operations.

It uses fixed script-relative `state/` and `.env` paths, sets `umask 077`, refuses existing state, and performs these steps:

1. Validate the repository grammar before creating state.
2. Generate certs and token through the broker image.
3. Render config with a Bash line loop and update `.env` with a portable temporary-file loop.
4. Use an `alpine:3` helper to assign the narrow config/TLS group/permission set described above.
5. If both SSH inputs are supplied, copy them into broker-only state, add a deterministic OpenSSH config (`IdentityAgent none`, `IdentitiesOnly yes`), and apply OpenSSH-valid owner/mode restrictions (config owner root, key owner 65532).
6. Validate rendered policy through the broker image as UID/GID 65532 and validate effective SSH settings with the image's real `ssh -G` when SSH inputs exist.

The script never prints token/key contents. It runs under Bash 3.2-compatible syntax. Linux is the verified target; macOS requires Bash, and Windows requires WSL2.

## Compose services

- `repowolf`: published/current broker image, rendered policy mounted read-only, server certificate/key mounted individually, optional `state/ssh` mounted at `/tmp/.ssh`, `GH_TOKEN` and `REPOWOLF_TOKEN_AGENT` from `.env`, loopback-only host port `127.0.0.1:8443`.
- `sandbox`: locally built image, endpoint `https://repowolf:8443`, principal token and public CA only, no provider credentials or SSH material.

`wait-for-broker.sh 127.0.0.1 8443 30` gates the first client command.

## Sample policy

One GitHub provider/repository and principal. Grants repository/issues/pull-requests/statuses/git read/write; deliberately omits `actions:read` so `gh run list --repo owner/name` is a deterministic policy-denial demonstration. Git guardrails deny writes to `refs/heads/main`, deny deletes, and cap ref updates.

## CI smoke test

A new `docker-example-smoke` job:

1. Syntax-checks and runs the Compose bootstrap/readiness and host-installer behavior tests using temporary directories and command shims.
2. Builds/loads the current broker OCI image (`repowolf:mvp`).
3. Builds current-commit goreleaser snapshot archives, serves them from a temporary Go HTTP fixture with an immediate exit trap plus bounded readiness probe, and builds the sandbox with `REPOWOLF_VERSION=snapshot` plus a local `REPOWOLF_RELEASE_ROOT`.
4. Bootstraps state with a dummy `GH_TOKEN`; validates broker-UID access to config/TLS and the real image's effective OpenSSH configuration.
5. Builds a static fake-SSH fixture into disposable broker-only state, rewrites only the fixed `ssh: null` test field to its mounted path, and pre-creates a host-readable argv log.
6. Starts the broker and waits with `wait-for-broker.sh`.
7. Asserts `gh repo view --repo ...` reaches an audit `github.repository_view`/`accepted` event before failing upstream on the dummy token.
8. Asserts `gh run list --repo ...` yields audit `denied`/`PermissionDenied` and no accepted `github.run_list` event.
9. Starts `git ls-remote git@github.com:owner/name.git`, expects fixture exit, and asserts both audit `git.upload-pack`/`accepted` and the exact fake-SSH argv log. The side effect proves `Runner.Start` occurred.
10. Asserts the sandbox has no `GH_TOKEN` or `ssh`, runs as UID 65532, and resolves both shims to `repowolf-client`.
11. Tears down in an `if: always()` step.

Workflow syntax is checked with a temporary Go program using the repository's existing `gopkg.in/yaml.v3`; no undeclared PyYAML dependency.

## Failure behavior

- Strict repository validation rejects newlines, whitespace, extra slashes, shell/sed metacharacters, and overlong names before state creation.
- Bootstrap refuses existing `state/`; reset documentation warns that deleting it destroys CA/private keys/tokens and recommends backing it up first.
- Host installers reject invalid or relative overrides before privileged writes, refuse existing destinations, and remove only invocation-created partial state.
- Missing only one SSH input is an error; omitting both keeps `gh` flows working but Git operations fail clearly at service-side SSH setup/authentication.
- Compose `${VAR:?}` guards fail fast on absent provider/principal env values.
- `wait-for-broker.sh` times out with a non-zero status and directs users to broker logs.

## Security boundaries

- Provider credentials and SSH private material exist only on the host or broker side.
- The CA private key remains host-owned mode `0600` and is not container-mounted.
- Broker TLS/SSH files use narrow UID/GID modes; no world-readable private key workaround.
- `.env`/`state/` ignores reduce accidental commits but are not an enforcement boundary; users can override ignores and remain responsible for secret handling.
- Compose publishes the broker only on loopback; the host path binds only the Docker bridge gateway.

## Documentation updates

- `examples/docker/README.md`: prerequisites and platform scope; sandbox contract; Compose quick start first; concise host path using the two installer scripts second; readiness and audit-based policy demo; full service-side SSH prerequisites for all Git operations; destructive reset warning; troubleshooting/security notes.
- Top-level README OCI section: one pointer to `examples/docker/README.md`.

## Verification

- BusyBox checksum path tested directly (`sha256sum -c` filtered file); no GNU-only option.
- Bootstrap `bash -n`, shellcheck via an explicit availability branch, strict-input rejection cases, state ownership/mode checks, duplicate-run refusal, and config validation.
- Host installer behavior tests cover valid rendering/install plans, absolute override validation, refusal to overwrite state, ownership/mode commands, token non-disclosure, provider/principal environment separation, and invocation-owned cleanup in temporary directories.
- Full local compose smoke for granted/denied `gh`, Git upload-pack acceptance, readiness, and sandbox boundary.
- Workflow YAML parsed with the temporary Go/yaml checker.
- Host-broker path manually succeeds with real `GH_TOKEN` and SSH credentials.
- Final `go test -race ./...` runs directly (no pipeline that can mask its exit status).

## Acceptance criteria

1. A Linux developer with Docker and Bash can bootstrap and run the compose example without installing RepoWolf on the host.
2. The README presents the Compose quick start before the host-broker walkthrough.
3. A normal sudo-capable operator can install the documented host TLS/policy and example principal with the two focused scripts, then connect using only the documented client variables.
4. The sandbox contains no provider credentials, real `gh`, or `ssh`; both restricted shims point to `repowolf-client`.
5. Audit events distinguish a granted provider request from a denied capability.
6. CI starts the broker's configured SSH executable and proves it received the exact `git-upload-pack 'owner/name.git'` argv; audit acceptance alone does not satisfy this criterion.
7. The actual OCI user can read its config/TLS files and `ssh -G` accepts the planned SSH ownership/modes and effective settings.
8. Successful Git clone documentation requires both broker-side authentication and verified known-hosts state.
9. Gitignore protects against ordinary accidental adds; documentation does not claim forced adds are impossible.
10. CI exercises host-installer behavior, compose startup/readiness, `gh` policy outcomes, the Git process-launch path, and the sandbox boundary on every PR and `main` push.
