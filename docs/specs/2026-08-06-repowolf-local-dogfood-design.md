# RepoWolf Local Dogfood Service Design

**Date:** 2026-08-06
**Status:** Approved

## Goal

Running `devenv shell` (or direnv activation) in this repository automatically bootstraps and starts a loopback RepoWolf service that brokers this repository's own Git and GitHub operations. Inside the shell, `git` uses `repowolf-git-ssh` and `gh` is the restricted RepoWolf client, so everyday development on `rochecompaan/repowolf` flows through the broker exactly as it would for a sandboxed agent.

## Non-goals

This change does not:

- modify RepoWolf production code, packages, or flake outputs;
- change the repository's `origin` remote or any Git configuration;
- run anything in CI (devenv files are not part of flake outputs; `nix flake check` is only a regression check);
- persist GitHub credentials anywhere;
- support multiple repositories, principals, or machines;
- add systemd/Home Manager deployment, Docker, or Kubernetes configuration;
- push the local `main` branch or publish anything.

The devenv shell intercepts `PATH` for convenience but is **not a security boundary**: the canonical `gh`/`ssh` and provider credentials remain reachable outside the broker, including from inside the shell via absolute paths. Enforcing isolation with a bubblewrap jail (for example `jail.nix`) around agent execution is deferred follow-up work with its own spec, consumers, and breakout verification.

## Decisions

| Topic | Decision |
|---|---|
| GitHub API credential | Host `gh auth token` at service start; held only in the service process environment |
| Git SSH authentication | Inherited host `SSH_AUTH_SOCK` (user's GitHub SSH key) |
| Principal capabilities | Full set on `rochecompaan/repowolf` only: repository/issues/pull-requests/actions/statuses read, issues/pull-requests write, git read/write |
| Activation | Fully automatic on `devenv shell`; no manual start step |
| Token/cert lifecycle | Bootstrap once; manual `dogfood-reset` deletes state and re-bootstraps on next entry |
| Supervision | devenv-native `processes.repowolf` with readiness probe and `on_failure` restart |

## State layout

All mutable state lives under `.devenv/repowolf/`, already covered by the `.devenv*` ignore rule. Nothing in this section is committed.

- `.devenv/repowolf/tls/` — output of `repowolf cert init --dns localhost --ip 127.0.0.1`, producing `ca.crt` (0644), `ca.key` (0600), `tls.crt` (0644), `tls.key` (0600). Directory mode 0700.
- `.devenv/repowolf/token` — one generated bearer token, mode 0600.
- `.devenv/repowolf/config.yaml` — rendered strict service policy. Contains paths and policy only; never token values or provider credentials.
- Audit JSONL is emitted on the service's stdout and captured by the devenv process manager under `.devenv` state.

## Bootstrap task

A devenv task `repowolf:bootstrap` runs before `devenv:enterShell` and is idempotent through a `status` check (all state files present, token readable, config validates).

On a cold or reset state it:

1. Creates `.devenv/repowolf/` with mode 0700.
2. Runs `repowolf cert init --output .devenv/repowolf/tls --dns localhost --ip 127.0.0.1` (fails safely if the output exists).
3. Runs `repowolf token generate`, writes the value to `.devenv/repowolf/token` with mode 0600, and never prints it.
4. Renders `.devenv/repowolf/config.yaml` (below) with absolute paths substituted.
5. Verifies `${pkgs.gh}/bin/gh auth token` returns a non-empty value as a pre-flight (the service wrapper re-reads it at every process start).
6. Runs `repowolf config validate --config .devenv/repowolf/config.yaml`. Bootstrap fails the shell entry with a clear message if any step fails, including the `gh auth token` pre-flight.

## Service policy

Rendered `config.yaml` shape (no secrets):

```yaml
apiVersion: repowolf.dev/v1alpha1
listen: 127.0.0.1:9443
tls:
  certificate: <state>/tls/tls.crt
  privateKey: <state>/tls/tls.key
tools:
  gh: ${pkgs.gh}/bin/gh          # real GitHub CLI, pinned by Nix
  ssh: ${pkgs.openssh}/bin/ssh   # real OpenSSH client, pinned by Nix
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
```

Pinning `tools.gh`/`tools.ssh` is required because the devenv `PATH` deliberately resolves `gh` to the restricted RepoWolf client; a `null` tool path would resolve the wrong binary. The push policy keeps the built-in `main` protection and rejects deletes.

## Service process

`processes.repowolf`:

- Exec wrapper: exports `GH_TOKEN="$(${pkgs.gh}/bin/gh auth token)"`, exports `REPOWOLF_TOKEN_AGENT` by reading `.devenv/repowolf/token`, verifies both are non-empty, then `exec repowolf serve --config .devenv/repowolf/config.yaml`. The token and `GH_TOKEN` exist only in the process environment; RepoWolf's runner already strips every `REPOWOLF_*` variable from provider subprocesses, so the agent token can never reach `gh` or `ssh`.
- Inherits the interactive session's `SSH_AUTH_SOCK` so provider `ssh` uses the user's agent. If `SSH_AUTH_SOCK` is unset at start, the service still starts (GitHub API operations work; Git operations will fail) and the enterShell hook prints a warning.
- Listens on `127.0.0.1:9443`; restart policy `on_failure`.
- Readiness: TCP probe on 127.0.0.1:9443. RepoWolf also exposes standard gRPC health over TLS, but TCP is sufficient for lifecycle; brokered smoke tests are the behavioral proof.

## Automatic start

`devenv shell` does not start processes by itself. The `enterShell` hook therefore:

1. Probes `127.0.0.1:9443` with bash `/dev/tcp`.
2. If unreachable, launches `nohup repowolf-dogfood.sh autostart` in the background (log under `.devenv`). The `autostart` subcommand first closes every inherited fd ≥ 3 and redirects stdin from `/dev/null` before running `devenv up -d repowolf`; redirecting only stdout/stderr leaks caller pipes (for example direnv's `.envrc` capture pipe, inherited as an ExtraFile on fd 3) into the long-lived process daemon, which deadlocks the caller's `Wait()` forever. This mechanism was prototyped on devenv 2.1.2: the launch is deduplicated ("Already running or waiting"), does not recurse, and a warm-cache service is healthy within about 10 seconds. The first-ever activation additionally builds the process manager closure and can take minutes; the hook prints a notice that the broker is starting in the background.
3. Exports the client environment:
   - `REPOWOLF_ENDPOINT=https://localhost:9443`
   - `REPOWOLF_TOKEN` read from `.devenv/repowolf/token` (file is 0600; value exists only in the shell environment)
   - `REPOWOLF_CA_FILE=$DEVENV_ROOT/.devenv/repowolf/tls/ca.crt`
   - `GIT_SSH_COMMAND=repowolf-git-ssh`

`REPOWOLF_SERVER_NAME` is unnecessary because the certificate carries DNS `localhost` and IP `127.0.0.1` SANs.

## Routing behavior inside the shell

- `git fetch/push/clone` against `git@github.com:rochecompaan/repowolf.git` invoke `repowolf-git-ssh`, which relays over TLS to the broker; the broker runs real OpenSSH with the inherited agent.
- `gh issue/pr/repo/run/api` commands resolve to the restricted client already installed by the devenv environment, which calls the broker's GitHub service; the broker runs real `gh` with `GH_TOKEN`.
- Scripts inside the RepoWolf tooling (bootstrap, process wrapper) always call the Nix-pinned real `gh` by absolute path, so they never hit the restricted client.
- Direct broker calls without the token fail with the existing unauthenticated path — verified in smoke tests.

## Reset

A devenv script `dogfood-reset`:

1. Stops the `repowolf` process (`devenv processes stop repowolf`, tolerating not-running).
2. Deletes `.devenv/repowolf/`.
3. Prints that the next `devenv shell` will re-bootstrap fresh TLS, token, and policy.

Rotation is manual-only; nothing regenerates automatically.

## Failure behavior

- Bootstrap failure (cert/token/config/`gh auth token`): shell entry fails with the failing step's message; no partial state is trusted (cert init is atomic; config is re-rendered and re-validated each bootstrap).
- Service crash: devenv restarts it per `on_failure`; a crashed-at-entry service is reported by the next shell's probe plus a background relaunch.
- Missing `SSH_AUTH_SOCK`: warning only.
- Port 9443 already bound by something else: service start fails; probe + process logs surface it; user resolves or runs `dogfood-reset`.
- No fallback to mutable host installs, direct tokens in files, or weakened policy at any point.

## Security boundaries

- No secret is committed; `.devenv*` remains ignored.
- `GH_TOKEN` and both RepoWolf tokens exist only in process/shell environments and the 0600 token file.
- The broker's own protections apply to this repo's traffic: exact repository grant, `main` push denial, delete denial, bounded concurrency and byte limits, sanitized audit JSONL.
- The restricted client PATH interception is intentional dogfooding, not a bypass; provider credentials live only service-side.

## Verification

This is development-environment orchestration, so the Testing Value Gate does not justify automated Go tests; behavior is proven directly:

1. Bootstrap runs twice: second run is a no-op (`status` short-circuit).
2. `repowolf config validate` passes on the rendered config.
3. After entry, TCP readiness on 9443 and `devenv processes list` show `repowolf` ready.
4. Broker-routed API: `gh repo view rochecompaan/repowolf` succeeds through the restricted client.
5. Broker-routed Git: `git ls-remote origin` succeeds with `GIT_SSH_COMMAND=repowolf-git-ssh`.
6. Audit JSONL in process logs shows accepted/completed events and no token or `GH_TOKEN` values.
7. Unauthenticated probe: a TLS client call without `REPOWOLF_TOKEN` fails.
8. `dogfood-reset` followed by re-entry produces a fresh healthy stack with new token/cert.
9. `nix flake check --accept-flake-config --print-build-logs` passes unchanged (regression).

## Acceptance criteria

- Entering the repo with devenv/direnv yields a running broker and exported client environment with no manual steps.
- Git and GitHub operations on `rochecompaan/repowolf` route through the broker by default.
- Real provider credentials are never persisted and never leave the service process environment.
- `dogfood-reset` reliably returns the environment to a fresh state.
- Existing `nix develop` and `nix flake check` behavior is unaffected.
