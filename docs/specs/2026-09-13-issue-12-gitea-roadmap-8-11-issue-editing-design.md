# Gitea issue editing design

**Date:** 2026-09-13

**Status:** Approved for implementation planning

## Context

Issue #11 added typed, authorized Gitea issue creation, comments, close, and reopen operations. It established strict `tea` parsing, canonical issue records, bounded label-name resolution, one-attempt writes, explicit unknown outcomes, and terminal mutation audits. This issue adds the remaining issue-edit surface from the approved Gitea roadmap.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-04, ID-06, ID-07, and ID-08. The merged issue-#11 implementation at commit `89d007a` is the baseline. No `AGENTS.md` exists in this worktree or its repository parents.

## Outcome

An agent with `issues:write` can edit one issue's title, body, assignees, and labels through the restricted `tea` client. RepoWolf validates all requested names before writing, derives a deterministic mutation plan from a bounded snapshot, checks family-specific postconditions with a final read, and performs at most one reconciliation pass when a concurrent change prevents the initial plan from satisfying the request.

The operation reports success only for a final issue snapshot that satisfies every effective requested postcondition. It distinguishes an unprovable write outcome from a proved but incomplete partial mutation. It does not claim transactional or exactly-once behavior.

## Approaches considered

### Deterministic family planner with one bounded reconciliation pass

This is the selected approach. Parse precedence into one effective action per field family, collect the issue and only the required assignee/label catalogs, build stable deltas, execute narrow provider operations in a fixed order, and evaluate a fresh issue snapshot. One newly computed corrective plan is allowed when all provider calls returned successfully but concurrent state made a postcondition false.

This preserves unrelated members for add/remove operations, gives exact replacement its documented meaning, bounds provider work, and creates the reusable planner seam needed by the later pull-request-edit slice without introducing a provider-neutral forge model.

### One broad `EditIssue` request

A single patch is smaller, but Gitea's issue patch cannot express label changes and does not safely preserve unrelated concurrent collection changes. It also obscures partial effects and cannot implement family-specific reconciliation. It is rejected.

### Unbounded compare-and-retry loop

Repeated reads and writes could eventually converge under light contention, but it has no deterministic call bound and can fight another actor indefinitely. Gitea does not expose a suitable issue edit version precondition through the pinned SDK. It is rejected.

## Existing behavior and affected components

The issue-#11 baseline provides these seams:

- `proto/repowolf/v1/gitea.proto` has additive issue mutation branches and canonical `GiteaIssueRecord` results.
- `internal/client/gitea` implements the closed issue command grammar, mutation output, local bounds, and conservative unknown diagnostics.
- `internal/provider/gitea` provides issue-kind preflight, bounded repository-label lookup, normalized issue snapshots, narrow SDK interfaces, and trusted write-attempt classification.
- `internal/server/gitea.go` performs request validation, `issues:write` authorization, provider-instance dispatch, response bounds, and mutation audit metadata transfer.
- `internal/audit` has closed terminal outcomes including `unknown`.
- `integration/gitea_issue_writes_test.go` proves the packaged client-to-Gitea path against the pinned current image.

Affected areas are the additive Gitea protocol/generated code, restricted parser and renderer dispatch, provider operation validation, a focused issue-edit planner/executor, SDK assignee and label methods, RPC status classification, terminal audit outcomes, server tests, and the pinned Gitea integration fixture. Existing transport, policy, provider-instance composition, issue normalization, and output shapes remain the shared foundations.

## Restricted command contract

Support:

```text
tea issues edit INDEX --repo OWNER/REPO MUTATION...
tea issues e INDEX --repo OWNER/REPO MUTATION...
```

`edit` and `e` are aliases. Exactly one positive index is accepted. `--repo`/`-r` remains required. `--output`/`-o simple|table|json` remains optional and defaults to `simple`.

The accepted mutation flags are:

- `--title TITLE`;
- `--description BODY`;
- `--set-assignees NAME[,NAME...]`;
- `--add-assignees NAME[,NAME...]` and `-a NAME[,NAME...]`;
- `--remove-assignees NAME[,NAME...]`;
- `--add-labels NAME[,NAME...]` and `-L NAME[,NAME...]`; and
- `--remove-labels NAME[,NAME...]`.

At least one mutation flag is required. Title is non-empty and at most 255 Unicode characters. Description has presence: omitted means unchanged and an explicit empty value clears the body; it is at most 64 KiB. A list contains at most 25 unique, case-sensitive names, each non-empty and at most 255 Unicode characters. `--set-assignees ""` is the sole empty-list form and means exact replacement with no assignees; empty add/remove lists are invalid. Invalid UTF-8 and NUL are forbidden in every value.

Precedence follows roadmap rule E1. After validating every supplied flag, the client retains only the highest-precedence assignee action (`set`, then `add`, then `remove`) and only the highest-precedence label action (`add`, then `remove`). Lower-precedence values have no provider effect. Repeated aliases of the same flag are still duplicates.

`--flag=value`, extra indices or positionals, stdin, prompts, editor fallback, milestone/deadline/referenced-version flags, and all other flags are rejected before RPC. The existing limits of 64 arguments and 64 KiB argv remain. Usage failures exit 2 without dialing; runtime failures exit 1 with bounded sanitized diagnostics.

Successful edits reuse the canonical issue mutation output: `index`, `title`, `state`, `url`, in that order for simple, table, and JSON. There is no changed-fields output.

## Protocol contract

Add one fresh `issue_edit` branch to `GiteaRequest.operation` and `GiteaResponse.result` without changing existing tags.

The typed request contains:

- a positive issue index;
- optional title and optional description fields;
- at most one assignee action, represented as a `oneof` with typed set/add/remove name lists; and
- at most one label action, represented as a `oneof` with typed add/remove name lists.

List wrapper messages preserve the distinction between an absent action and an intentionally empty assignee replacement. The server independently validates all presence, text, count, uniqueness, and oneof rules. Generic maps, raw JSON, SDK types, endpoint selectors, provider IDs, and concurrency knobs do not enter the protocol.

The response contains exactly one canonical `GiteaIssueRecord`. Existing response metadata, 8 MiB Protobuf/provider limits, rendered-output limits, semantic validation, and atomic output behavior remain unchanged. Generated code changes only through `scripts/generate.sh`.

## Effective postconditions

The planner evaluates each effective request family independently:

- title: final title equals the requested title;
- body: final body equals the requested body, including empty;
- set assignees: the final case-sensitive name set equals the requested set;
- add assignees: every requested name is present;
- remove assignees: every requested name is absent;
- add labels: every requested name is present; and
- remove labels: every requested name is absent.

Assignee and label order is not a postcondition because Gitea owns response ordering. Duplicate provider names, invalid nested records, or conflicting IDs make the snapshot or catalog invalid rather than being silently coalesced. Add/remove predicates intentionally preserve unrelated concurrent members. Exact assignee replacement intentionally removes unrelated members, including members concurrently introduced before a reconciliation snapshot.

## Preflight and deterministic planning

After authorization, RepoWolf performs these sequential preflight reads before any write:

1. Read and normalize the issue once, proving repository, index, and issue kind.
2. If effective set/add assignees contains names, read the repository's assignable-user collection once. The response remains subject to the 8 MiB provider budget. Require positive unique user IDs and non-empty unique case-sensitive usernames, and require every requested target to match exactly once. Removal of an already-absent name needs no eligibility lookup.
3. If an effective label action may write, list repository labels in pages of 50 with the existing maximum of 20 full pages plus the page-21 completion probe. Require positive unique IDs and non-empty unique case-sensitive names. Added labels must resolve exactly once. A removed label only needs an ID when it is present on the issue.

Any preflight validation, bound, lookup, cancellation, or name-resolution failure makes zero writes.

A pure planner consumes the normalized request, issue snapshot, and validated catalogs. It emits only currently necessary steps. Deltas are de-duplicated and sorted by case-sensitive name (label deletions then use the corresponding stable IDs). Families and steps execute in this fixed order:

1. one title/body patch when either text predicate is unmet;
2. assignee removal, then assignee addition; and
3. label addition, then individual label removals.

Text patching carries current values for SDK fields whose wire representation lacks presence so an edit cannot clear an unrequested field. Assignee set computes removals as current minus target and additions as target minus current. Assignee add/remove uses one batch call for the missing/present subset. Label add uses one batch call. The pinned Gitea API removes labels by ID, so each necessary label removal is one sequential call, at most 25 per pass.

A request whose preflight snapshot already satisfies every predicate is a successful no-op with no provider write; that validated snapshot is its final record.

## Execution, final reads, and reconciliation

Every planned SDK write is context-aware, invoked once, and has its returned issue or label collection fully validated before the next step. A successful intermediate provider response is evidence for that step but never replaces final-state observation.

After the initial plan completes, RepoWolf reads and normalizes the issue again. If all predicates hold, that snapshot is returned. If any predicate is false, RepoWolf treats the difference as concurrent change, builds one new plan from that fresh snapshot, executes only unmet predicates, and performs one second final read. There is no third plan.

The same family predicates govern planning, reconciliation, and completion, preventing a response from being accepted under weaker criteria than those used to mutate. A reconciliation step is a new ensure-postcondition operation based on observed state, not a blind retry of an earlier HTTP call.

The maximum normal work is therefore three issue reads (preflight, initial final read, reconciliation final read), one assignable-user collection read, 21 repository-label page reads, and two bounded mutation passes. Each pass has at most one text patch, two assignee batch writes, one label-add batch write, and 25 label-delete writes. Calls are sequential; existing global/per-principal concurrency and operation deadlines still apply.

This contract guarantees only that all predicates held at the successful final read. Without provider compare-and-swap support, another actor can change the issue immediately afterward; RepoWolf makes no locking, linearizability, or lasting-state claim.

## Errors, unknown outcomes, and partial mutations

Before the first write, failures retain existing known failure, denial, and cancellation semantics.

Once a provider write is invoked, an error or invalid response remains conservatively `GITEA_WRITE_OUTCOME_UNKNOWN`. RepoWolf stops issuing writes and makes one bounded issue read when the context permits. If that read proves every requested predicate, the edit succeeds with that record. Otherwise it returns the existing unknown-outcome diagnostic and does not reconcile or repeat the uncertain call.

Add a trusted `GITEA_EDIT_PARTIAL` status for cases where RepoWolf proves that at least one mutation took effect but the complete requested postcondition is not achieved—for example, a later step fails before invocation, cancellation occurs between confirmed steps, or the sole reconciliation pass ends with unmet predicates. The client prints only:

```text
tea: issue edit partially applied; inspect issue state before retrying
```

No response record is printed on unknown or partial failure. A pre-write failure is never classified partial. An uncertain call is never classified partial merely because an earlier call succeeded; uncertainty takes precedence unless a final read proves complete success.

The terminal audit outcome set gains `partial`. `gitea.issue_edit` records `completed` only after a satisfying final snapshot (including a no-op), `partial` for a proved incomplete mutation, `unknown` for unresolved write uncertainty, `cancelled` only before any mutation takes effect, and existing `failed`/`denied` outcomes for their established cases. Audit records do not identify changed families, names, issue indices, text, or provider errors.

## Request and provider flow

```text
restricted `tea issues edit`
  -> strict local parsing, validation, and precedence normalization
  -> typed issue_edit request over authenticated TLS gRPC
  -> server validation and exact issues:write authorization
  -> accepted audit event
  -> trusted provider-instance dispatch
  -> issue-kind snapshot plus required bounded catalogs
  -> deterministic initial family plan
  -> sequential validated writes
  -> final issue read and predicate evaluation
  -> at most one fresh corrective plan and second final read
  -> bounded canonical issue result or sanitized partial/unknown error
  -> terminal audit event
```

## Authorization and security

`issue_edit` requires exactly `issues:write`; `issues:read` alone is insufficient. Request validation precedes policy resolution, and repository authorization precedes every provider read and write. Unknown repository, unauthorized repository, absent capability, and wrong provider kind remain indistinguishable to clients.

The trusted resolved repository supplies canonical owner/name and provider ID. Input cannot select an adapter, authority, credential, raw path, retry count, or API method. Existing exact-authority token injection, TLS roots, strict redirect rejection, two-minute deadline, response budgets, concurrency limits, cancellation, gRPC retry disabling, and secret scrubbing remain in force.

Issue text, assignee/label names and IDs, indices, catalog contents, provider errors/bodies, authorities, and credentials are excluded from diagnostics and audit. Planner errors use stable internal categories only.

## Compatibility constraints

- Existing repository view, issue reads, create/comment/close/reopen, GitHub, and Git behavior remain unchanged.
- `issues edit` and `issues e` match the approved `tea 0.15.1` subset and E1-E3/E6-E7 semantics.
- Protocol changes are additive within `v1`; existing fields, tags, result shapes, and JSON keys are unchanged.
- The planner is internal and typed around issue-family predicates. It may later be reused by pull-request editing, but this issue adds no provider-neutral DTO, registry, generic command, or public planning API.
- Production continues to use the pinned SDK directly and contains no upstream `tea` binary.
- Completion targets the pinned current Gitea `1.27.2` fixture under ID-07. Broad certification remains deferred under ID-08.

## Verification strategy

These tests pass the Patchmill Testing Value Gate because they prove public mutation behavior, reusable planning rules, provider call bounds, concurrency outcomes, security boundaries, and regressions with lasting risk.

### Protocol, parser, and renderer

- Descriptor tests lock the fresh request/result tags, optional text presence, action oneofs, empty replacement representation, and unchanged predecessor tags.
- Parser tables cover both command aliases, every flag/alias, E1 precedence, no-op rejection, explicit body clearing, empty assignee replacement, Unicode/byte/list limits, duplicate flags/names, malformed CSV, extra positionals, unsupported flags, output defaults, and zero-RPC usage failures.
- Fuzz tests prove arbitrary argv cannot panic, bypass bounds, construct ambiguous actions, or echo attacker input in diagnostics.
- Renderer tests prove the existing canonical issue mutation output and reject wrong branches or malformed final records atomically.

### Planner and adapter

- Pure planner tables cover every predicate, no-op plans, exact replacement, add/remove preservation, stable sorting, fixed family order, and recomputation from a changed snapshot.
- Catalog tests cover absent-unneeded reads, assignable-user validation, label pagination through the page-21 probe, missing/duplicate names and IDs, exact case matching, response bounds, cancellation, and zero writes after preflight failure.
- Adapter tests assert exact SDK methods and arguments, current-value protection for text patches, batch assignee/label additions, sequential label deletion IDs, response validation, and the stated read/write maxima.
- Injected concurrent changes between preflight, writes, and final reads prove unrelated add/remove members survive, exact replacement converges, only unmet predicates are corrected, one correction succeeds, and persistent contention stops after the second final read.
- Failure matrices cover every step before invocation, after invocation, between confirmed steps, during each final read, and after reconciliation. They assert completed/partial/unknown classification, one invocation per planned step, no blind retry after uncertainty, and no partial output.

### Server, audit, and integration

- Server tests prove `issues:write`, anti-enumeration, trusted provider isolation, operation name, response bounds, accepted/terminal ordering, and content-free partial/unknown status details.
- Audit tests add `partial` to the closed outcome allowlist and prove that names, text, indices, IDs, provider data, and credentials never enter events.
- Extend the pinned Gitea integration fixture to edit title/body, replace then add/remove assignees, and add/remove labels; independently read the issue and assert canonical output and provider call bounds.
- A controlled concurrent actor changes assignees and labels between the initial write and final read; assert deterministic reconciliation and preservation/exactness according to the family predicates.
- A failure injected after an earlier confirmed family write proves the partial diagnostic, terminal `partial` audit, independent final state, and no unplanned calls. Retain the provider-response-loss case for `unknown`.

Existing Gitea reads/basic writes, GitHub, Git, security, fuzz, race, integration, generated-code, Nix/package, OCI, and release checks remain regression gates. Final verification includes `go tool buf lint`, generated-code freshness, `gofmt`, `go vet`, `go test -race`, tagged pinned-Gitea integration, `nix flake check`, release checks, and `git diff --check`.

## Non-goals

This issue does not add:

- pull-request reads or editing;
- milestones, deadlines, referenced versions, comments editing, state transitions within edit, or multi-index/batch commands;
- label replacement (`--set-labels`), rich label objects, case-insensitive name merging, or output diffs;
- unbounded retries, blind retry after uncertain writes, transactions, locks, idempotency keys, compare-and-swap, exactly-once, or lasting-state guarantees;
- repository inference, stdin, prompts, editors, arbitrary API calls, raw JSON forwarding, or a production `tea` dependency;
- provider-neutral forge models, public planner APIs, diagnostics/metrics expansion, reload, or broad compatibility certification.

## Acceptance criteria

1. The restricted client accepts exactly the documented single-index issue-edit forms, aliases, flags, precedence, presence rules, and bounds; zero-mutation and malformed commands make no RPC.
2. One additive typed protocol branch represents optional text and one effective assignee/label action without generic or provider-selecting fields.
3. Every edit requires `issues:write`, preserves repository anti-enumeration, and routes only through the trusted Gitea provider instance.
4. Preflight performs one issue read plus only required bounded assignee/label catalog reads, validates all write targets, and makes zero writes on failure.
5. A pure deterministic planner uses the documented family predicates, stable deltas, and fixed execution order; add/remove preserves unrelated members and set-assignees enforces exact set equality.
6. Every successful operation returns a normalized final issue read satisfying all effective predicates; already-satisfied edits use zero writes.
7. At most one fresh reconciliation plan runs after a concurrent mismatch, and all read/write call counts remain within the documented bounds.
8. An uncertain provider invocation is not blindly retried; a bounded final read may prove complete success, otherwise the existing unknown outcome is returned.
9. Proved incomplete multi-step edits return the sanitized partial status and terminal `partial` audit outcome without exposing issue content, names, IDs, provider data, or credentials.
10. Successful output remains the canonical bounded `index,title,state,url` issue mutation shape in simple, table, and JSON formats.
11. Unit, fuzz, service, concurrent-change, failure-injection, audit, and pinned-Gitea integration tests prove reconciliation, partial mutation, provider call bounds, and client-to-provider behavior.
12. Existing Gitea operations, GitHub, Git, protocol generation, security, packaging, and release checks remain green, and no pull-request editing or deferred feature is added.

## Open questions

None.
