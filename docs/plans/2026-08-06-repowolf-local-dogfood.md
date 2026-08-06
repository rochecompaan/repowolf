# RepoWolf Local Dogfood Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `devenv shell` in this repository automatically bootstrap and start a loopback RepoWolf broker and route this repo's own Git/GitHub operations through it.

**Architecture:** A single bash tool (`scripts/repowolf-dogfood.sh`) owns bootstrap, service startup, reset, status, and probe logic against state in `.devenv/repowolf/`. devenv wires it as a pre-entry task, a supervised native process with TCP readiness and `on_failure` restart, a reset script, and an `enterShell` hook that exports the client environment and launches the process manager in the background when the broker is unreachable.

**Tech Stack:** devenv 2.x (native process manager, tasks), bash, RepoWolf Nix packages, host `gh auth token`, host `SSH_AUTH_SOCK`

## Global Constraints

- Spec: `docs/specs/2026-08-06-repowolf-local-dogfood-design.md`. Follow its policy YAML, capability set, port (`127.0.0.1:9443`), and security boundaries exactly.
- All mutable state lives under `$DEVENV_ROOT/.devenv/repowolf/` (covered by the existing `.devenv*` ignore rule). Never commit state, tokens, certificates, or credentials.
- Token file mode must be 0600; state and TLS directories mode 0700; config contains paths/policy only, never token values.
- The GitHub API credential comes from `"$(${pkgs.gh}/bin/gh auth token)"` at each service start; it is never written to disk.
- The service inherits `SSH_AUTH_SOCK`; missing agent warns but does not block.
- Service config pins real tools from Nix (`${pkgs.gh}/bin/gh`, `${pkgs.openssh}/bin/ssh`) because devenv `PATH` deliberately resolves `gh` to the restricted client.
- Principal `agent` gets the full capability set on `rochecompaan/repowolf` only; `refs/heads/main` push and all deletes stay denied.
- No RepoWolf production code, flake outputs, CI workflows, Git remotes, or README changes. No jail/bubblewrap work (explicitly deferred in the spec).
- Testing Value Gate: this is development-environment orchestration. Do not add Go tests; prove behavior with the direct verification steps below.
- All `devenv shell --` invocations run the `repowolf:bootstrap` task first (it is `before = [ "devenv:enterShell" ]`), so later steps can assume state exists.

---

### Task 1: Bootstrap tooling and devenv task

**Files:**
- Create: `scripts/repowolf-dogfood.sh`
- Modify: `devenv.nix`

**Interfaces:**
- Consumes: RepoWolf packages already in the devenv (`repowolf` on PATH), `${pkgs.gh}`, `${pkgs.openssh}`.
- Produces (Task 2 relies on these): subcommands `bootstrap`, `status`, `serve`, `reset`, `probe`; state layout `$DEVENV_ROOT/.devenv/repowolf/{tls,token,config.yaml}`; env vars `REPOWOLF_DOGFOOD_REAL_GH`, `REPOWOLF_DOGFOOD_REAL_SSH`; task `repowolf:bootstrap` that runs before every shell entry.

- [ ] **Step 1: Record the missing-tooling baseline**

Run:

```bash
test ! -e scripts/repowolf-dogfood.sh
if devenv tasks run repowolf:bootstrap >/tmp/dogfood-task1-red.log 2>&1; then
  echo "unexpected existing repowolf:bootstrap task" >&2
  exit 1
fi
```

Expected: PASS. The script does not exist and the task run fails because `repowolf:bootstrap` is undefined.

- [ ] **Step 2: Write the dogfood tool script**

Create `scripts/repowolf-dogfood.sh` with exactly this content:

```bash
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
```

- [ ] **Step 3: Make it executable and syntax-check**

Run:

```bash
chmod +x scripts/repowolf-dogfood.sh
bash -n scripts/repowolf-dogfood.sh
test "$(scripts/repowolf-dogfood.sh 2>&1 || true)" = "usage: repowolf-dogfood <bootstrap|status|serve|reset|probe>"
```

Expected: all exit 0; bare invocation prints usage and exits 2.

- [ ] **Step 4: Wire env pins and the bootstrap task into devenv.nix**

Append to `devenv.nix`, inside the module's attribute set after the existing `packages = [ ... ];` block:

```nix
  env.REPOWOLF_DOGFOOD_REAL_GH = "${pkgs.gh}/bin/gh";
  env.REPOWOLF_DOGFOOD_REAL_SSH = "${pkgs.openssh}/bin/ssh";

  tasks."repowolf:bootstrap" = {
    exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} bootstrap";
    status = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} status";
    before = [ "devenv:enterShell" ];
  };
```

- [ ] **Step 5: Parse, stage, and evaluate**

Run:

```bash
nix-instantiate --parse devenv.nix >/dev/null
git add devenv.nix scripts/repowolf-dogfood.sh
git diff --cached --check
devenv shell -- true
```

Expected: all exit 0. The shell entry runs the bootstrap task; output shows `repowolf:bootstrap` succeeded.

- [ ] **Step 6: Verify generated state, modes, and config**

Run:

```bash
state="$PWD/.devenv/repowolf"
test "$(stat -c %a "$state")" = 700
test "$(stat -c %a "$state/tls")" = 700
for f in tls/ca.crt tls/tls.crt tls/tls.key token config.yaml; do test -f "$state/$f"; done
test "$(stat -c %a "$state/token")" = 600
test "$(stat -c %a "$state/tls/tls.key")" = 600
devenv shell -- repowolf config validate --config .devenv/repowolf/config.yaml | grep -qx "configuration valid"
grep -qx "listen: 127.0.0.1:9443" "$state/config.yaml"
! grep -qF "$(cat "$state/token")" "$state/config.yaml"
```

Expected: all exit 0. Config validates, policy listens on the loopback port, and the token value never appears in the config.

- [ ] **Step 7: Prove task idempotency**

Run:

```bash
state="$PWD/.devenv/repowolf"
before=$(sha256sum "$state/token" | cut -d' ' -f1)
out=$(devenv tasks run repowolf:bootstrap 2>&1)
after=$(sha256sum "$state/token" | cut -d' ' -f1)
test "$before" = "$after"
! grep -q "bootstrap complete" <<<"$out"
```

Expected: all exit 0. The `status` check short-circuits the second run; the token is unchanged and the bootstrap body did not re-execute.

- [ ] **Step 8: Prove the gh-token pre-flight failure path, then restore**

Run:

```bash
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
printf '#!/usr/bin/env bash\nexit 0\n' >"$tmp/fake-gh"; chmod +x "$tmp/fake-gh"
set +e
out=$(REPOWOLF_DOGFOOD_REAL_GH="$tmp/fake-gh" bash scripts/repowolf-dogfood.sh bootstrap 2>&1)
rc=$?
set -e
test "$rc" -ne 0
grep -q "auth token returned empty" <<<"$out"
devenv shell -- bash scripts/repowolf-dogfood.sh bootstrap >/dev/null
devenv shell -- repowolf config validate --config .devenv/repowolf/config.yaml | grep -qx "configuration valid"
```

Expected: the fake-gh run fails with the clear message; the immediately following direct script invocation (bypassing the task's `status` cache) re-renders the config with the pinned real `gh` path and validates.

- [ ] **Step 9: Commit Task 1**

Run:

```bash
git add devenv.nix scripts/repowolf-dogfood.sh
test "$(git diff --cached --name-only | LC_ALL=C sort | tr '\n' ' ')" = "devenv.nix scripts/repowolf-dogfood.sh "
git diff --cached --check
git commit -m "chore(devenv): add dogfood bootstrap tooling"
```

Expected: one commit with exactly those two files; `git status --short` clean afterwards.

---

### Task 2: Supervised broker, automatic start, and client routing

**Files:**
- Modify: `devenv.nix`

**Interfaces:**
- Consumes (from Task 1): subcommands `serve`, `reset`, `probe`; state under `.devenv/repowolf/`; env pins; the `repowolf:bootstrap` task.
- Produces: `processes.repowolf` (supervised, TCP readiness, `on_failure` restart); `dogfood-reset` command; `enterShell` exports `REPOWOLF_ENDPOINT`, `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, `GIT_SSH_COMMAND`; background auto-start when the broker is unreachable.

- [ ] **Step 1: Record the missing-runtime baseline**

Run:

```bash
if devenv shell -- bash -c 'test -n "${REPOWOLF_ENDPOINT:-}"' >/tmp/dogfood-task2-red.log 2>&1; then
  echo "unexpected REPOWOLF_ENDPOINT export" >&2
  exit 1
fi
if devenv processes list >/tmp/dogfood-task2-red-processes.log 2>&1; then
  echo "unexpected running process manager" >&2
  exit 1
fi
```

Expected: PASS. No endpoint export and no process manager exist yet. (Bootstrap runs during these entries, which is fine.)

- [ ] **Step 2: Add the process, reset script, and enterShell wiring**

Append to `devenv.nix` after the `tasks."repowolf:bootstrap"` block:

```nix
  processes.repowolf = {
    exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} serve";
    restart.on = "on_failure";
    ready.exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} probe";
  };

  scripts.dogfood-reset.exec = "${pkgs.bash}/bin/bash ${./scripts/repowolf-dogfood.sh} reset";

  enterShell = ''
    export REPOWOLF_ENDPOINT="https://localhost:9443"
    export REPOWOLF_TOKEN="$(cat "$DEVENV_ROOT/.devenv/repowolf/token")"
    export REPOWOLF_CA_FILE="$DEVENV_ROOT/.devenv/repowolf/tls/ca.crt"
    export GIT_SSH_COMMAND="repowolf-git-ssh"
    if [ -z "''${SSH_AUTH_SOCK:-}" ]; then
      echo "repowolf-dogfood: warning: SSH_AUTH_SOCK is unset; brokered Git operations will fail" >&2
    fi
    if ! (exec 3<>/dev/tcp/127.0.0.1/9443) 2>/dev/null; then
      echo "repowolf-dogfood: starting broker in the background (first activation may take a few minutes)"
      nohup devenv up -d repowolf >"$DEVENV_ROOT/.devenv/repowolf/autostart.log" 2>&1 &
    fi
  '';
```

- [ ] **Step 3: Parse, stage, and evaluate; wait for readiness**

Run:

```bash
nix-instantiate --parse devenv.nix >/dev/null
git add devenv.nix
git diff --cached --check
devenv shell -- true
ready=""
for i in $(seq 1 36); do
  if (exec 3<>/dev/tcp/127.0.0.1/9443) 2>/dev/null; then ready="after $((i*5))s"; break; fi
  sleep 5
done
test -n "$ready"
echo "broker ready $ready"
```

Expected: all exit 0 and a readiness line is printed within 180 seconds (warm cache; first-ever process-manager build may take longer — if the loop times out only because of building, rerun it once before investigating).

- [ ] **Step 4: Verify client exports and process state**

Run:

```bash
devenv shell -- bash -euc '
  test "$REPOWOLF_ENDPOINT" = "https://localhost:9443"
  test -n "$REPOWOLF_TOKEN"
  test "$REPOWOLF_CA_FILE" = "$DEVENV_ROOT/.devenv/repowolf/tls/ca.crt"
  test "$GIT_SSH_COMMAND" = "repowolf-git-ssh"
  test "$(command -v gh)" != "$REPOWOLF_DOGFOOD_REAL_GH"
  command -v repowolf-git-ssh >/dev/null
'
devenv processes list 2>&1 | grep -E "repowolf\s+ready"
```

Expected: all exit 0. The shell's `gh` is the restricted client (not the pinned real one), and the manager reports `repowolf` ready.

- [ ] **Step 5: Prove broker-routed GitHub API access**

Run:

```bash
devenv shell -- gh repo view rochecompaan/repowolf
```

Expected: exit 0. The restricted client resolves the repository through the broker, which calls the real `gh` with the host's `GH_TOKEN`.

- [ ] **Step 6: Prove broker-routed Git access**

Run:

```bash
devenv shell -- git ls-remote origin HEAD
```

Expected: exit 0 and a line beginning with a commit hash followed by `HEAD`. Git used `GIT_SSH_COMMAND=repowolf-git-ssh`; the broker ran real OpenSSH with the inherited `SSH_AUTH_SOCK`.

- [ ] **Step 7: Prove audit events exist and no credential leaks into state**

Run (token values are only ever compared, never printed):

```bash
token=$(cat "$PWD/.devenv/repowolf/token")
grep -rlI '"outcome":"accepted"' "$PWD/.devenv" | grep -q .
token_files=$(grep -rlIF "$token" "$PWD/.devenv" || true)
test "$token_files" = "$PWD/.devenv/repowolf/token"
gh_token=$(gh auth token)
test -z "$(grep -rlIF "$gh_token" "$PWD/.devenv" || true)"
```

Expected: all exit 0. Audit JSONL under `.devenv` contains accepted events; the agent token appears only in its 0600 file; the GitHub token appears nowhere on disk.

- [ ] **Step 8: Prove unauthenticated calls are rejected**

Run:

```bash
if devenv shell -- bash -c 'REPOWOLF_TOKEN="$(repowolf token generate)" gh repo view rochecompaan/repowolf' >/tmp/dogfood-unauth.log 2>&1; then
  echo "broker accepted an unknown bearer token" >&2
  exit 1
fi
```

Expected: PASS. A well-formed but unknown token fails against the broker's authenticated admission.

- [ ] **Step 9: Prove reset and fresh re-bootstrap**

Run:

```bash
state="$PWD/.devenv/repowolf"
old=$(cat "$state/token")
devenv shell -- dogfood-reset
test ! -e "$state"
devenv shell -- true
for i in $(seq 1 36); do
  if (exec 3<>/dev/tcp/127.0.0.1/9443) 2>/dev/null; then break; fi
  sleep 5
done
(exec 3<>/dev/tcp/127.0.0.1/9443) 2>/dev/null
new=$(cat "$state/token")
test "$old" != "$new"
devenv shell -- gh repo view rochecompaan/repowolf >/dev/null
```

Expected: all exit 0. Reset removes state and stops the process; the next entry re-bootstraps with a new token and the broker is healthy and functional again.

- [ ] **Step 10: Run the Nix regression and hygiene checks**

Run:

```bash
nix flake check --accept-flake-config --print-build-logs
nix-instantiate --parse devenv.nix >/dev/null
test -z "$(git status --short --untracked-files=all | grep -E '(\.devenv|\.direnv)' || true)"
test "$(git status --short | LC_ALL=C sort | tr '\n' ' ')" = "M devenv.nix "
```

Expected: all exit 0. Flake checks pass unchanged; generated state stays untracked; only `devenv.nix` is modified.

- [ ] **Step 11: Commit Task 2**

Run:

```bash
git add devenv.nix
git diff --cached --check
git commit -m "chore(devenv): run local dogfood broker"
```

Expected: one commit modifying only `devenv.nix`; clean tracked state afterwards.

- [ ] **Step 12: Run the post-commit smoke verification**

Run:

```bash
devenv shell -- bash -euc '
  test "$REPOWOLF_ENDPOINT" = "https://localhost:9443"
  gh repo view rochecompaan/repowolf >/dev/null
  git ls-remote origin HEAD | grep -q "HEAD$"
'
test -z "$(git status --short)"
```

Expected: all exit 0 on the committed head, tracked worktree clean.
