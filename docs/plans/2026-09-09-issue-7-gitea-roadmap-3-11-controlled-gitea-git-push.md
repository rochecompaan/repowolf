# Controlled Gitea Git Push Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow an authorized agent to push policy-compliant commits to a permitted Gitea repository through RepoWolf while denied pushes forward no update bytes and credentials remain isolated.

**Architecture:** Extend only the trusted-provider allowlist used by the existing shared receive-pack command builder; keep capability resolution, protocol validation, push policy, bounded relay, runner cleanup, terminal categories, and audit lifecycle provider-neutral. Prove the behavior first at the Git service boundary, then through real Git with fake SSH, and finally against the existing digest-pinned Gitea fixture.

**Tech Stack:** Go, gRPC bidirectional streams, Git smart protocol over SSH, the existing RepoWolf policy/audit/runner packages, real Git CLI integration tests, Docker, and digest-pinned Gitea.

**Spec:** `docs/specs/2026-09-09-issue-7-gitea-roadmap-3-11-controlled-gitea-git-push-design.md`

## Global Constraints

- Execute this plan from a base containing Issue #6, including `internal/policy.ResolveGit`, the additive `ssh_user` selector, Gitea upload-pack support, `internal/testutil/gitea.go`, and `integration/gitea_test.go`; the inspected predecessor tip is `4fe0ea2`.
- Keep `apiVersion` at `repowolf.dev/v1alpha1`; add no Protobuf field, service, or wire form.
- Permit receive-pack only for trusted providers of kind `github` or `gitea`.
- Require both `git:read` and `git:write` and require both resolutions to return the same trusted repository ID.
- Build the SSH executable, user, host, port, owner, repository, and remote command only from trusted configuration.
- Keep `denyRefs`, `denyDeletes`, and `maxRefUpdates` provider-independent; do not change `internal/policy/push.go` or the configuration model.
- Forward zero client request bytes when receive-pack parsing or local push policy rejects a request.
- Preserve the existing initial-frame, idle-stream, operation, prefix, directional-byte, stderr, ref-count, global-concurrency, and per-principal limits.
- Preserve process-group cleanup, terminal categories, accepted-before-process audit order, exactly one terminal Git audit event, safe audit metadata, and the existing delivery-versus-audit error contract.
- Keep Gitea provider tokens, principal tokens, unrelated provider credentials, provider stderr, pack data, object IDs, generated argv, and commands out of client errors and audit records.
- Keep existing GitHub clone, fetch, and push behavior and Issue #6 Gitea clone/fetch behavior unchanged.
- Reuse the one digest-pinned Gitea image and fixture from Issue #6; add no version matrix, Gitea API production code, SDK, `tea`, HTTPS Git, credential provisioning, or deployment documentation.
- Apply the Testing Value Gate to every test below: each exercises security-sensitive authorization, policy, cancellation, byte-forwarding, audit, or real-provider behavior and can fail on a meaningful regression. Do not add tests that merely inspect workflow YAML or static policy text; verify CI syntax through the behavioral CI run and `git diff --check` instead.

## File Structure

### Modified files

- `internal/gitservice/receive.go`: admit trusted Gitea repositories to the existing two-capability receive-pack path.
- `internal/gitservice/receive_test.go`: prove Gitea read/write authorization, trusted argv, provider-independent policy, zero-byte denial, safe audit, and cancellation cleanup.
- `integration/testdata/policy.yaml`: grant the existing Gitea repository write capability and add an allowed and denied ref policy for the push demonstration.
- `integration/testdata/fake-ssh.sh`: route Gitea receive-pack to the existing Gitea bare repository and record its input separately.
- `integration/git_test.go`: replace the Issue #6 Gitea-push denial with allowed and locally denied real-Git pushes through the same broker.
- `integration/audit_assertions_test.go`: describe exact Gitea upload/receive accepted and terminal audit sequences.
- `integration/leak_test.go`: include Gitea receive-pack input/output and push diagnostics in the existing credential-marker boundary.
- `integration/gitea_test.go`: extend the pinned clone/fetch demonstration with allowed, denied, and cancelled pushes plus audit/leak assertions.
- `internal/testutil/gitea.go`: expose narrow operator-side ref lookup for provider-state assertions.
- `internal/testutil/gitea_test.go`: test the ref lookup parser without requiring another container.
- `.github/workflows/ci.yml`: run the renamed, expanded pinned Gitea Git interoperability test.

### Unchanged production boundaries

- `internal/policy/push.go`: existing push-policy implementation is reused without a provider branch.
- `proto/repowolf/v1/*.proto` and `gen/repowolf/v1/*.pb.go`: no protocol changes.
- `internal/app` and provider runtime code: the existing token-free SSH environment is reused.

---

### Task 1: Admit Gitea to the shared receive-pack service

**Files:**
- Modify: `internal/gitservice/receive.go` (`receiveCommand`)
- Modify: `internal/gitservice/receive_test.go` (Gitea receive-pack fixtures and tests)

**Interfaces:**
- Consumes: `Service.command(ctx, open, capability, "git-receive-pack", allowedKinds...)`, `policy.ResolveGit`, `config.ValidRepositoryIdentity`, and the existing receive-pack parser/policy relay.
- Produces: `receiveCommand(context.Context, *repowolfv1.GitOpen) (policy.ResolvedRepository, runner.Command, error)` that accepts GitHub or Gitea only after matching read and write repository IDs.
- Produces: reusable service-level proof that Gitea receives the same validation, audit, limit, cancellation, and cleanup behavior as GitHub.

- [ ] **Step 1: Replace the predecessor's Gitea-denial test with failing authorization and trusted-command tests**

In `internal/gitservice/receive_test.go`, replace `TestReceivePackGiteaDenialBeforeInput` with table-driven cases built from `newGiteaTestService`. Cover both-capability success, missing `git:read`, missing `git:write`, wrong SSH user/host/port, an ungranted repository, and a read/write repository mismatch fixture. On success, call `receiveCommand` and require this exact trusted argv shape:

```go
want := []string{
    "-T", "-p", "2222", "--",
    "forge_user@gitea.example",
    "git-receive-pack 'team_name/repo.one.git'",
}
```

For every denial, use `countingProcessRunner`, invoke `receivePack` with a second frame containing `[]byte("must remain unread")`, and require `starts == 0`, `stream.recvAt == 1`, and `GIT_TERMINAL_CATEGORY_PERMISSION_DENIED`. Add one unsupported provider-kind case by constructing a policy snapshot directly and require the same pre-start denial.

- [ ] **Step 2: Run the focused authorization tests and verify the expected failure**

Run:

```bash
go test ./internal/gitservice -run 'TestReceive(Command|Pack).*Gitea' -count=1
```

Expected: FAIL because `receiveCommand` still passes only `config.ProviderGitHub` to both capability checks.

- [ ] **Step 3: Extend the closed receive-pack provider allowlist**

Change only the two `service.command` calls in `receiveCommand`:

```go
allowedKinds := []config.ProviderKind{config.ProviderGitHub, config.ProviderGitea}
_, readRepository, err := service.command(
    ctx, open, config.GitRead, "git-receive-pack", allowedKinds...,
)
// Preserve the existing early return.
command, writeRepository, err := service.command(
    ctx, open, config.GitWrite, "git-receive-pack", allowedKinds...,
)
```

Keep the existing `readRepository.ID != writeRepository.ID` denial and return the write-resolved trusted command. Do not duplicate the receive-pack relay or branch on provider kind after authorization.

- [ ] **Step 4: Add failing provider-parity policy and audit tests**

Add a `receiveExecutableGiteaService` helper that uses `newGiteaTestService(t, config.GitRead, config.GitWrite)`, emits the existing bounded advertisement, validates the canonical Gitea receive-pack argv, and writes provider stdin to a capture file. Add table rows for:

```go
[]struct {
    name       string
    policy     config.PushPolicy
    prefix     []byte
    want       repowolfv1.GitTerminalCategory
    wantRefs   []string
    wantCount  int
    wantInput  bool
}{
    {name: "allowed", policy: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, DenyDeletes: true, MaxRefUpdates: 4}, prefix: receivePrefix("refs/heads/feature"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, wantRefs: []string{"refs/heads/feature"}, wantCount: 1, wantInput: true},
    {name: "denied ref", policy: config.PushPolicy{DenyRefs: []string{"refs/heads/main"}, MaxRefUpdates: 4}, prefix: receivePrefix("refs/heads/main"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST, wantRefs: []string{"refs/heads/main"}, wantCount: 1},
    {name: "denied delete", policy: config.PushPolicy{DenyDeletes: true, MaxRefUpdates: 4}, prefix: receiveDeletePrefix("refs/heads/feature"), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST, wantRefs: []string{"refs/heads/feature"}, wantCount: 1},
    {name: "too many updates", policy: config.PushPolicy{MaxRefUpdates: 1}, prefix: receiveTwoUpdatePrefix(), want: repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_INVALID_REQUEST},
}
```

For accepted input, require byte-for-byte captured prefix/pack data. For every denied row, require an absent or empty capture. Decode audit JSONL and require `accepted` followed by exactly one `completed` or `denied` terminal event, provider `gitea`, the configured repository ID, safe refs/update count where parsing completed, and `InputBytes == 0` on denial. Reuse existing receive-pack prefix helpers and add concrete delete/two-update pkt-line helpers; do not test the internals of `policy.AuthorizePush` separately.

- [ ] **Step 5: Add a failing active-cancellation cleanup test**

Create `TestReceivePackGiteaCancellationReapsProviderAndWritesCancelledAudit`. Use a real temporary shell runner that writes its PID, emits a valid advertisement, then blocks while reading or before completing output. Start `receivePack` with a cancellable authenticated stream context, wait with a bounded channel for the PID file, cancel the context, and require:

```go
elapsed < 500*time.Millisecond
terminalAudit.Outcome == audit.OutcomeCancelled
terminalAudit.Provider == "gitea"
terminalAudit.Repository == "gitea-project"
terminalAudit.Operation == "git.receive-pack"
```

Use the existing `assertProcessCleanup` helper to require the process group is gone and its temporary working directory is removed. Scan returned errors and audit bytes for a distinct sensitive cancellation marker and require it is absent. This test exercises the existing runner contract rather than introducing Gitea-specific cleanup code.

- [ ] **Step 6: Run and format the complete Git service package**

Run:

```bash
gofmt -w internal/gitservice/receive.go internal/gitservice/receive_test.go
go test ./internal/gitservice -run 'TestReceive(Command|Pack).*Gitea' -count=1
go test -race ./internal/gitservice -count=1
```

Expected: PASS, including existing GitHub advertisement, prefix, policy, limit, audit, terminal-delivery, and lifecycle regression tests.

- [ ] **Step 7: Commit the shared service change**

```bash
git add internal/gitservice/receive.go internal/gitservice/receive_test.go
git commit -m "feat(git): permit controlled Gitea receive-pack"
```

---

### Task 2: Prove controlled Gitea pushes with real Git and fake SSH

**Files:**
- Modify: `integration/testdata/policy.yaml` (Gitea grant and push policy)
- Modify: `integration/testdata/fake-ssh.sh` (Gitea receive-pack route and capture)
- Modify: `integration/git_test.go` (`TestRealGitGiteaCloneFetchAndControlledPushes`, fixture fields/setup)
- Modify: `integration/audit_assertions_test.go` (Gitea Git audit expectations)
- Modify: `integration/leak_test.go` (Gitea push channels)

**Interfaces:**
- Consumes: real `git`, `repowolf-git-ssh`, TLS gRPC, the broker, the mixed-provider policy, fake SSH, and local bare Git repositories.
- Produces: an end-to-end allowed Gitea ref update and a same-runtime local-policy denial that leaves the provider ref unchanged and captures zero update bytes.
- Produces: exact safe Gitea receive-pack audit and credential-isolation assertions.

- [ ] **Step 1: Add Gitea write policy and a separate receive-input channel**

In `integration/testdata/policy.yaml`, add `refs/heads/denied` to `gitea-read.git.denyRefs` and add `git:write` beside its existing `git:read` grant. Keep `denyDeletes: true` and `maxRefUpdates: 16` unchanged.

In `integration/testdata/fake-ssh.sh`, add a `git-receive-pack 'Team_Name/Repo.One.git'` case parallel to the GitHub case:

```sh
: "${FAKE_GIT_RECEIVE_PACK:?}"
: "${FAKE_SSH_GITEA_RECEIVE_INPUT:?}"
: > "$FAKE_SSH_GITEA_RECEIVE_INPUT"
"$FAKE_TEE" "$FAKE_SSH_GITEA_RECEIVE_INPUT" |
  "$FAKE_GIT_RECEIVE_PACK" "$FAKE_SSH_GITEA_REPOSITORY"
```

Add `giteaReceiveInput` to `gitFixture`, initialize it under the fixture root, and pass `FAKE_SSH_GITEA_RECEIVE_INPUT` to the broker environment. Keep all child-environment logging unchanged.

- [ ] **Step 2: Replace the old fail-closed push assertion with failing allowed/denied demonstrations**

Rename the test to `TestRealGitGiteaCloneFetchAndControlledPushes`. After its clone/fetch checks, configure the checkout identity, create `refs/heads/feature/allowed`, commit `allowed-gitea-content-marker`, and run:

```go
allowed := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/feature/allowed")
```

Require success, require `rev-parse refs/heads/feature/allowed` in `fixture.giteaRemote` to equal the local commit, and require `giteaReceiveInput` to be non-empty.

Then create another commit, record the provider's missing-or-current `refs/heads/denied` state, and run:

```go
denied := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/denied")
```

Require a nonzero result with the fixed `repowolf git transport failed` diagnostic, require the provider ref state to be unchanged, and require `giteaReceiveInput` to be empty because the fake SSH invocation truncates the capture before each receive-pack attempt. Do not restart the broker or rewrite policy between pushes.

- [ ] **Step 3: Run the real-Git test and verify the expected failure**

Run:

```bash
sh -n integration/testdata/fake-ssh.sh
go test ./integration -run '^TestRealGitGiteaCloneFetchAndControlledPushes$' -count=1 -v
```

Expected before Task 1 is present: FAIL at the allowed push with a permission denial. Expected after Task 1 but before fixture routing is complete: FAIL because fake SSH has no Gitea receive-pack route.

- [ ] **Step 4: Assert exact Gitea lifecycle audit records**

Replace `giteaUploadAuditExpectations` with `giteaGitAuditExpectations(allowedRef, deniedRef string)`. Return the two existing upload-pack invocation triplets followed by:

```go
// Allowed receive-pack.
[]auditExpectation{
    giteaAccepted("git.receive-pack"),
    giteaReceiveTerminal("completed", "GIT_TERMINAL_CATEGORY_COMPLETED", allowedRef, true),
    stream("/repowolf.v1.GitService/ReceivePack"),
},
// Locally denied receive-pack.
[]auditExpectation{
    giteaAccepted("git.receive-pack"),
    giteaReceiveTerminal("denied", "GIT_TERMINAL_CATEGORY_INVALID_REQUEST", deniedRef, false),
    stream("/repowolf.v1.GitService/ReceivePack"),
},
```

Define the helpers in the same file with concrete fields: principal `agent`, provider `gitea`, repository `gitea-read`, update count `1`, positive output bytes for both provider advertisements, positive input bytes only for the allowed push, and no `input_bytes` field for the denied terminal event. Require each accepted event to precede its one terminal Git event and corresponding gRPC stream completion.

- [ ] **Step 5: Extend leak and cleanup assertions**

Include allowed/denied push stdout and stderr, `giteaReceiveInput`, both new commit contents, SSH argv/environment, broker stderr, audit JSONL, and checkout-visible files in the existing marker scan. Forbid the principal token, Gitea provider token, GitHub provider token, ambient `GH_*` markers, fake SSH stderr, pack markers, and both new content markers from unsafe channels according to the existing per-channel rules. Require the SSH environment to continue reporting every principal/provider credential as `unset` and require `assertNoFixtureProcess` after stopping the broker.

- [ ] **Step 6: Run the focused and full integration tests**

Run:

```bash
gofmt -w integration/git_test.go integration/audit_assertions_test.go integration/leak_test.go
sh -n integration/testdata/fake-ssh.sh
go test ./integration -run 'TestRealGitGiteaCloneFetchAndControlledPushes|TestRealGitDefaultPortStreamsOfflineAndDeniesDefaultMainBeforeProviderInput|TestMarkersRemainInTheirIntendedChannels|TestGitFixtureIgnoresHostConfigurationInjection' -count=1 -v
go test -race ./integration -count=1
```

Expected: PASS. The GitHub push regression retains its argv, exact-byte forwarding, local denial, audit, and leak behavior.

- [ ] **Step 7: Commit the fake-provider end-to-end proof**

```bash
git add integration/testdata/policy.yaml integration/testdata/fake-ssh.sh integration/git_test.go integration/audit_assertions_test.go integration/leak_test.go
git commit -m "test(git): prove controlled Gitea pushes"
```

---

### Task 3: Extend the pinned Gitea interoperability gate

**Files:**
- Modify: `internal/testutil/gitea.go` (`Gitea.Ref` and ref-output parser)
- Modify: `internal/testutil/gitea_test.go` (ref-output parser tests)
- Modify: `integration/gitea_test.go` (`TestPinnedGiteaGitInteroperability`)
- Modify: `.github/workflows/ci.yml` (renamed pinned test selector)

**Interfaces:**
- Consumes: the Issue #6 digest-pinned Gitea fixture, operator SSH setup, real Git, one running RepoWolf broker, and unchanged policy.
- Produces: `(*testutil.Gitea).Ref(testing.TB, string) (string, bool)` for narrow operator-side provider-state verification.
- Produces: one current-image allowed-push, denied-push, cancellation/cleanup, audit-order, and credential-leak completion gate.

- [ ] **Step 1: Write failing tests for narrow provider ref lookup**

Add a private parser in `internal/testutil/gitea.go`:

```go
func parseLSRemoteRef(output, ref string) (string, bool, error)
```

In `internal/testutil/gitea_test.go`, add table rows for an exact single match, empty output, a different ref, malformed field count, invalid 40-hex object ID, and duplicate exact matches. Require malformed and duplicate output to return an error rather than selecting one value.

Add the public fixture method:

```go
func (fixture *Gitea) Ref(t testing.TB, ref string) (string, bool)
```

It must reject refs that do not begin with `refs/heads/`, run operator-side `git ls-remote --refs fixture.remoteURL() ref` with `fixture.gitEnvironment()`, call `parseLSRemoteRef`, and fail the test on command or parse errors. It returns no token, URL, environment, or command detail.

- [ ] **Step 2: Run the parser tests and verify the expected failure**

Run:

```bash
go test ./internal/testutil -run 'TestParseLSRemoteRef' -count=1
```

Expected: FAIL because the parser and `Gitea.Ref` do not exist.

- [ ] **Step 3: Implement and verify ref lookup**

Implement strict tab-delimited parsing, `^[0-9a-f]{40}$` object-ID validation, and exact ref equality. Format and run:

```bash
gofmt -w internal/testutil/gitea.go internal/testutil/gitea_test.go
go test ./internal/testutil -count=1
```

Expected: PASS without starting Docker for the parser test.

- [ ] **Step 4: Extend the pinned policy and SSH wrapper for receive-pack observation**

Rename `TestPinnedGiteaCloneFetch` to `TestPinnedGiteaGitInteroperability`. In its generated policy, grant `git:write`, set:

```yaml
git:
  denyRefs:
    - refs/heads/denied
  denyDeletes: true
  maxRefUpdates: 16
```

Extend the wrapper with `SSH_RECEIVE_CAPTURE`, `SSH_BLOCK_RECEIVE`, and `SSH_RECEIVE_PID` paths supplied through the already token-free broker environment. Inspect only the final remote-command argument. For normal receive-pack, truncate the capture and execute:

```sh
"$TEE_PATH" "$SSH_RECEIVE_CAPTURE" |
  "$SSH_PATH" -i "$IDENTITY_PATH" -o IdentitiesOnly=yes \
  -o UserKnownHostsFile="$KNOWN_HOSTS_PATH" -o StrictHostKeyChecking=yes "$@"
```

For receive-pack while the block marker exists, write `$$` to `SSH_RECEIVE_PID` and `exec "$SLEEP_PATH" 30`; all other invocations exec the existing verified SSH command. Resolve and inject canonical `tee` and `sleep` paths. Continue recording argv and the credential-free environment for every invocation.

- [ ] **Step 5: Add failing allowed and denied real-Gitea pushes**

After clone/fetch, configure the checkout identity and create one commit. Push it to `refs/heads/feature/allowed` through the same RepoWolf remote. Require success, require `gitea.Ref(t, "refs/heads/feature/allowed")` to return the local commit, and require the receive capture to be non-empty.

Create another commit and push it to `refs/heads/denied` without changing policy or restarting the broker. Require the fixed sanitized client diagnostic, require `gitea.Ref(t, "refs/heads/denied")` to report absent before and after, and require the receive capture to be empty after the denied attempt.

- [ ] **Step 6: Add a cancellable active-push demonstration**

Create the block marker, start a new real `git push origin HEAD:refs/heads/cancelled` with `exec.CommandContext`, and wait up to five seconds for `SSH_RECEIVE_PID`. Cancel the command context, require the Git command to return within 500 milliseconds, remove the block marker, and poll the recorded PID with signal `0` until it is absent or a one-second deadline expires. Require `gitea.Ref(t, "refs/heads/cancelled")` to remain absent.

After stopping the broker, require the audit stream to contain these ordered Git event pairs for provider `gitea` and repository `pinned-gitea`: upload accepted/completed, allowed receive accepted/completed, denied receive accepted/denied, and cancelled receive accepted/cancelled. Require safe allowed/denied ref metadata and zero provider input bytes on the denied and cancelled terminal events according to omission rules.

- [ ] **Step 7: Complete pinned leak and cleanup assertions**

Scan clone, fetch, all three push diagnostics, broker stderr, audit, checkout content, receive capture, SSH argv/environment capture, and temporary artifacts for both `pinnedGiteaProviderMarker` and `pinnedGiteaPrincipalMarker`. Require neither marker in any untrusted or child-process channel. Require each SSH invocation to report `REPOWOLF_TOKEN_GITEA`, `REPOWOLF_TOKEN_AGENT`, `GH_TOKEN`, and `GITHUB_TOKEN` as `unset`; require trusted configured casing in upload-pack and receive-pack argv; and require no recorded wrapper or SSH PID to survive cleanup.

- [ ] **Step 8: Update and run the pinned CI gate**

Change the workflow test selector, without adding a static workflow-content test:

```yaml
run: go test ./integration -run '^TestPinnedGiteaGitInteroperability$' -count=1 -v
```

Run the behavioral gate directly:

```bash
REPOWOLF_GITEA_INTEGRATION=1 go test ./integration -run '^TestPinnedGiteaGitInteroperability$' -count=1 -v
```

Expected: PASS against the existing `testutil.PinnedGiteaImage` digest with allowed, denied, cancelled, audit, cleanup, and leak assertions.

- [ ] **Step 9: Run the complete repository verification gate**

Run exactly:

```bash
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
git diff --check
```

Expected: every command exits zero. `scripts/check-generated.sh` confirms no Protobuf change; the race suite covers GitHub push and Gitea clone/fetch regressions; the opt-in pinned test from Step 8 supplies the real-provider completion evidence.

- [ ] **Step 10: Confirm issue scope and commit the pinned gate**

Run:

```bash
git status --short
git diff --name-only HEAD~2..HEAD
git diff --check
```

Expected: only the files named by Tasks 1-3 are changed or committed, with no `.pi/todos` files, generated protocol changes, push-policy model changes, SDK dependency, or credential value.

Commit:

```bash
git add internal/testutil/gitea.go internal/testutil/gitea_test.go integration/gitea_test.go .github/workflows/ci.yml
git commit -m "test(gitea): gate controlled Git pushes"
```

Then run:

```bash
git status --short
git log --oneline -4
```

Expected: a clean worktree and the three task commits following this plan commit.
