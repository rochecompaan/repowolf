# RepoWolf Docker Example Design

Status: approved design (2026-08-07). Audience: developers evaluating or adopting RepoWolf who want a copy-pasteable Docker setup.

## Goal

Provide `examples/docker/`, a self-contained, copy-pasteable example that demonstrates RepoWolf's security boundary in Docker, in two run modes that share one sandbox image:

1. **Host broker (primary path).** RepoWolf runs on the developer's Linux host exactly as the README already documents; a sandbox container built from the example connects to it. This is the production topology: the broker and all credentials stay on the trusted host, the agent runs untrusted in a container.
2. **Compose variant (self-contained path).** `docker compose up` runs the broker from the published `ghcr.io/rochecompaan/repowolf` image alongside the sandbox container. For evaluation, demos, and CI — no host installation required.

The example is documented in `examples/docker/README.md` and guarded by a behavioral CI smoke test so it cannot silently rot.

## Non-goals

- Publishing a sandbox/client image to a registry. Developers build the sandbox image locally; only the service image is published.
- Kubernetes manifests (the top-level README already covers Kubernetes).
- Running the broker on macOS/Windows hosts (RepoWolf is Linux-only; the compose variant still works there because everything runs in Docker's Linux VM).
- Rootless Docker, Podman, or alternative-runtime specifics.
- Multi-principal or multi-repository policy showcases beyond the single example principal.
- Changes to RepoWolf itself: no Go code changes, no new subcommands, no configuration schema changes.

## Prerequisite: first release

The example pins release artifacts that do not exist yet: no git tag, no GitHub release, and no `ghcr.io` image have been published so far. Before the example can work (and before its CI smoke test can pass), tag `v0.1.0` on `main`. The existing release workflow then publishes:

- `repowolf_linux_amd64.tar.gz`, `repowolf_linux_arm64.tar.gz`, and `checksums.txt` on the GitHub release (consumed by the sandbox Dockerfile);
- `ghcr.io/rochecompaan/repowolf:v0.1.0` (consumed by the compose variant and `bootstrap.sh`).

Rollout order: tag `v0.1.0` first, then merge the example. Tagging is a release decision and remains an owner action.

## Decisions

- **Two run modes, one sandbox image.** The sandbox image — the artifact developers actually need to copy — is identical in both modes; only broker placement differs. Weighed a broker-only example and a compose-only example against this split; the split teaches the real topology (host broker) while keeping a zero-install evaluation path (compose).
- **Sandbox image installs `repowolf-client` from the pinned GitHub release** and verifies it against `checksums.txt`. It does not build from source: copy-pasters get the same signed release bits as every other install path.
- **Pinned versions, overridable.** The Dockerfile pins `ARG REPOWOLF_VERSION=v0.1.0`; the compose file defaults `REPOWOLF_IMAGE` to `ghcr.io/rochecompaan/repowolf:v0.1.0`. Both are environment/build-arg overridable, which the CI smoke test uses to test the current commit instead of the release.
- **Bootstrap through the published image.** `bootstrap.sh` runs `repowolf cert init`, `token generate`, and `config validate` via `docker run` against the service image, so the compose path needs nothing installed but Docker. Bootstrap containers run with `--user $(id -u):$(id -g)` so generated state files are owned by the invoking user.
- **TLS via `REPOWOLF_SERVER_NAME`, not cert reissue.** The host-broker path reuses the operator's existing `localhost` certificate; the sandbox connects to the bridge-gateway IP and overrides the TLS server name. No SAN surgery, dogfood-compatible.
- **The example policy omits `actions:read` on purpose.** The supported client surface (`repo view`, `issue *`, `pr *`, `run list/view`, `status view`, git) is otherwise fully granted, so `gh run list` becomes a deterministic, credential-free demonstration of policy denial. The README uses it to teach policy editing ("grant `actions:read` to enable workflow runs").
- **CI smoke test runs unconditionally, not path-filtered.** A broker protocol or policy change in `internal/` can break the example without touching `examples/docker/**`; filtering would skip exactly the runs that matter. The job is cheap because the Nix-built OCI image is already produced in the `test` job's dependency chain.
- **Assertions are behavioral, not YAML-content checks** (repo testing policy): the smoke test drives the real stack and distinguishes policy outcomes from provider outcomes using only a dummy `GH_TOKEN`.

## Directory layout

```
examples/docker/
├── README.md            # canonical walkthrough for both paths
├── sandbox/
│   └── Dockerfile       # the shared core developers copy
├── compose.yaml         # compose variant: repowolf + sandbox services
├── bootstrap.sh         # generates state/ using the service image
├── config/
│   └── repowolf.yaml    # sample strict policy, parameterized by bootstrap.sh
├── .env.example         # GH_TOKEN for the compose broker
└── .gitignore           # state/, .env
```

`state/` is generated, mode `0700`, and never committed: it holds `tls/` (CA, server cert/key), `token` (mode `0600`), and the rendered `config.yaml`.

## Sandbox image

Multi-stage `Dockerfile` (`examples/docker/sandbox/Dockerfile`):

1. **Fetch stage** (`alpine:3`): downloads `repowolf_linux_${TARGETARCH}.tar.gz` and `checksums.txt` from `https://github.com/rochecompaan/repowolf/releases/download/${REPOWOLF_VERSION}`, verifies with `sha256sum -c --ignore-missing`, extracts only `repowolf-client`. `TARGETARCH` is the buildx built-in (`amd64`/`arm64`), which matches the goreleaser archive names directly.
2. **Runtime stage** (`alpine:3`): adds `git` and `ca-certificates`, creates non-root user `65532:65532` (matching the service image), installs `repowolf-client` to `/usr/local/bin`, symlinks `gh` and `repowolf-git-ssh` to it, sets `ENV GIT_SSH_COMMAND=repowolf-git-ssh`, and runs as the non-root user.

Deliberately absent: the real `gh`, OpenSSH, provider tokens, SSH keys, `SSH_AUTH_SOCK`. The sandbox trusts only its broker endpoint, token, and CA certificate, supplied at `docker run` time via the documented client contract: `REPOWOLF_ENDPOINT` (must be an `https` origin), `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, and optionally `REPOWOLF_SERVER_NAME`.

## Host-broker path (primary)

README walkthrough:

1. Install and bootstrap RepoWolf on the host per the top-level README (or use this repository's devenv, which auto-starts a dogfood broker).
2. Configure the broker to listen on the default bridge gateway, e.g. `listen: 172.17.0.1:9443` — reachable from containers but not from the LAN. The README shows how to confirm the address with `ip -4 addr show docker0` and warns against `0.0.0.0`. No certificate reissue is needed when the host certificate already carries a stable DNS SAN (the dogfood setup uses `localhost`); the walkthrough's bootstrap command includes `--dns localhost` for fresh host installs.
3. Build and run the sandbox:

   ```sh
   docker build -t repowolf-sandbox examples/docker/sandbox
   docker run --rm -it \
     -e REPOWOLF_ENDPOINT=https://172.17.0.1:9443 \
     -e REPOWOLF_SERVER_NAME=localhost \
     -e REPOWOLF_TOKEN="$(cat /var/lib/repowolf/token)" \
     -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
     -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
     repowolf-sandbox gh repo view rochecompaan/repowolf
   ```

   `REPOWOLF_SERVER_NAME` must match the host certificate's DNS SAN (`localhost` above, matching the walkthrough's `cert init` flags; operators with a different SAN, such as the `repowolf.internal` example in the top-level README, set it accordingly).

4. Document the `--add-host=host.docker.internal:host-gateway` alternative (`REPOWOLF_ENDPOINT=https://host.docker.internal:9443`, same `REPOWOLF_SERVER_NAME=localhost`).
5. **Boundary proof**, run inside the container: `env | grep GH_TOKEN` finds nothing, `command -v ssh` finds nothing, `readlink "$(command -v gh)"` points at `repowolf-client`, yet `gh repo view` and `git clone` work through the broker.

The token stays out of the image and out of shell history in the walkthrough: it is read from the host's protected state file at run time.

## Compose variant

`bootstrap.sh` (env-overridable: `REPOWOLF_IMAGE`, `REPOWOLF_REPO` as `owner/name`, `STATE_DIR`):

1. Refuses to run if `state/` already exists (mirrors `cert init`'s refusal to clobber).
2. Generates certificates with SANs covering both access routes: `cert init --output state/tls --dns repowolf --dns localhost --ip 127.0.0.1` (`repowolf` is the compose service name; `localhost` covers the published port from the host).
3. Generates the principal token into `state/token` (`umask 077`, never printed).
4. Renders `state/config.yaml` from `config/repowolf.yaml` with the requested `owner/name`, then validates it with `config validate` before any container starts.
5. Creates or updates only the `REPOWOLF_TOKEN_AGENT` line in `.env` (never clobbering the user's `GH_TOKEN` line) and prints next steps.

`compose.yaml` services:

- `repowolf`: `${REPOWOLF_IMAGE:-ghcr.io/rochecompaan/repowolf:v0.1.0}`, command `serve --config /etc/repowolf/repowolf.yaml`, `state/config.yaml` and `state/tls` mounted read-only at the config's paths, `GH_TOKEN` and `REPOWOLF_TOKEN_AGENT` from `.env` with `${VAR:?}` guards so a missing secret fails fast at startup, port published as `127.0.0.1:8443:8443` (loopback only).
- `sandbox`: `build: ./sandbox`, environment `REPOWOLF_ENDPOINT=https://repowolf:8443`, `REPOWOLF_TOKEN` from `.env`, `REPOWOLF_CA_FILE=/run/repowolf/ca.crt`, the CA mounted read-only from `state/tls/ca.crt`, `depends_on: repowolf`. Not a long-running service; used via `docker compose run --rm sandbox gh repo view …`. No published ports.

Git-write operations through the compose broker need the host's SSH agent available to the **broker** service (it holds the service-side OpenSSH). The README documents the optional Linux-only step (mount `SSH_AUTH_SOCK` into the broker service) and notes read-only flows need nothing.

## Sample policy

`config/repowolf.yaml` mirrors the repository's own dogfood policy, with the repository `owner`/`name` rendered by `bootstrap.sh`:

- `listen: "0.0.0.0:8443"` (container-internal; the compose service publishes it on loopback only);
- provider `github-public` (`apiHost`/`gitHost` `github.com`, `sshUser git`);
- git guardrails: `denyRefs: [refs/heads/main]`, `denyDeletes: true`, `maxRefUpdates: 16`;
- principal `agent` with `tokenEnvs: [REPOWOLF_TOKEN_AGENT]`, granted: `repository:read`, `issues:read`, `issues:write`, `pull_requests:read`, `pull_requests:write`, `statuses:read`, `git:read`, `git:write` — **`actions:read` deliberately omitted** (see Decisions);
- the same `limits` block as the dogfood policy.

## CI smoke test

New `docker-example-smoke` job in `.github/workflows/ci.yml` (`needs: test`, same runner, pinned-action style consistent with the existing workflow):

1. Build the broker image from the current commit: `nix build .#ociImage` + `docker load` (yields `repowolf:mvp`).
2. Build the sandbox image from `examples/docker/sandbox/Dockerfile` with its pinned release default — deliberately mixing a release client against a current-commit broker to catch protocol drift.
3. Run `bootstrap.sh` with `REPOWOLF_IMAGE=repowolf:mvp`, `REPOWOLF_REPO=rochecompaan/repowolf`, and dummy `GH_TOKEN` in `.env`.
4. `docker compose up -d repowolf`; wait for TLS on `127.0.0.1:8443`.
5. Assert, driving the real stack with no real credentials:
   - **Granted capability passes policy:** `docker compose run --rm sandbox gh repo view rochecompaan/repowolf` fails, and its output shows an upstream authentication failure (GitHub rejects the dummy token — "Bad credentials"), proving the request passed policy and reached the provider. A policy denial at this step fails the job.
   - **Denied capability stops at policy:** `docker compose run --rm sandbox gh run list` fails with a RepoWolf `permission denied` error, and no provider call is attempted.
   - **Boundary holds:** `docker compose run --rm sandbox env` contains no `GH_TOKEN`; `gh` inside the sandbox resolves to `repowolf-client`; `ssh` is absent.
6. `docker compose down -v` teardown on success or failure.

Exact assertion strings are pinned in the implementation plan after one observed run; the two outcomes are distinguishable today (`PermissionDenied` mapped to `permission denied` in `internal/rpcstatus/status.go`).

## Failure behavior

- `bootstrap.sh` uses `set -euo pipefail`, refuses to overwrite an existing `state/`, validates the rendered config before returning, and never echoes the token (prints its path).
- Compose services fail fast on missing `.env` values via `${VAR:?}` expansion.
- A sandbox run against an unreachable or misnamed broker surfaces the client's existing, clear configuration errors (e.g. `REPOWOLF_ENDPOINT must be an https origin`, TLS name mismatch).
- The README's troubleshooting section covers: TLS server-name mismatch (use `REPOWOLF_SERVER_NAME`), broker not reachable from the container (wrong listen address), and `state/` ownership (regenerate via `bootstrap.sh`).

## Security boundaries

- Provider credentials (`GH_TOKEN`, SSH keys, `SSH_AUTH_SOCK`) exist only on the host or inside the broker container — never in the sandbox image, its environment, or its filesystem.
- `state/` and `.env` are gitignored; the example README repeats the rule that tokens never go into git, remotes, or logs.
- The compose broker publishes its port on loopback only; the host-broker walkthrough binds the Docker bridge gateway, not `0.0.0.0`.
- The token is passed at run time from protected state; it is never baked into an image layer.
- The sample policy ships least-privilege-by-demonstration: full read/write for the one example repository except the deliberately omitted `actions:read`.

## Documentation updates

- `examples/docker/README.md`: what the example shows; prerequisites (Docker with compose v2); quick start via the compose variant; building the sandbox image; the host-broker walkthrough (primary); the boundary proof; editing the policy (grant `actions:read`); git clone/push including the broker-side SSH agent note; troubleshooting; security notes.
- Top-level `README.md`: the OCI section gains one pointer line to `examples/docker/` for a complete walkthrough. No content moves.

## Verification

- New CI job `docker-example-smoke` green on the PR and on `main` after tagging `v0.1.0`.
- Existing CI (`go test -race ./...`, `nix flake check`, OCI smoke, release smoke) unaffected — no Go changes.
- Manual runbook executed once per path and recorded in the PR description: compose variant end-to-end with a real token (read-only flow), and the host-broker path against this repository's devenv dogfood broker.
- Shell lint of `bootstrap.sh` via `bash -n` (and `shellcheck` where available); YAML sanity via `repowolf config validate` inside CI.

## Acceptance criteria

1. A developer with only Docker can clone the repo, run `bootstrap.sh` and `docker compose up`, and complete a brokered `gh repo view` against their chosen repository.
2. A developer with RepoWolf on their host can build the sandbox image and reach their broker from a container using only the four documented client environment variables.
3. Inside the sandbox container, no provider credentials, real `gh`, or `ssh` exist; `gh` and git SSH are brokered.
4. A denied capability (`gh run list` under the sample policy) fails with a policy error without contacting GitHub; a granted capability with a bad provider token fails upstream, not at policy.
5. `examples/docker/state/`, `.env`, and token material can never be committed (gitignored, verified by `git check-ignore`).
6. The CI smoke test exercises all of 1–4 on every PR and `main` push.
