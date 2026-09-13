# Gitea basic issue writes design

**Date:** 2026-09-12

**Status:** Approved for implementation planning

## Context

Issue #10 establishes the Gitea issue-read path: typed issue records, strict `tea issues` parsing, trusted provider-instance dispatch, SDK-native issue access, kind validation, bounded rendering, and accepted/terminal audit events. This issue adds the smallest useful write slice on that path.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-04, ID-06, ID-07, and ID-08. Its implementation baseline is the completed issue-#10 branch even if this specification branch predates that merge.

No `AGENTS.md` exists in this worktree or its repository parents.

## Outcome

An agent with `issues:write` can create an issue, add an issue comment, close an issue, and reopen an issue in one authorized configured Gitea repository. Each command makes a bounded number of provider calls, returns a validated canonical record, emits safe terminal audit metadata, and never retries a provider write automatically.

Create and comment are non-idempotent. If RepoWolf cannot prove whether one of those provider calls took effect, it reports an explicit unknown outcome rather than a generic failure or an automatic retry.

## Approaches considered

### Extend the existing Gitea issue vertical slice

This is the selected approach. Add four typed request/result branches, extend the strict issue command parser and mutation renderer, and add focused write methods to the existing Gitea adapter. Reuse issue normalization, authorization, provider dispatch, response limits, and lifecycle auditing.

This keeps provider behavior local and introduces no generic mutation framework before issue editing needs one.

### Translate writes through an upstream `tea` subprocess

This would inherit more CLI behavior but would put `tea` in the production path, require another credential-bearing process boundary, and conflict with the approved SDK-native architecture. It is rejected.

### Add issue editing and a general comment abstraction now

A shared edit planner or issue/pull comment abstraction might reduce later duplication, but neither seam is proven by this slice. It would also expand the issue beyond create/comment/state transitions. It is rejected under ID-02 and ID-08.

## Existing behavior and affected components

The issue-#10 baseline provides these seams:

- `proto/repowolf/v1/gitea.proto` contains repository view and issue list/view branches plus `GiteaIssueRecord` and `GiteaCommentRecord`.
- `internal/client/gitea` parses and renders the restricted repository and issue-read commands.
- `internal/provider/gitea.RepositoryAdapter` validates and executes typed requests through one context-aware SDK seam.
- `internal/server/gitea.go` maps each operation to a capability, resolves the repository, invokes the trusted provider instance, bounds the response, and participates in the shared audit lifecycle.
- `internal/app/gitea_executor.go` prevents cross-provider routing.
- the pinned Gitea integration fixture exercises the packaged `tea` personality through the broker.

Affected areas are the additive Gitea protocol and generated code, Gitea client parser/renderer, provider SDK seam and write normalization, sanitized RPC status mapping, terminal audit classification, server tests, and the real Gitea integration fixture. Shared provider transport and runtime composition need no new abstraction.

## Restricted command contract

Every command requires `--repo`/`-r OWNER/REPO`. Mutation output defaults to `simple`; `--output`/`-o simple|table|json` remains accepted. Output never depends on a TTY.

### Create

```text
tea issues create --repo OWNER/REPO --title TITLE
  [--description BODY] [--assignees NAME[,NAME...]] [--labels NAME[,NAME...]]
tea issues c ...
```

`create` and `c` are aliases. `--title`/`-t` is required, non-empty, and at most 255 Unicode characters. `--description`/`-d` is optional and may be explicitly empty; it is at most 64 KiB. `--assignees`/`-a` and `--labels`/`-L` are optional comma-separated lists of at most 25 non-empty entries, each at most 255 Unicode characters. Duplicate entries are a usage error; matching remains case-sensitive so RepoWolf does not silently merge distinct provider names. NUL and invalid UTF-8 are forbidden in every string.

Gitea's create API accepts assignee names but label IDs. When labels are requested, the adapter resolves their names before the write by listing repository labels sequentially in pages of 50. It accepts at most 20 full pages (1,000 labels) and probes page 21 after a full page 20 to distinguish exact completion from overflow. Every provider label must have a positive unique ID and a non-empty unique case-sensitive name. Every requested name must match exactly once; an absent or ambiguous name is a sanitized failed precondition. Any lookup, bound, cancellation, or validation failure occurs before the write and returns no partial result. The resolved IDs preserve requested order.

After optional label resolution, the command makes exactly one create write containing the title, description presence, assignee names, and resolved label IDs. RepoWolf does not emulate create with follow-up edits.

### Comment

```text
tea comments add INDEX [BODY] --repo OWNER/REPO
tea comments add INDEX --description BODY --repo OWNER/REPO
tea comments a INDEX ...
tea comment INDEX ...
tea comments INDEX ...
tea c INDEX ...
```

These are the generic `tea 0.15.1` spellings retained by the authoritative roadmap, but they always produce the kind-explicit typed `issue_comment` operation in this issue slice. The adapter proves that the index denotes an issue before writing; a later pull-request slice must add its own typed operation and cannot infer kind from an unvalidated shared index. Exactly one positional body or `--description`/`-d` is required. The body must be non-empty and at most 64 KiB. Stdin, prompts, and editor fallback are rejected.

A pull-request index receives the existing corrective kind error after authorization and no comment is created.

### Close and reopen

```text
tea issues close INDEX --repo OWNER/REPO
tea issues reopen INDEX --repo OWNER/REPO
tea issues open INDEX --repo OWNER/REPO
```

`reopen` and `open` are aliases. These commands accept only repository and output flags.

Close and reopen are idempotent ensure-state operations. The adapter first reads the index to validate kind and current state. If it is already in the requested state, the command succeeds without a write. Otherwise the adapter makes one state-change call and validates that the returned issue has the requested index, kind, repository, and final state. It does not retry either call.

All commands retain the existing client-wide limits of at most 64 arguments and 64 KiB of argv. Long and short forms of the same flag count as duplicates. `--flag=value`, extra positionals, unsupported flags, and all issue-edit flags are rejected. Usage failures exit 2 without an RPC. Runtime failures exit 1 with sanitized diagnostics.

## Protocol and output contract

Extend `GiteaRequest.operation` and `GiteaResponse.result` additively with fresh branches for:

- `issue_create`;
- `issue_comment`;
- `issue_close`; and
- `issue_reopen`.

Requests contain only typed values:

- create: title, optional description, repeated assignees, and repeated labels;
- comment: positive issue index and body;
- close/reopen: positive issue index.

Create returns one `GiteaIssueRecord`. Close and reopen also return one issue record in its final state. Comment returns one `GiteaCommentRecord`. Existing record types and semantic validation are reused; no raw SDK record, response body, arbitrary path, provider ID, endpoint, or generic key/value field enters the protocol.

Mutation output contains only canonical keys:

- issue mutations: `index`, `title`, `state`, `url`;
- comment: `id`, `url`, `body`.

`simple` renders fixed-order labeled lines, `table` renders one header and one data row, and `json` renders one typed object followed by a newline. The client validates the response branch, non-empty request ID, required values, positive IDs, enums, UTF-8, and timestamps before preparing the complete output. Existing 8 MiB Protobuf and rendered-output bounds remain unchanged, and no partial output is written after validation failure.

All enum zero values remain `UNSPECIFIED`, existing tags and names remain unchanged, and generated code changes only through `scripts/generate.sh`.

## Request and provider flow

```text
restricted `tea issues` mutation
  -> strict local parsing and bounds
  -> typed GiteaRequest over authenticated TLS gRPC
  -> server request validation
  -> exact issues:write policy resolution and accepted audit event
  -> trusted provider ID selects one immutable Gitea adapter
  -> optional bounded label lookup or issue kind/state read
  -> at most one SDK write against canonical owner/name
  -> complete provider response validation and normalization
  -> bounded typed response and deterministic rendering
  -> terminal audit event
```

Create performs zero to 21 sequential label-list calls when labels are present, then one SDK issue-create request containing all initial fields. Comment performs one issue read followed by at most one comment-create request. Close/reopen perform one issue read followed by zero or one state update. Calls are sequential and context-aware; none is retried.

The adapter validates every successful provider response before returning it. A nil response, wrong repository or index, pull-request marker, missing canonical fields, invalid state, invalid timestamp, malformed nested value, or response above a bound fails without partial output.

## Authorization and security

All four operations require exactly `issues:write`. Possessing only `issues:read` is insufficient. Request validation occurs before policy resolution, and exact repository authorization occurs before any provider read or write.

Unknown repositories, unauthorized repositories, missing capability, and wrong provider kind remain indistinguishable to clients. Provider selection uses only the trusted resolved repository. Client input cannot select an adapter, provider ID, authority, credential, or raw API path.

Issue and comment text, labels, assignees, indices, provider errors, API response bodies, and credentials are excluded from audit records and client diagnostics. The existing exact-authority token injection, TLS roots, redirect rejection, operation deadline, 8 MiB provider-response budget, concurrency limits, cancellation, and secret scrubbing remain in force.

## Write errors, unknown outcomes, and retries

Errors before a provider write begins are known failures: invalid requests, denials, accepted-audit failure, label-resolution failure, failed kind/state preflight, already-cancelled contexts, and other preconditions make no write call.

A validated success response proves provider completion. A validated preflight showing that close/reopen already has the requested state also proves completion without a transition.

The adapter sets an unexported trusted `writeAttempted` marker immediately before calling the SDK create-comment, create-issue, or state-update method. It never derives this marker from an SDK error or provider text. Once marked, any SDK error or incomplete/invalid success response becomes the internal `ErrWriteOutcomeUnknown`; this includes cancellation, deadline expiry, connection loss, provider error status, response-read/limit failure, and malformed success data. Thus an SDK-local failure is conservatively unknown once invocation starts, while every lookup and validation failure before invocation is known not to have written.

`rpcstatus.Error` maps only that trusted domain error to gRPC `Unavailable` with `google.rpc.ErrorInfo{reason: "GITEA_WRITE_OUTCOME_UNKNOWN", domain: "repowolf.dev/gitea"}`. It preserves only this exact self-produced detail through the outer status interceptor; arbitrary status details and provider errors remain canonicalized. The client recognizes the exact code/reason/domain tuple. For it, any unavailable, cancelled, deadline, internal, or response-limit result after starting a mutation RPC is also conservatively unknown because the response may have been lost after server completion. It prints only:

```text
tea: write outcome unknown; inspect repository state before retrying
```

The server audit records provider truth, not client delivery. It records `completed` after obtaining and validating the provider response even if the gRPC response is subsequently lost or exceeds the final message bound; the client may therefore report unknown while the server terminal event correctly says completed. The server records `unknown` only when it cannot establish the provider result. An ambiguous close/reopen update follows the same rule. Close/reopen remains safe for a caller to invoke again, but RepoWolf does not retry or reconcile it automatically.

Every provider lookup and write invocation is single-attempt: there is no retry loop, retrying HTTP round tripper, backoff, hidden SDK retry, or resubmission after cancellation. The Gitea client connection disables configured gRPC retries. gRPC's library-level transparent retry is acceptable only before request bytes reach a server or when the remote server has not processed the RPC; tests prove it cannot cause the service handler or provider write to run twice. Failure-injection tests count SDK calls at pre-invocation, request, response-read, final-response, and client-response-loss boundaries.

## Audit behavior

Accepted events retain the existing bounded fields and use these operation names:

- `gitea.issue_create`;
- `gitea.issue_comment`;
- `gitea.issue_close`;
- `gitea.issue_reopen`.

Add `unknown` to the closed audit outcome set. A trusted internal unknown-outcome marker controls terminal classification; arbitrary provider or client error text cannot select it. Terminal outcomes are:

- `completed` after a validated mutation response or a close/reopen no-op;
- `failed` when RepoWolf proves no write began;
- `unknown` when a dispatched write lacks a provable result;
- `denied` for policy denial; and
- `cancelled` only when cancellation occurs before a write begins.

Close/reopen terminal events add one optional bounded boolean, `transitioned`, after the adapter knows the result: `false` for an already-satisfied no-op and `true` for a validated state transition. Other operations omit it. The terminal reason is a stable category or canonical status, never raw error text.

Accepted-audit failure still prevents provider work and therefore cannot promise an accepted record. Existing terminal-audit sink behavior remains unchanged. After an accepted event is written successfully, exactly one terminal event is attempted for the command; an early rejection has only its existing terminal event.

## Compatibility constraints

- Issue list/view and repository view from predecessors remain unchanged.
- GitHub, Git, configuration, packaging, and conditional Gitea service registration behavior remain unchanged.
- Protocol and JSON changes are additive within `v1`; existing fields are not renamed, removed, or retyped.
- No production artifact contains upstream `tea`, and no provider registry or generic forge DTO is introduced.
- Provider calls always use canonical configured owner/name and the adapter selected from the trusted provider ID.
- The implementation targets the existing pinned current Gitea `1.27.2` fixture under ID-07. Broad version certification remains deferred under ID-08.

## Verification strategy

These tests pass the Patchmill Testing Value Gate because they exercise public mutation behavior, security boundaries, non-idempotent uncertainty, bounded audit semantics, and provider call counts.

### Protocol, parser, and renderer

- Descriptor tests lock fresh branch tags, typed fields, optional description presence, and unchanged predecessor tags.
- Parser tables cover aliases, defaults, positional versus flagged comments, Unicode character versus byte bounds, empty/maximum/over-limit values, CSV limits and duplicates, index bounds, duplicate flags, edit flags, stdin/interactivity, and representative malformed argv.
- Fuzz tests prove arbitrary argv cannot panic, bypass bounds, create an invalid typed request, or echo attacker input in diagnostics.
- Renderer tests prove exact simple/table/JSON output, canonical key order, typed values, branch validation, response semantic validation, output limits, and no partial writes.

### Adapter, server, and audit

- Adapter tests assert exact canonical SDK arguments and one create/comment/state-write attempt; initial create fields use one write rather than follow-up edits.
- Label-resolution tests cover no-label zero-call behavior, short and multiple pages, exact 1,000-label completion with page-21 probe, overflow, exact case-sensitive matching, missing/duplicate names and IDs, stable requested order, cancellation, sanitized failures, and no create call after lookup failure.
- Kind tests prove comment/close/reopen reject pull-request indices before mutation.
- State tests prove close/reopen no-op success, one-call transition success, returned-state validation, and no retry after any write error.
- Failure-injection tests cover cancellation and transport failure before invocation, during request handling, while reading a provider response, during final server response processing, and after server completion but before client receipt. They assert SDK and handler call counts, trusted status-detail preservation, client diagnostics, server audit truth, and `failed` versus `unknown` outcomes.
- Server tests prove `issues:write` enforcement, repository anti-enumeration, validation before policy/provider work, provider-instance isolation, response bounds, canonical operations, accepted/terminal ordering, and absence of request content and secrets from errors and audits.
- Audit tests cover `transitioned` presence and both values, the closed outcome allowlist, and exactly one terminal event.

### Real Gitea integration

Extend the digest-pinned Gitea `1.27.2` fixture and exercise the packaged client through TLS:

1. create an issue with title, description, assignees, and labels;
2. add a comment through a documented `tea` alias and verify it becomes the typed `issue_comment` operation;
3. close it and close it again to prove ensure-state no-op behavior;
4. reopen it and view it through the read path.

Assert canonical mutation output, final issue/comment content through an independent read, exact provider call order where observable, accepted/completed audit pairs, `transitioned` values, and absence of provider credentials and write bodies from environment, arguments, diagnostics, and audit. A separate injected ambiguous-response test proves the client receives the unknown diagnostic, the audit records `unknown`, and the write call count remains one.

Existing Gitea read, GitHub, Git, security, fuzz, race, integration, generated-code, Nix/package, OCI, and release checks remain regression gates. Final verification includes `go vet`, `go test -race`, `buf lint`, breaking/generated checks, tagged Gitea integration, and `git diff --check`.

## Non-goals

This issue does not add:

- general issue title/body editing;
- assignee or label replacement, addition, or removal on an existing issue;
- pull-request reads or writes;
- a provider-neutral comment protocol operation, or comment list/edit/delete;
- exact-replacement list mutations, milestones, deadlines, or referenced versions;
- multi-index mutation, batch writes, idempotency keys, automatic retries, reconciliation loops, or exactly-once claims;
- repository inference, prompts, editors, stdin bodies, arbitrary API calls, or raw JSON forwarding;
- a generic mutation planner, provider registry, production `tea` dependency, diagnostics, metrics, reload, or broad certification.

## Acceptance criteria

1. The restricted client accepts only the documented create, comment, close, and reopen forms, flags, aliases, and bounds; every accepted comment alias maps explicitly to typed `issue_comment`.
2. The additive protocol represents all four requests and canonical results without raw or generic fields.
3. Every operation requires `issues:write`, preserves repository anti-enumeration, and routes only to the trusted Gitea provider instance.
4. Create resolves requested label names through at most 1,000 bounded repository labels, then uses one SDK write containing title, optional description, assignees, and label IDs; comment validates issue kind then uses one comment write.
5. Close/reopen are ensure-state operations: an already-satisfied request succeeds without a write, and a transition makes at most one write.
6. Successful responses are fully validated and render deterministic bounded simple, table, and JSON output with canonical keys.
7. Dispatched writes with no provable result return the sanitized unknown-outcome status and terminal audit outcome; no raw provider data is exposed.
8. No provider write path retries automatically, including after cancellation, timeout, connection loss, malformed response, or output-limit failure.
9. Audit events use canonical operations, safe terminal outcomes, and bounded close/reopen transition metadata without indices, user content, provider errors, or credentials.
10. Unit, fuzz, service, failure-injection, audit, and one pinned-Gitea integration demonstration prove the complete client-to-provider behavior and no-retry contract.
11. Existing Gitea reads, GitHub, Git, security, generated-code, packaging, and release checks remain green.
12. No issue editing, exact-replacement list change, pull-request operation, provider-neutral comment operation, or automatic reconciliation is added.

## Open questions

None.
