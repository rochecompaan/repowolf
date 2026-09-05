# Gitea roadmap issue decomposition design

**Date:** 2026-09-02

**Status:** Approved for issue creation

## Context

The Gitea roadmap defines the complete first-release contract. It is too large for one implementation issue or one implementation plan.

RepoWolf is still an experimental project. The decomposition must favor useful increments, fast feedback, and reversible changes. It must not create premature operations work.

The implementation will use one serial work stream. Small foundation issues are acceptable when they unlock later vertical slices.

## Goals

This decomposition has these goals:

- Deliver Gitea Git access and collaboration features in useful increments.
- Keep each issue small enough for one focused specification and implementation plan.
- Preserve the security and compatibility rules in the roadmap.
- Make each merged issue safe to keep or revert.
- Use dogfooding results to change later issue boundaries.

The useful target includes these capabilities:

- Clone, fetch, and controlled push.
- Repository inspection through the restricted `tea` client.
- Issue read and write operations.
- Pull-request read and write operations.

## Non-goals

This decomposition does not schedule these topics:

- Full compatibility certification.
- A broad `config doctor` interface.
- Metrics work.
- Migration and rollback guides.
- Transactional reload.
- Exactly-once writes.
- Cross-repository pull heads.
- Generic comment commands.
- Remote metrics.

A later issue needs a concrete dogfooding result or release need before it adds one of these topics.

## Decomposition approach

The work uses a hybrid structure:

1. Merge the minimum shared foundations.
2. Complete the Git path.
3. Add one small API foundation.
4. Add repository, issue, and pull-request vertical slices.
5. Add hardening work only when actual use identifies a problem.

The protocol grows with each vertical slice. The implementation does not add every Gitea message before a command uses it.

Shared lifecycle extraction occurs when the first Gitea API slice creates a second provider path. This rule avoids an early provider framework.

## Issue sequence

### 1. Provider-instance runtime and credential isolation

**Outcome:** RepoWolf can construct isolated runtime state for each provider record without changing GitHub behavior.

**Scope:**

- Add the Gitea provider kind and the provider-instance configuration model.
- Add per-record adapter or client construction.
- Define `tokenEnv` and the narrow GitHub legacy-token migration.
- Render GitHub and token-free SSH child environments.
- Reject duplicate credential names and values.
- Reject case-folded Gitea repository slug collisions.
- Add observable GitHub parity tests.

**Non-goal:** Do not add a Gitea API operation or Gitea SDK dependency.

**Dependencies:** None.

### 2. Gitea Git clone and fetch

**Outcome:** An agent can clone and fetch one permitted Gitea repository without receiving a provider credential.

**Scope:**

- Generalize the Git SSH parser for configured users, hosts, ports, owners, and repository names.
- Preserve the existing GitHub grammar and behavior.
- Resolve Git repositories by provider kind.
- Route Gitea `upload-pack` through the existing Git service and policy path.
- Add clone, fetch, denial, malformed-input, and credential-leak tests.

**Non-goal:** Do not add push or Gitea API access.

**Dependencies:** Issue 1.

### 3. Controlled Gitea Git push

**Outcome:** An agent can push an allowed change to a permitted Gitea repository.

**Scope:**

- Route Gitea `receive-pack` through the existing Git service.
- Apply the existing push policies without provider-specific exceptions.
- Preserve audit order, limits, and credential isolation.
- Add allowed-push, denied-push, cancellation, and leak tests.

**Non-goal:** Do not change the push-policy model.

**Dependencies:** Issue 2.

### 4. Secure Gitea API transport

**Outcome:** RepoWolf has one bounded Gitea SDK transport that is ready for API operations.

**Scope:**

- Move Go, Nix, CI, OCI, and release builds to Go 1.26.
- Add the pinned Gitea SDK.
- Add `internal/providerhttp` as the transport boundary.
- Add exact-authority token injection.
- Add trust-root handling and strict redirect rejection.
- Add operation deadlines and response budgets.
- Construct one SDK client for each Gitea provider record.
- Add fake-server transport tests.

**Non-goal:** Do not add gRPC methods or restricted `tea` commands.

**Dependencies:** Issue 1.

### 5. Repository view through restricted `tea`

**Outcome:** An agent can inspect one configured Gitea repository through the restricted `tea` client.

**Scope:**

- Add the minimal Gitea service, request, response, and repository messages.
- Add the `tea` client personality and strict parser shell.
- Add repository-view parsing and rendering.
- Add the SDK repository adapter operation.
- Extract the shared provider request lifecycle now that two API providers exist.
- Add bounded terminal audit metadata.
- Add fake-adapter tests and one real Gitea integration test.

**Non-goal:** Do not add issue or pull-request messages.

**Dependencies:** Issue 4.

### 6. Issue read operations

**Outcome:** An agent can list and inspect Gitea issues.

**Scope:**

- Add issue list and view requests and responses.
- Add the approved issue fields and output formats.
- Add explicit comment-page retrieval.
- Add issue-kind validation and anti-enumeration behavior.
- Add pagination, ordering, presence, and response-limit tests.

**Non-goal:** Do not add issue mutations.

**Dependencies:** Issue 5.

### 7. Basic issue writes

**Outcome:** An agent can create, comment on, close, and reopen a Gitea issue.

**Scope:**

- Add issue create.
- Add the kind-explicit issue comment command.
- Add issue close and reopen.
- Add sanitized write errors and terminal audit results.
- Define unknown outcomes for non-idempotent writes.
- Prove that no write path retries automatically.

**Non-goal:** Do not add general issue editing or exact-replacement list changes.

**Dependencies:** Issue 6.

### 8. Issue editing

**Outcome:** An agent can edit issue text, assignees, and labels with bounded concurrency behavior.

**Scope:**

- Add title and body edits.
- Add assignee replacement, addition, and removal.
- Add label addition and removal.
- Add bounded preflight collection reads.
- Add family-specific predicates, deterministic plans, reconciliation, and final reads.
- Add concurrent-change and partial-mutation tests.

**Non-goal:** Do not add pull-request editing.

**Dependencies:** Issue 7.

### 9. Pull-request read operations

**Outcome:** An agent can list and inspect Gitea pull requests.

**Scope:**

- Add pull-request list and view requests and responses.
- Add explicit comment-page retrieval.
- Add bounded all-page review hydration.
- Add user and team review actors.
- Recover raw `mergeable` and `allow_maintainer_edit` presence.
- Add ordering, presence, pagination, and response-limit tests.

**Non-goal:** Do not add pull-request mutations.

**Dependencies:** Issue 5. The serial stream schedules this issue after Issue 8.

### 10. Basic pull-request writes

**Outcome:** An agent can create, comment on, close, and reopen a Gitea pull request.

**Scope:**

- Add same-repository pull-request creation.
- Add the kind-explicit pull-request comment command.
- Add pull-request close and reopen.
- Treat maintainer-edit correction as one compound mutation.
- Add final confirmation, partial outcomes, unknown outcomes, and no-retry tests.

**Non-goal:** Do not add general pull-request editing or cross-repository heads.

**Dependencies:** Issue 9.

### 11. Pull-request editing

**Outcome:** An agent can edit pull-request text, membership fields, reviewers, and draft state.

**Scope:**

- Add title and body edits.
- Add assignee and label changes.
- Add reviewer addition and removal.
- Add draft and ready transitions.
- Reuse the bounded mutation planner and family predicates.
- Add concurrent-change, reconciliation, and partial-mutation tests.

**Non-goal:** Do not add merge operations or cross-repository heads.

**Dependencies:** Issue 10.

## Source-of-truth rules

The Gitea roadmap remains the source for cross-cutting contracts. These contracts include security, limits, errors, audit behavior, and deferred scope.

Each issue specification owns only its local interface and behavior. It references the applicable roadmap decisions instead of copying the full text.

If implementation discovery conflicts with the roadmap, the issue must update the roadmap explicitly. An issue specification must not create an implicit exception.

The parent roadmap issue tracks issue order, dependencies, demonstrations, and sequence changes. It also records new work from dogfooding.

## Issue artifact contract

### Issue body

Each issue body contains:

- One observable outcome.
- Its predecessor issues.
- The shortest useful demonstration.
- Explicit non-goals.
- Applicable roadmap decision identifiers.
- Links to the approved specification and implementation plan.

### Issue specification

Each issue specification uses this path:

`docs/specs/YYYY-MM-DD-gitea-<issue-name>-design.md`

Each specification covers:

1. Existing behavior and affected modules.
2. The exact interface or behavior change.
3. Request and data flow.
4. Security and error behavior.
5. Compatibility constraints.
6. Test strategy.
7. Non-goals.
8. Acceptance criteria.

### Implementation plan

Each implementation plan uses this path:

`docs/plans/YYYY-MM-DD-gitea-<issue-name>.md`

Each plan contains:

- Exact files and symbols.
- Small test-driven steps.
- Commands that prove each step.
- Integration-test setup.
- Commit checkpoints.
- One final end-to-end demonstration.

Mechanical work stays in the applicable plan. It becomes a separate issue only when discovery shows independent risk or value.

## Execution model

The project creates lightweight stubs for all eleven issues. The project writes detailed specifications and plans only for the next unblocked issue.

Each issue uses its own branch and worktree. The serial stream has one active implementation issue.

After each merge, the project reviews the next boundary. The project can split, combine, or reorder later issues when evidence supports the change.

The project does not preserve an obsolete decomposition only because issue stubs already exist.

## Lean completion standard

An issue is complete when all applicable conditions are true:

- Its useful path works from the client to the provider.
- Automated tests cover security-sensitive and reusable behavior.
- Existing GitHub regression tests pass.
- One current pinned Gitea image exercises the vertical slice.
- The change is easy to revert.
- The issue documents known limits.

A broader version matrix runs before a release-readiness claim. It is not a separate near-term implementation issue.

## Release-readiness checkpoint

Issue 11 ends the planned implementation sequence. The project then reviews actual use and open defects.

This checkpoint can create focused work for diagnostics, compatibility, packaging, or documentation. It does not make those topics mandatory.

## Decisions

- **ID-01:** Use one serial implementation stream.
- **ID-02:** Use thin foundations followed by vertical slices.
- **ID-03:** Complete the Git path before the Gitea API feature path.
- **ID-04:** Grow the protocol with each vertical slice.
- **ID-05:** Extract shared lifecycle code only when the first Gitea API slice needs it.
- **ID-06:** Write specifications and plans just before implementation.
- **ID-07:** Use one current Gitea image for issue completion.
- **ID-08:** Defer broad operations and certification work until actual use creates a need.

## Open questions

None.
