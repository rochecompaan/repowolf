# RepoWolf migration handoff for `roche-pi` and clubhouse

This is a rollback-safe handoff for a **later migration plan**. It does not authorize or perform changes in `roche-pi`, `clubhouse-infra`, or a local clubhouse checkout. The embedded jailed GitHub broker remains the production path until every pre-cutover gate below passes.

## Immutable source baseline

Review the later `roche-pi` changes against exactly:

- repository: `ssh://git@git.compaan/roche/pi-config.git`
- commit: `5b8425c8663a4c7bc7c79a7188743ec464eaba02`

The later plan must inspect these paths at that commit, never silently substitute the external checkout's current worktree:

```text
roche-pi:nix/lib/mk-jailed-pi.nix
roche-pi:nix/lib/jailed-github-broker-lifecycle.nix
roche-pi:nix/packages/jailed-github-broker.nix
roche-pi:modules/checks/jailed-github-broker-real-jail.nix
```

Verify every anchor before reading it:

```sh
ROCHE_PI_SOURCE=/absolute/path/to/roche-pi
PIN=5b8425c8663a4c7bc7c79a7188743ec464eaba02
for path in \
  nix/lib/mk-jailed-pi.nix \
  nix/lib/jailed-github-broker-lifecycle.nix \
  nix/packages/jailed-github-broker.nix \
  modules/checks/jailed-github-broker-real-jail.nix
do
  git -C "$ROCHE_PI_SOURCE" cat-file -e "$PIN:$path"
  git -C "$ROCHE_PI_SOURCE" show "$PIN:$path"
done
```

## Clubhouse operator input

The clubhouse `devenv.nix` reviewed during design is ignored and untracked. It is an operator-supplied input, not an immutable source anchor and not recoverable from `clubhouse-infra` Git history. Before drafting the later migration plan, the operator must provide its absolute location and exact content explicitly. The plan must record when that content was supplied and must not attribute it to any Git commit.

Do not proceed if this input is missing, has been inferred from another checkout, or has been described as tracked history.

## Pre-cutover contract

### Standalone RepoWolf readiness

Before touching either consumer:

1. Pin a reviewed RepoWolf revision and build its separate `repowolf` and `repowolf-client` outputs.
2. Run the full RepoWolf standalone verification in `docs/verification/mvp.md`, including the real Bubblewrap check, offline Git allowed/denied-main flow, closure inspection, leak scans, race suite, full flake check, archives, and OCI smoke.
3. Resolve adversarial review findings against `docs/specs/2026-08-01-repowolf-mvp-design.md`.
4. Retain the current embedded broker package, lifecycle, socket integration, and real-jail check unchanged as the rollback implementation.

### Trusted-parent certificate contract

For a local CA, an operator must first create an existing private parent directory that untrusted principals cannot write while initialization runs. `repowolf cert init --output` must name a **nonexistent child** of that trusted parent:

```sh
install -d -m 0700 /var/lib/repowolf
repowolf cert init \
  --output /var/lib/repowolf/tls \
  --dns repowolf.internal \
  --ip 127.0.0.1
```

Do not pre-create `/var/lib/repowolf/tls`. Any existing file, directory, or symlink at the destination is a hard failure and must remain untouched. Keep the CA private key and server private key service-side. Mount or distribute only the public CA certificate to the jail. The endpoint hostname or explicit server name must match a certificate SAN. Certificate, token, policy, and executable changes take effect only after a service restart.

Generate one role token with `repowolf token generate`. Store the value outside Nix and Git in a protected runtime environment file. Strict policy YAML names its service environment variable; it never contains the token value or digest. The jail receives that same role value only as `REPOWOLF_TOKEN`.

## Staged migration checklist

Perform these stages as separate reviewable changes. Do not combine cutover with embedded-broker removal.

### Stage 1 — deploy the host service without changing the jail

- Add the pinned RepoWolf service/admin package to Home Manager.
- Define a Home Manager systemd user service for `repowolf serve` with the strict policy path, protected runtime environment file, server certificate/key, and service-side provider authentication.
- Keep real `gh`, OpenSSH, provider configuration, SSH keys, provider tokens, and `SSH_AUTH_SOCK` service-side only.
- Start and restart the unit; verify TLS readiness, authenticated health through a typed read, safe JSONL audit collection, failure/restart behavior, and complete process cleanup.
- Leave the current embedded broker enabled and serving the existing jail.

### Stage 2 — add a parallel RepoWolf jail path

Update the later design around `mk-jailed-pi.nix` and its real-jail check so the parallel path:

- adds only the `repowolf-client` runtime closure, with its restricted `gh` and `repowolf-git-ssh` links;
- puts restricted `gh` first in the jail command path;
- shares network access to the HTTPS endpoint;
- mounts the public CA read-only;
- provides `REPOWOLF_ENDPOINT`, the role value as `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, and `GIT_SSH_COMMAND=.../repowolf-git-ssh`;
- does not mount the embedded broker socket into the RepoWolf test path;
- does not expose real host `gh`, `ssh`, provider config, keys, `SSH_AUTH_SOCK`, the RepoWolf server package, service policy, service token environment names, or private TLS material.

Keep the existing broker option and launcher available so an operator can select either path without rebuilding or deleting rollback code.

### Stage 3 — parity and leak gates

Run both implementations against the same approved repository/capability matrix. Record exact commands and results for:

- every currently approved restricted `gh` read and write operation;
- identical fail-closed behavior for unsupported commands, repositories, and capabilities;
- real Git fetch through each implementation;
- approved non-main Git writes without force;
- offline exact-main denial with zero client update bytes reaching the fake host;
- stdin and TTY behavior;
- cancellation, signal forwarding, no surviving service/client/provider process, and no runtime residue;
- closure and jail-filesystem inspection;
- unique-marker scans of client stdout/stderr, service errors, audit JSONL, provider argv/stdin/stdout/stderr/environment, SSH channels, pack data, tokens, credentials, configuration paths, and private keys.

Any semantic difference, leak, missing current operation, process residue, or checkout/ref mutation blocks cutover.

### Stage 4 — authenticated smoke before default cutover

Using an approved disposable non-main branch:

1. run an authenticated repository read through restricted `gh`;
2. run a real authenticated `git fetch`;
3. push one approved non-main update without force;
4. verify the intended remote ref and audit records;
5. verify main remains protected by policy and do not probe a real forge main with a denial test.

Use the generated offline fake host—not the real forge—to retain the exact-main zero-forwarding proof.

### Stage 5 — cut over with rollback armed

- Change only the clubhouse selection/default from embedded broker to RepoWolf.
- Keep the previous embedded-broker package, lifecycle code, socket option, configuration, and last-known-good generation available.
- Activate one controlled Home Manager generation.
- Repeat the authenticated read/fetch/non-main-write smoke and leak/process checks immediately.
- Observe service restart and audit behavior for the agreed soak period.

## Rollback

Rollback is required for failed readiness, authentication/TLS regressions, parity differences, unexpected denials, leak evidence, provider-tool exposure, process residue, or audit gaps.

1. Stop new clubhouse work and do not retry writes blindly.
2. Switch the clubhouse selection back to the retained embedded broker configuration or activate the last-known-good Home Manager generation.
3. Restart the jail/session so no RepoWolf endpoint, token, CA, client path, or `GIT_SSH_COMMAND` remains in the new process.
4. Verify the embedded broker socket/lifecycle and its real-jail check, then repeat an authenticated read and fetch through the old path.
5. Stop or disable the RepoWolf user service if it contributed to the failure; preserve sanitized service/audit diagnostics and never copy tokens, credentials, bodies, pack data, or private keys into the incident record.
6. Confirm no RepoWolf or Bubblewrap child survives and no test checkout/ref was mutated unexpectedly.
7. Revert only the cutover selection. Keep the independent RepoWolf deployment/change available for diagnosis unless its presence is itself unsafe.

Rollback does not include deleting embedded-broker code. Removal of `jailed-github-broker-lifecycle.nix`, `jailed-github-broker.nix`, socket wiring, or the existing real-jail check is a separate later change, allowed only after stable operation, fresh parity/leak evidence, and explicit review approval.
