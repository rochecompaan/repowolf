# RepoWolf Docker Example Design

Status: approved design, revised after review (2026-08-08). Audience: developers evaluating or adopting RepoWolf who want a copy-pasteable Docker setup.

## Goal

Provide `examples/docker/`, a copy-pasteable example that demonstrates RepoWolf's security boundary in two run modes sharing one sandbox image:

1. **Host broker (primary path).** RepoWolf runs on the developer's Linux host; a sandbox container connects to it. Provider credentials and real provider tools stay on the trusted host.
2. **Compose variant (self-contained broker path).** Docker Compose runs the published broker image next to the sandbox. This removes the need to install RepoWolf on the host, but still requires Docker and Bash. The documented/tested host is Linux; macOS requires a Unix shell, and Windows users must run the commands from WSL2. Native PowerShell/cmd portability is not promised.

The canonical walkthrough lives in `examples/docker/README.md`. A behavioral CI smoke job guards image construction, `gh` policy outcomes, the brokered Git upload-pack path, and the sandbox boundary.

## Non-goals

- Publishing a sandbox/client image. Developers build it locally.
- Kubernetes manifests (the top-level README already covers Kubernetes).
- Native Windows shell support or a portability guarantee outside Linux.
- Rootless Docker, Podman, or alternative-runtime specifics.
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
- **Broker-readable TLS without broad permissions.** Cert generation runs as the host user. A root `alpine:3` helper changes only the TLS directory/certificate/server-key group to GID 65532 and modes to directory `0750`, public/server files `0640`; `ca.key` stays host-owned `0600` and is never mounted into the broker or sandbox. Compose mounts `tls.crt` and `tls.key` individually into the broker, and only `ca.crt` into the sandbox.
- **Git always needs service-side SSH setup.** Public GitHub SSH clones still require authentication and host verification. Compose supports optional `REPOWOLF_SSH_KEY` plus `REPOWOLF_KNOWN_HOSTS` inputs at bootstrap. Both must be supplied together. The private key becomes UID/GID 65532 mode `0600`; known-hosts/config are readable only by the broker group. No SSH material enters the sandbox.
- **CI tests the Git contract without real credentials.** It generates a disposable unregistered key and keyscans GitHub, starts `git ls-remote`, expects provider authentication failure, and asserts a `git.upload-pack` audit event with `outcome: accepted`. This proves the sandbox shim, TLS/token transport, policy, service-side SSH launch, and host-key path. Successful cloning is manually verified with real service-side SSH credentials.
- **Audit events are the stable assertion surface.** The client intentionally reports both policy denials and provider failures as `gh: GitHub operation failed`; CI distinguishes them with broker audit events (`accepted`, `denied`/`PermissionDenied`, `git.upload-pack`).
- **Readiness is explicit.** A bounded `wait-for-broker.sh` TCP probe is shared by the README and CI. `depends_on` is not treated as a readiness signal.
- **CI runs unconditionally.** Broker/client changes outside `examples/docker/**` can break the example.
- **Gitignore is accidental-commit protection only.** `.gitignore` cannot prevent `git add -f`; docs state that limitation rather than promising secrets can never be committed.

## Directory layout

```
examples/docker/
├── README.md
├── sandbox/
│   └── Dockerfile
├── compose.yaml
├── bootstrap.sh
├── wait-for-broker.sh
├── config/
│   └── repowolf.yaml
├── .env.example
└── .gitignore
```

Generated `state/` holds TLS material, the principal token, rendered config, and optional service-side SSH material. `.env` holds the provider and principal environment values. Both are gitignored.

## Sandbox image

The multi-stage Dockerfile:

1. Fetch stage (`alpine:3`): accepts `REPOWOLF_VERSION` and `REPOWOLF_RELEASE_ROOT`, downloads the selected Linux archive and co-hosted checksum file, filters one exact checksum line, verifies it with BusyBox `sha256sum -c`, and extracts only `repowolf-client`.
2. Runtime stage (`alpine:3`): adds `git` and CA roots, creates UID/GID `65532:65532`, installs `repowolf-client`, symlinks `gh` and `repowolf-git-ssh`, and sets `GIT_SSH_COMMAND=repowolf-git-ssh`.

Deliberately absent: the real `gh`, OpenSSH, provider tokens, SSH keys, and `SSH_AUTH_SOCK`.

## Host-broker path

The README instructs operators to bind the broker to the Docker bridge gateway (for example `172.17.0.1:9443`), not `0.0.0.0`; pin service-side `tools.gh`/`tools.ssh` to absolute paths; and set `REPOWOLF_SERVER_NAME` to the host certificate's DNS SAN.

The canonical client command uses the supported repository syntax:

```sh
docker run --rm -it \
  -e REPOWOLF_ENDPOINT=https://172.17.0.1:9443 \
  -e REPOWOLF_SERVER_NAME=localhost \
  -e REPOWOLF_TOKEN="$(cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

The boundary proof checks that `GH_TOKEN` and `ssh` are absent, `gh` resolves to `repowolf-client`, and brokered `gh` works. Git clone is demonstrated only after confirming the host broker has an SSH identity and verified known-hosts state.

## Compose bootstrap

`bootstrap.sh` accepts:

- required `REPOWOLF_REPO=owner/name`;
- optional `REPOWOLF_IMAGE` (default `ghcr.io/rochecompaan/repowolf:v0.1.0`);
- optional pair `REPOWOLF_SSH_KEY` and `REPOWOLF_KNOWN_HOSTS` for Git operations.

It uses fixed script-relative `state/` and `.env` paths, sets `umask 077`, refuses existing state, and performs these steps:

1. Validate the repository grammar before creating state.
2. Generate certs and token through the broker image.
3. Render config with a Bash line loop and update `.env` with a portable temporary-file loop.
4. Use an `alpine:3` helper to assign the narrow group/permission set described above.
5. If both SSH inputs are supplied, copy them into broker-only state, add a deterministic OpenSSH config (`IdentityAgent none`, `IdentitiesOnly yes`), and apply owner/mode restrictions.
6. Validate rendered policy through the broker image.

The script never prints token/key contents. It runs under Bash 3.2-compatible syntax. Linux is the verified target; macOS requires Bash, and Windows requires WSL2.

## Compose services

- `repowolf`: published/current broker image, rendered policy mounted read-only, server certificate/key mounted individually, optional `state/ssh` mounted at `/tmp/.ssh`, `GH_TOKEN` and `REPOWOLF_TOKEN_AGENT` from `.env`, loopback-only host port `127.0.0.1:8443`.
- `sandbox`: locally built image, endpoint `https://repowolf:8443`, principal token and public CA only, no provider credentials or SSH material.

`wait-for-broker.sh 127.0.0.1 8443 30` gates the first client command.

## Sample policy

One GitHub provider/repository and principal. Grants repository/issues/pull-requests/statuses/git read/write; deliberately omits `actions:read` so `gh run list --repo owner/name` is a deterministic policy-denial demonstration. Git guardrails deny writes to `refs/heads/main`, deny deletes, and cap ref updates.

## CI smoke test

A new `docker-example-smoke` job:

1. Builds/loads the current broker OCI image (`repowolf:mvp`).
2. Builds current-commit goreleaser snapshot archives, serves them from a temporary Go HTTP fixture with an exit trap, and builds the sandbox with `REPOWOLF_VERSION=snapshot` plus a local `REPOWOLF_RELEASE_ROOT`.
3. Generates a disposable SSH key and GitHub known-hosts test file; bootstraps state using those inputs and a dummy `GH_TOKEN`.
4. Starts the broker and waits with `wait-for-broker.sh`.
5. Asserts `gh repo view --repo ...` reaches an audit `github.repository_view`/`accepted` event before failing upstream on the dummy token.
6. Asserts `gh run list --repo ...` yields audit `denied`/`PermissionDenied` and no accepted `github.run_list` event.
7. Starts `git ls-remote git@github.com:owner/name.git`, expects dummy-key authentication failure, and asserts audit `git.upload-pack`/`accepted` (and no completed upload-pack).
8. Asserts the sandbox has no `GH_TOKEN` or `ssh`, runs as UID 65532, and resolves both shims to `repowolf-client`.
9. Tears down in an `if: always()` step.

Workflow syntax is checked with a temporary Go program using the repository's existing `gopkg.in/yaml.v3`; no undeclared PyYAML dependency.

## Failure behavior

- Strict repository validation rejects newlines, whitespace, extra slashes, shell/sed metacharacters, and overlong names before state creation.
- Bootstrap refuses existing `state/`; reset documentation warns that deleting it destroys CA/private keys/tokens and recommends backing it up first.
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

- `examples/docker/README.md`: prerequisites and platform scope; sandbox contract; host path; compose quick start with readiness wait; audit-based policy demo; full service-side SSH prerequisites for all Git operations; destructive reset warning; troubleshooting/security notes.
- Top-level README OCI section: one pointer to `examples/docker/README.md`.

## Verification

- BusyBox checksum path tested directly (`sha256sum -c` filtered file); no GNU-only option.
- Bootstrap `bash -n`, shellcheck via an explicit availability branch, strict-input rejection cases, state ownership/mode checks, duplicate-run refusal, and config validation.
- Full local compose smoke for granted/denied `gh`, Git upload-pack acceptance, readiness, and sandbox boundary.
- Workflow YAML parsed with the temporary Go/yaml checker.
- Host-broker path manually succeeds with real `GH_TOKEN` and SSH credentials.
- Final `go test -race ./...` runs directly (no pipeline that can mask its exit status).

## Acceptance criteria

1. A Linux developer with Docker and Bash can bootstrap and run the compose example without installing RepoWolf on the host.
2. A developer with a host broker can build the sandbox and connect using only the documented client variables.
3. The sandbox contains no provider credentials, real `gh`, or `ssh`; both restricted shims point to `repowolf-client`.
4. Audit events distinguish a granted provider request from a denied capability.
5. CI starts a real brokered Git upload-pack path through service-side SSH and asserts policy acceptance without using a real SSH credential.
6. Successful Git clone documentation requires both broker-side authentication and verified known-hosts state.
7. Gitignore protects against ordinary accidental adds; documentation does not claim forced adds are impossible.
8. CI exercises compose startup/readiness, `gh` policy outcomes, the Git path, and the sandbox boundary on every PR and `main` push.
