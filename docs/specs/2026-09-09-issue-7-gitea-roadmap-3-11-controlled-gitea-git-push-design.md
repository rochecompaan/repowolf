# Controlled Gitea Git push design

**Date:** 2026-09-09

**Status:** Ready for implementation planning

## Context

Issue 2 adds Gitea clone and fetch to RepoWolf's shared Git SSH path. It generalizes the client selector, resolves the provider and canonical SSH coordinates from trusted policy, and permits Gitea `git-upload-pack`. It deliberately leaves `git-receive-pack` restricted to GitHub.

RepoWolf already has a provider-neutral receive-pack implementation. It requires both `git:read` and `git:write`, starts a pinned broker-side SSH process, parses the provider advertisement, buffers and validates the policy-relevant request prefix, and forwards no client update bytes until every requested ref update passes the repository's push policy. It then relays the remaining bounded stream and records safe lifecycle audit events.

This design is the third issue in the approved decomposition. Its authoritative cross-cutting contracts remain:

- `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`
- `docs/specs/2026-09-02-gitea-roadmap-issue-decomposition-design.md`
- `docs/specs/2026-09-02-gitea-provider-runtime-credential-isolation-design.md`
- `docs/specs/2026-09-08-issue-6-gitea-roadmap-2-11-gitea-git-clone-and-fetch-design.md`

## Outcome

An agent can push an allowed commit to a permitted Gitea repository through RepoWolf. A push denied by the existing repository policy remains denied without changing policy, mutating the denied ref, forwarding denied update bytes, or exposing credentials.

GitHub push behavior remains unchanged.

## Applicable roadmap decisions

This specification applies the issue-decomposition decisions:

- **ID-01:** Use one serial implementation stream.
- **ID-02:** Use thin foundations followed by vertical slices.
- **ID-03:** Complete the Git path before the Gitea API feature path.
- **ID-06:** Write specifications and plans just before implementation.
- **ID-07:** Use one current Gitea image for issue completion.
- **ID-08:** Defer broad operations and certification work until actual use creates a need.

It also preserves the roadmap's exact-repository authorization, provider-independent capabilities and push policy, credential isolation, bounded streams, cancellation and cleanup, sanitized errors, safe audit order, and GitHub compatibility rules.

## Goals

- Permit an authorized Gitea repository to use the existing `GitService.ReceivePack` path.
- Require the existing `git:read` and `git:write` capabilities for Gitea pushes.
- Apply the repository's existing `denyRefs`, `denyDeletes`, and `maxRefUpdates` policy without a Gitea exception.
- Continue deriving upstream SSH user, host, port, owner, and repository name only from trusted configuration.
- Preserve receive-pack byte limits, prefix validation, idle and operation timeouts, cancellation, process cleanup, terminal categories, and audit ordering.
- Prove allowed push, denied push, cancellation, and credential non-disclosure through reusable tests and one pinned Gitea interoperability demonstration.

## Non-goals

This issue does not add or change:

- the push-policy model, configuration fields, defaults, or capability names;
- force-push, branch-protection, ancestry, commit-content, signature, or actor-specific policy;
- Gitea API access, an SDK dependency, a `tea` client, or token-scope discovery;
- HTTPS Git transport, SSH key management, agent forwarding, or known-host provisioning;
- a provider-specific Git service, protocol message, terminal category, limit, or audit schema;
- repository wildcards, provider selection from client input, or a provider registry;
- broad Gitea version certification or deployment documentation.

Gitea remains authoritative for server-side repository rules such as protected branches. RepoWolf does not reinterpret those failures as local push-policy decisions.

## Approaches considered

### Extend the shared receive-pack provider allowlist

This is the selected approach. The receive-pack authorization step permits both trusted GitHub and Gitea resolutions, then runs the unchanged capability, push-policy, stream, runner, and audit path.

This is the smallest change and directly enforces the roadmap requirement that both providers use the same deep Git module and policy model. It also keeps later fixes to protocol validation and process lifecycle shared.

### Add a separate Gitea receive-pack implementation

A separate handler could isolate provider rollout, but it would duplicate security-sensitive framing, prefix validation, policy, limits, cleanup, and audit behavior. The duplicate paths could drift and violate the no-provider-exceptions requirement. This approach is rejected.

### Forward receive-pack and rely on Gitea branch protection

This would reduce broker-side parsing but bypass RepoWolf's configured push policy and forward denied data to the provider. Gitea branch protection is complementary provider behavior, not a replacement for broker policy. This approach is rejected.

## Proposed behavior

### Authorization and command construction

The client interface and Protobuf protocol do not change. The Git SSH shim continues to parse `git-receive-pack` into the repository selector introduced by Issue 2.

On the broker, receive-pack performs these checks before starting SSH:

1. Authenticate the principal.
2. Resolve the full SSH selector through the Git-specific policy path with `git:read`.
3. Require the resolved provider kind to be `github` or `gitea`.
4. Resolve the same selector with `git:write` and require the same trusted repository ID.
5. Revalidate the trusted owner and repository using the resolved provider's configured grammar.
6. Construct `git-receive-pack '<owner>/<repository>.git'` and the complete SSH argv only from the trusted provider and repository records.

A missing capability, unknown repository, unauthorized repository, wrong SSH authority, ambiguous selector, unsupported provider, or mismatch between read and write resolution returns the existing indistinguishable denial. No SSH process starts and no Git update data reaches a provider.

No provider ID, kind, host, executable, command, credential, or policy value is accepted from a new client field.

### Receive-pack and policy flow

After authorization, the existing receive-pack flow remains unchanged:

1. Write the accepted `git.receive-pack` audit event before provider process start.
2. Start the pinned SSH executable with the token-free environment and existing operation limits.
3. Read and validate the bounded provider advertisement.
4. Relay that advertisement to the client.
5. Buffer the bounded receive-pack request prefix and parse its ref updates, negotiated capabilities, shallow declarations, push options, and push certificate forms.
6. Apply the repository's existing push policy to the complete requested update set.
7. Only after validation succeeds, write the validated prefix and remaining pack stream to Gitea.
8. Relay bounded provider output, wait for process cleanup, send the existing terminal frame when transport permits, and write the terminal audit event.

The policy remains provider-independent:

- a ref in `denyRefs` is denied;
- a deletion is denied when `denyDeletes` is true;
- an update set larger than `maxRefUpdates` is denied;
- malformed refs, object IDs, capabilities, certificates, or request framing are rejected by the existing protocol validation.

A locally denied or malformed request forwards zero client request bytes to Gitea. Starting SSH to obtain the advertisement is expected; the provider receives no update prefix or pack data.

Once validation succeeds, RepoWolf does not inspect commit ancestry or pack contents. A later Gitea rejection is reported as the existing sanitized provider failure and cannot retroactively be classified as a RepoWolf policy denial.

## Cancellation, limits, and cleanup

Gitea receive-pack uses the same bounds as GitHub receive-pack:

- initial-frame, idle-stream, and operation timeouts;
- maximum stream chunk size;
- maximum receive-pack prefix bytes;
- maximum Git bytes in each direction;
- maximum provider stderr capture;
- repository `maxRefUpdates`;
- global and per-principal concurrency limits.

Cancellation before process start launches nothing. Cancellation while reading the advertisement, validating the prefix, writing provider input, relaying provider output, or waiting for completion cancels the process context, closes stdin, drains bounded output as required by the runner contract, waits for process-group cleanup, and records the existing cancelled outcome. A blocked client or provider must not leave an SSH, Git, or helper process alive after the bounded cleanup path.

Terminal delivery can be impossible after client cancellation. Audit completion remains required and follows the existing delivery-versus-audit error contract; this issue adds no special Gitea behavior.

## Security and credential isolation

- The agent receives no Gitea provider token or broker SSH identity.
- The startup-loaded Gitea token is not used for Git SSH and never enters a Git request, response, argv, audit record, checkout, or child environment.
- The broker-side SSH process receives only the token-free environment from Issue 1. Principal tokens, provider tokens, `REPOWOLF_*`, `GH_*`, and `GITHUB_*` variables remain absent.
- Broker-side SSH authentication continues to use operator-controlled state outside the agent sandbox.
- Trusted policy supplies canonical upstream coordinates. Untrusted casing or aliases can select a configured Gitea repository only under Issue 2's matching rules; trusted configured casing is emitted upstream.
- Local policy denial occurs before any client-controlled receive-pack bytes are forwarded.
- Provider stderr, pack contents, ref object IDs, commands, and credentials are not copied into client errors or audit records.

## Error and audit behavior

The existing client diagnostic and Git terminal categories remain unchanged:

- authorization failures: permission denied;
- malformed receive-pack input and local push-policy denial: invalid request;
- direction or prefix bounds: limit exceeded;
- timeout: deadline exceeded;
- provider process rejection or failure: provider failure;
- other unavailable lifecycle failures: unavailable.

Audit uses the existing schema and `git.receive-pack` operation name. After repository resolution, accepted and terminal events identify provider `gitea` and the configured repository ID. Safe terminal metadata includes the final outcome, terminal category, byte counts, requested refs, and update count according to the existing omission rules.

The lifecycle order does not change:

- an authorized attempt writes `accepted` before starting SSH, followed by exactly one Git terminal event;
- a local push-policy denial produces an accepted event followed by a denied terminal event, with zero provider input bytes and the bounded requested ref metadata;
- cancellation produces an accepted event when authorization completed, followed by a cancelled terminal event;
- authorization denial writes no accepted Git event and starts no process;
- audit failure behavior remains fail-closed under the existing Git service contract.

No audit field contains credentials, pack data, object IDs, provider stderr, or generated argv.

## Compatibility constraints

- Existing GitHub clone, fetch, and push behavior remains a regression gate.
- Existing valid GitHub receive-pack requests produce the same SSH argv, forwarded bytes, terminal categories, and audit sequence.
- Gitea clone and fetch behavior from Issue 2 remains unchanged.
- The Git SSH grammar and wire protocol gain no new forms or fields in this issue.
- Gitea push uses the same `git:read` plus `git:write` capability combination as GitHub.
- Existing repository push-policy configuration has identical meaning for GitHub and Gitea.
- The current digest-pinned Gitea image and fixture from Issue 2 are reused rather than introducing another image or matrix.

## Affected components

- `internal/gitservice/receive.go`: allow trusted Gitea repositories through both receive-pack capability checks; retain the same-repository check and shared relay path.
- `internal/gitservice/receive_test.go` and lifecycle tests: add Gitea authorization, trusted argv, policy denial, cancellation, limit, audit, and cleanup coverage where existing provider-neutral tests do not already prove the behavior.
- `internal/policy` tests: confirm the existing Git resolver and capability checks produce the same Gitea repository for read and write; no production policy-model change is expected.
- `integration/git_test.go` and fixtures: add real Git allowed/denied Gitea push coverage and exact fake-SSH/audit/leak assertions while preserving GitHub regression coverage.
- `integration/gitea_test.go` and `internal/testutil/gitea.go`: extend the pinned Gitea fixture with a broker-mediated push and provider-side ref verification.
- CI wiring: run the extended pinned-image interoperability gate through the existing opt-in Gitea integration path. No production dependency is added.

Implementation should keep production changes localized. If enabling Gitea requires duplicating receive-pack flow or changing `internal/policy/push.go`, stop and revisit the design rather than introducing a provider exception.

## Verification strategy

All proposed automated tests pass the Patchmill Testing Value Gate: they verify security-sensitive behavior, cancellation, policy enforcement, and real provider interoperability and can fail on meaningful regressions.

### Focused service and policy tests

- Permit Gitea receive-pack only when one trusted repository resolves for both `git:read` and `git:write`.
- Deny missing read or write capability, wrong authority, ambiguity, and repository mismatch before process start.
- Generate SSH argv from canonical Gitea configuration, including configured user, host, port, owner, and repository casing.
- Apply `denyRefs`, `denyDeletes`, and `maxRefUpdates` identically to Gitea and GitHub.
- Prove a denied or malformed prefix forwards zero client bytes while retaining safe requested-ref audit metadata where parsing succeeds.
- Prove allowed input is byte-preserved after validation.
- Cancel a Gitea receive-pack while work is active; prove bounded return, process reaping, and cancelled audit outcome.
- Retain existing advertisement, capability, certificate, push-option, stream-limit, terminal-delivery, audit-failure, and race tests.

### Fake-provider integration

Using real Git, the RepoWolf SSH shim, TLS gRPC, the broker, and fake SSH:

1. clone a configured Gitea repository;
2. create and push one allowed feature ref;
3. verify the upstream ref reaches the expected commit;
4. attempt a denied ref update without changing policy;
5. verify the denied upstream ref is unchanged and fake SSH receives zero client update bytes for that attempt;
6. cancel or terminate an in-flight push and verify no fixture process survives;
7. assert provider `gitea`, repository ID, operation, outcome, refs, update count, byte accounting, and lifecycle order in audit records;
8. scan client output, broker output, audit, SSH argv/environment/input/output, repository content, and sandbox-visible artifacts for principal and provider credential markers.

### Pinned Gitea demonstration

Extend Issue 2's digest-pinned current Gitea test. Provisioning remains operator-side test setup; RepoWolf production code does not call the Gitea API.

The demonstration:

1. starts the pinned Gitea fixture and broker-side verified SSH access;
2. grants the test principal `git:read` and `git:write` for one repository with a policy that allows a feature ref and denies a separate ref;
3. clones through RepoWolf, commits locally, and pushes the allowed ref through RepoWolf;
4. verifies provider state through operator-side fixture access;
5. attempts the denied push with the same running broker and unchanged policy;
6. verifies the denied ref did not change;
7. cancels an active push path and verifies process cleanup;
8. proves the Gitea provider token and principal token are absent from all untrusted and child-process channels;
9. verifies accepted/completed, accepted/denied, and accepted/cancelled audit sequences as applicable.

One current pinned image is the issue-completion gate under ID-07. A broader version matrix remains deferred under ID-08.

### Regression commands

The implementation plan must include focused package and integration tests plus the repository gates:

```text
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
git diff --check
```

Run the existing Nix/CI checks relevant to the Gitea integration wiring. Static workflow or fixture text does not need tests that merely restate configuration; validate it through syntax checks, container startup, and the behavioral demonstration.

## Acceptance criteria

1. A principal with `git:read` and `git:write` can push an allowed commit to one permitted Gitea repository through real Git and the existing RepoWolf Git service.
2. Receive-pack resolves the provider and every upstream SSH coordinate only from trusted policy and permits only GitHub or Gitea.
3. Missing capability, unknown or unauthorized repository, wrong authority, ambiguity, or read/write repository mismatch denies the request before process start.
4. Gitea uses the existing `denyRefs`, `denyDeletes`, and `maxRefUpdates` semantics without provider-specific branches or configuration changes.
5. A locally denied push forwards zero client update bytes and leaves the denied provider ref unchanged without a policy restart or change.
6. Allowed pushes retain the existing receive-pack prefix validation, byte limits, timeouts, cancellation, process cleanup, terminal categories, and provider-failure handling.
7. Cancellation returns within configured bounds, reaps provider processes, and records the existing cancelled lifecycle outcome.
8. The agent and broker-side SSH process receive no Gitea provider token, principal token, or unrelated provider credential, and no tested output or audit channel leaks them.
9. Gitea receive-pack audit events preserve the existing accepted-before-execution and terminal-event order with safe provider, repository, ref, update-count, and byte metadata.
10. Existing GitHub Git behavior and Issue 2's Gitea clone/fetch behavior continue to pass.
11. One current digest-pinned Gitea image passes allowed-push, denied-push, cancellation/cleanup, audit, and leak demonstrations.
12. No push-policy model, Gitea API, SDK, `tea`, Git wire protocol, audit schema, or provider framework is added.

## Open questions

None.
