# Gitea Basic Issue Writes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authorized restricted `tea` client create, comment on, close, and reopen Gitea issues with deterministic output, sanitized uncertainty, bounded audit records, and no automatic write retry.

**Architecture:** Extend the issue-#10 Gitea vertical slice with four additive typed operations. Keep parsing/rendering in `internal/client/gitea`, provider-specific preflight and one-shot SDK writes in focused adapter files, and trusted uncertainty/audit classification in `rpcstatus` and the server lifecycle; no generic mutation or provider-neutral comment framework is introduced.

**Tech Stack:** Go, Protocol Buffers/gRPC, `gitea.dev/sdk`, the existing provider HTTP transport, JSON/table renderers, Docker-backed Gitea 1.27.2 integration tests, Nix.

**Spec:** `docs/specs/2026-09-12-issue-11-gitea-roadmap-7-11-basic-issue-writes-design.md`

## Global Constraints

- Start implementation from the completed issue-#10 baseline (`origin/agent/issue-10-gitea-roadmap-6-11-issue-read-operations` as inspected while planning); do not recreate or weaken its issue-read behavior.
- Every mutation requires exactly `issues:write`; `issues:read` alone is insufficient.
- Every command requires `--repo`/`-r OWNER/REPO`; mutation output defaults to `simple`, and `simple|table|json` never depends on a TTY.
- Keep the existing limits of 64 arguments, 64 KiB argv, 8 MiB Protobuf responses, and 8 MiB rendered output.
- Create title is non-empty and at most 255 Unicode characters; description is optional-with-presence and at most 64 KiB; assignees and labels are case-sensitive unique CSV lists of at most 25 non-empty values, each at most 255 Unicode characters.
- Comment body is exactly one non-empty positional body or `--description`/`-d`, at most 64 KiB; prompts, editor fallback, and stdin are rejected.
- Reject invalid UTF-8 and NUL in every string, duplicate long/short forms, `--flag=value`, extra positionals, unsupported flags, and issue-edit flags.
- Resolve create labels sequentially in pages of 50, accept at most 1,000 labels, and probe page 21 after a full page 20; validate positive unique IDs and non-empty unique case-sensitive names before the write.
- Create and comment make exactly one non-idempotent write after all preconditions. Close/reopen read once and make zero or one state update. No lookup or write is retried.
- Set trusted write-attempt state immediately before SDK invocation. Any error or invalid/incomplete response after invocation is `ErrWriteOutcomeUnknown`; pre-invocation failures remain known failures.
- Preserve only the self-produced `Unavailable` + `GITEA_WRITE_OUTCOME_UNKNOWN` + `repowolf.dev/gitea` status detail. Never preserve provider status details or raw error text.
- Mutation diagnostics, errors, and audits exclude issue/comment text, labels, assignees, indices, provider bodies/errors, authorities, and credentials.
- Add only issue create, issue comment, close, and reopen. Do not add general editing, exact-replacement list changes, pull-request writes, idempotency keys, retries, or reconciliation.
- Generate Protobuf artifacts only with `scripts/generate.sh`; do not hand-edit `gen/`.
- Apply the Testing Value Gate: the planned protocol, parser, renderer, adapter, failure-injection, security, audit, and integration tests prove risky reusable behavior and meaningful regressions. Do not add tests that merely restate fixture YAML or documentation; verify those directly.

## File Structure

### New files

- `internal/client/gitea/issue_mutation_parse_test.go`: closed mutation grammar, aliases, boundary tables, and no-RPC usage behavior.
- `internal/client/gitea/issue_mutation_render.go`: canonical issue/comment mutation validation and simple/table/JSON rendering.
- `internal/client/gitea/issue_mutation_render_test.go`: exact output, semantic rejection, output bounds, and atomic-write tests.
- `internal/provider/gitea/issue_create.go`: bounded label resolution and one-shot issue creation.
- `internal/provider/gitea/issue_create_test.go`: label paging/validation, canonical arguments, and no-write precondition tests.
- `internal/provider/gitea/issue_mutation.go`: issue-kind preflight, comment creation, and ensure-state close/reopen.
- `internal/provider/gitea/issue_mutation_test.go`: kind, no-op, transition, response-validation, and write-count tests.
- `internal/provider/gitea/write_outcome.go`: private write-attempt boundary and trusted `ErrWriteOutcomeUnknown` classification.
- `internal/provider/gitea/write_outcome_test.go`: before/during/after invocation failure matrix and one-attempt proof.
- `internal/server/gitea_write_test.go`: capability, anti-enumeration, provider isolation, audit operation/outcome/metadata, and response-limit tests.
- `internal/client/gitea/write_outcome_test.go`: exact unknown tuple handling, conservative post-RPC uncertainty, and sanitized diagnostics.
- `integration/gitea_issue_writes_test.go`: pinned-Gitea create/comment/close/no-op/reopen demonstration and independent state checks.

### Modified files

- `proto/repowolf/v1/gitea.proto`: additive request/result branches and typed mutation messages.
- `gen/repowolf/v1/gitea.pb.go`, `gen/repowolf/v1/gitea_grpc.pb.go`: generated protocol output only.
- `internal/provider/gitea/operation.go`: mutation validation, `issues:write`, and canonical operation names.
- `internal/provider/gitea/adapter.go`: extend the narrow SDK seams and dispatch mutation operations.
- `internal/provider/gitea/adapter_test.go`: SDK-service capture and operation dispatch coverage.
- `internal/provider/gitea/issue_normalize.go`: reuse/expose focused projection needed for complete mutation records.
- `internal/client/gitea/parse.go`: dispatch create/comment/close/reopen grammar.
- `internal/client/gitea/client.go`: mutation-aware unknown diagnostic without changing read diagnostics.
- `internal/client/gitea/render.go`: dispatch mutation result rendering.
- `internal/client/gitea/fuzz_test.go`: accepted mutation request invariants and bounded safe diagnostics.
- `internal/rpcstatus/status.go`, `internal/rpcstatus/status_test.go`: trusted unknown-outcome status creation and exact detail recognition.
- `internal/clientconfig/dial.go`, `internal/clientconfig/dial_test.go`: disable configured gRPC retries and prove one service-handler invocation.
- `internal/audit/event.go`, `internal/audit/writer_test.go`: closed `unknown` outcome and optional bounded `transitioned` metadata.
- `internal/server/audit.go`: classify trusted unknown outcomes and copy only trusted transition metadata.
- `internal/server/gitea.go`: carry mutation completion facts into terminal audit metadata.
- `internal/server/gitea_test.go`: retain read behavior while extending authorization/lifecycle coverage.
- `integration/gitea_fixture_test.go`: fixture support for write-path request counting/failure injection.
- `integration/testdata/gitea-policy.yaml`: grant `issues:write` to the existing authorized repository.

---

### Task 1: Add the typed mutation protocol and operation contract

**Files:**
- Modify: `proto/repowolf/v1/gitea.proto`
- Modify (generated): `gen/repowolf/v1/gitea.pb.go`
- Modify (generated): `gen/repowolf/v1/gitea_grpc.pb.go`
- Modify: `internal/provider/gitea/operation.go`
- Modify: `internal/provider/gitea/adapter_test.go`

**Interfaces:**
- Consumes: issue-#10 `GiteaRequest`, `GiteaResponse`, `GiteaIssueRecord`, and `GiteaCommentRecord`.
- Produces: `GiteaIssueCreateRequest`, `GiteaIssueCommentRequest`, `GiteaIssueCloseRequest`, and `GiteaIssueReopenRequest`.
- Produces: result wrappers returning exactly one existing issue/comment record.
- Produces: `Capability(request) == config.IssuesWrite` and canonical operation names for all four branches.

- [ ] **Step 1: Write failing descriptor and operation tests**

Extend the descriptor test beside the issue-read protocol tests to require unchanged tags 10-12 and fresh tags 13-16 on both oneofs. Assert description has proto3 presence and all request fields are typed rather than generic maps or raw JSON.

Add operation table rows equivalent to:

```go
{
	name: "create",
	request: mutationRequest(&repowolfv1.GiteaRequest_IssueCreate{IssueCreate: &repowolfv1.GiteaIssueCreateRequest{Title: "title"}}),
	operation: "gitea.issue_create",
},
{
	name: "comment",
	request: mutationRequest(&repowolfv1.GiteaRequest_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentRequest{Index: 7, Body: "body"}}),
	operation: "gitea.issue_comment",
},
{
	name: "close",
	request: mutationRequest(&repowolfv1.GiteaRequest_IssueClose{IssueClose: &repowolfv1.GiteaIssueCloseRequest{Index: 7}}),
	operation: "gitea.issue_close",
},
{
	name: "reopen",
	request: mutationRequest(&repowolfv1.GiteaRequest_IssueReopen{IssueReopen: &repowolfv1.GiteaIssueReopenRequest{Index: 7}}),
	operation: "gitea.issue_reopen",
},
```

Require `config.IssuesWrite` for every row. Add malformed rows for zero index, empty/over-limit strings, invalid UTF-8/NUL, duplicate/empty/over-limit CSV values, more than 25 entries, and a request with no branch.

- [ ] **Step 2: Run the focused tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'TestGiteaMutationProtocolDescriptor|TestGiteaOperation|TestValidateMutation' -count=1
```

Expected: FAIL because mutation messages and branches do not exist.

- [ ] **Step 3: Add additive protocol messages**

Use tags 13-16 without changing predecessor tags:

```proto
message GiteaIssueCreateRequest {
  string title = 1;
  optional string description = 2;
  repeated string assignees = 3;
  repeated string labels = 4;
}
message GiteaIssueCommentRequest { int64 index = 1; string body = 2; }
message GiteaIssueCloseRequest { int64 index = 1; }
message GiteaIssueReopenRequest { int64 index = 1; }

message GiteaIssueCreateResult { GiteaIssueRecord issue = 1; }
message GiteaIssueCommentResult { GiteaCommentRecord comment = 1; }
message GiteaIssueCloseResult { GiteaIssueRecord issue = 1; }
message GiteaIssueReopenResult { GiteaIssueRecord issue = 1; }
```

Add matching oneof branches named `issue_create`, `issue_comment`, `issue_close`, and `issue_reopen` with tags 13-16 in request and response.

- [ ] **Step 4: Generate code and implement strict request validation**

Run `scripts/generate.sh`. In `operation.go`, add private validators that count Unicode code points with `utf8.RuneCountInString`, while enforcing byte limits for 64 KiB bodies. Preserve optional description presence, reject duplicates case-sensitively, and return only `ErrInvalidRequest` to callers.

Extend `Capability` and `OperationName` with an explicit closed switch; do not let a default branch silently classify future operations.

- [ ] **Step 5: Validate protocol generation and package behavior**

```bash
gofmt -w internal/provider/gitea/operation.go internal/provider/gitea/adapter_test.go
go tool buf lint
scripts/check-generated.sh
go test ./internal/provider/gitea -run 'TestGiteaMutationProtocolDescriptor|TestGiteaOperation|TestValidateMutation' -count=1
```

Expected: PASS; generated output is reproducible and predecessor tags remain unchanged.

- [ ] **Step 6: Commit the protocol contract**

```bash
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go internal/provider/gitea/operation.go internal/provider/gitea/adapter_test.go
git commit -m "feat(gitea): add typed issue write protocol"
```

---

### Task 2: Parse the restricted mutation command grammar

**Files:**
- Modify: `internal/client/gitea/parse.go`
- Create: `internal/client/gitea/issue_mutation_parse_test.go`
- Modify: `internal/client/gitea/fuzz_test.go`

**Interfaces:**
- Consumes: Task 1 request branches and the existing `command{request, format}` type.
- Produces: typed requests for `tea issues create|c`, generic comment aliases, `issues close`, and `issues reopen|open`.
- Produces: a private mutation discriminator used by Task 3 to choose uncertainty diagnostics.

- [ ] **Step 1: Write the failing accepted-command table**

Cover these spellings and assert the exact typed request, repository context, description presence, and output format:

```text
issues create --repo Owner/Repo --title title
issues c -r Owner/Repo -t title -d "" -a alice,bob -L bug,urgent -o json
comments add 7 body --repo Owner/Repo
comments add 7 --description body --repo Owner/Repo
comments a 7 body -r Owner/Repo
comment 7 body -r Owner/Repo
comments 7 body -r Owner/Repo
c 7 body -r Owner/Repo
issues close 7 -r Owner/Repo -o table
issues reopen 7 -r Owner/Repo
issues open 7 -r Owner/Repo -o json
```

Require every comment spelling to create `GiteaRequest_IssueComment`, never a generic comment or inferred pull-request operation.

- [ ] **Step 2: Write boundary and rejection tables**

Generate exact 255-rune titles/list entries and 64-KiB bodies, plus one-rune/one-byte overages. Reject whitespace-free empty title/body, missing repository/title/index/body, both positional and `-d`, comma-empty values, duplicate case-sensitive list values, 26 entries, duplicate long/short flags, `--flag=value`, negative/overflow index, extra positionals, `--comments`, issue-edit flags, and stdin/editor/prompt flags.

Add a no-RPC `Run` test for representative usage errors; assert exit 2, empty stdout, bounded static usage on stderr, and zero dial/provider calls using the existing client test seam.

- [ ] **Step 3: Run focused parser tests and confirm failure**

```bash
go test ./internal/client/gitea -run 'TestParseIssueMutation|TestIssueMutationBoundaries|TestIssueMutationUsageDoesNotDial' -count=1
```

Expected: FAIL because the mutation grammar is not dispatched.

- [ ] **Step 4: Implement focused mutation parsers**

Keep `parse.go` as the command-family dispatcher and add private functions with narrow responsibilities:

```go
func parseIssueCreate(args []string) (command, error)
func parseIssueComment(args []string) (command, error)
func parseIssueState(args []string, reopen bool) (command, error)
func parseUniqueCSV(value string) ([]string, error)
func validateMutationText(value string, allowEmpty bool, maximumBytes, maximumRunes int) error
```

Normalize aliases before duplicate detection so `-t` plus `--title` is rejected. Build repository context only after all local grammar validation succeeds. Mark the returned command as a mutation; do not infer it from command text later.

- [ ] **Step 5: Extend fuzz invariants**

For every successful fuzz parse, call provider `ValidateRequest`, assert one operation branch, canonical repository identity, limits, uniqueness, and no panic. For every failure, assert diagnostics do not contain fuzz input. Seed every documented alias and representative malformed forms.

- [ ] **Step 6: Run parser and fuzz regression tests**

```bash
gofmt -w internal/client/gitea/parse.go internal/client/gitea/issue_mutation_parse_test.go internal/client/gitea/fuzz_test.go
go test -race ./internal/client/gitea -run 'TestParse|TestIssueMutation|FuzzParse' -count=1
```

Expected: PASS with the read grammar unchanged.

- [ ] **Step 7: Commit the mutation parser**

```bash
git add internal/client/gitea/parse.go internal/client/gitea/issue_mutation_parse_test.go internal/client/gitea/fuzz_test.go
git commit -m "feat(tea): parse issue write commands"
```

---

### Task 3: Render validated canonical mutation output

**Files:**
- Create: `internal/client/gitea/issue_mutation_render.go`
- Create: `internal/client/gitea/issue_mutation_render_test.go`
- Modify: `internal/client/gitea/render.go`

**Interfaces:**
- Consumes: Task 1 response branches and Task 2 parsed operation/format.
- Produces: issue mutation keys `index,title,state,url` and comment keys `id,url,body`.
- Produces: complete `[]byte` only after semantic validation, retaining atomic output through existing `writeExact`.

- [ ] **Step 1: Write failing exact-output tests**

For create/close/reopen, require fixed simple labels, one table header/row, and JSON key order:

```text
index: 7
title: created title
state: open
url: https://gitea.example/Owner/Repo/issues/7
```

```json
{"index":7,"title":"created title","state":"open","url":"https://gitea.example/Owner/Repo/issues/7"}
```

For comment, require `id,url,body` in that order for all formats, including multiline body escaping in table/JSON and literal body preservation in simple output.

- [ ] **Step 2: Write response rejection and atomicity tests**

Reject nil response/result/record/meta, empty request ID, wrong branch, non-positive IDs, unspecified/all issue state, non-issue kind, empty title/URL, invalid UTF-8/NUL, invalid or missing timestamps, timestamps out of order, and issue records that are not fully populated according to predecessor normalization. Assert output remains empty for validation failure and when rendered size exceeds 8 MiB.

- [ ] **Step 3: Run renderer tests and confirm failure**

```bash
go test ./internal/client/gitea -run 'TestRenderIssueMutation|TestRenderCommentMutation|TestMutationRenderRejects|TestMutationRenderIsAtomic' -count=1
```

Expected: FAIL because mutation branches are unsupported.

- [ ] **Step 4: Implement a focused mutation renderer**

Define typed private JSON shapes rather than maps:

```go
type issueMutationJSON struct {
	Index int64  `json:"index"`
	Title string `json:"title"`
	State string `json:"state"`
	URL   string `json:"url"`
}
type commentMutationJSON struct {
	ID   int64  `json:"id"`
	URL  string `json:"url"`
	Body string `json:"body"`
}
```

Reuse predecessor record validators where their contract matches; add mutation-specific completeness checks rather than weakening read projection rules. Render into an internal buffer, enforce `maxRenderedBytes`, then return bytes. Add explicit branch dispatch in `render.go`.

- [ ] **Step 5: Run all client renderer tests**

```bash
gofmt -w internal/client/gitea/issue_mutation_render.go internal/client/gitea/issue_mutation_render_test.go internal/client/gitea/render.go
go test -race ./internal/client/gitea -count=1
```

Expected: PASS; list/view/repository output remains byte-for-byte compatible.

- [ ] **Step 6: Commit mutation rendering**

```bash
git add internal/client/gitea/issue_mutation_render.go internal/client/gitea/issue_mutation_render_test.go internal/client/gitea/render.go
git commit -m "feat(tea): render issue write results"
```

---

### Task 4: Resolve labels and create an issue with one SDK write

**Files:**
- Create: `internal/provider/gitea/issue_create.go`
- Create: `internal/provider/gitea/issue_create_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`
- Modify: `internal/provider/gitea/issue_normalize.go`

**Interfaces:**
- Consumes: `sdk.RepositoriesService.ListRepoLabels`, `sdk.IssuesService.CreateIssue`, canonical configured owner/name, and Task 1 create request.
- Produces: `resolveLabelIDs(ctx, owner, repo string, requested []string) ([]int64, error)`.
- Produces: exactly one `sdk.CreateIssueOption{Title, Body, Assignees, Labels}` write after successful optional lookup.

- [ ] **Step 1: Extend the narrow SDK seam and write failing call-shape tests**

Add context-aware methods to `giteaAPI` and its SDK wrapper:

```go
ListRepoLabels(context.Context, string, string, sdk.ListLabelsOptions) ([]*sdk.Label, error)
CreateIssue(context.Context, string, string, sdk.CreateIssueOption) (*sdk.Issue, error)
```

Use a recording fake to assert zero label calls without requested labels and exactly one create call with canonical `Owner/Repo`, title, body presence semantics converted to the SDK body value, copied assignee names, and requested-order label IDs. Assert no follow-up edit call exists in the seam.

- [ ] **Step 2: Write the complete failing label-resolution matrix**

Cover short page, two pages, exactly 1,000 labels plus empty page-21 probe, page-21 overflow, nil label, non-positive/duplicate ID, empty/duplicate case-sensitive name, exact-case match, case mismatch, missing name, stable requested order, cancellation, provider error, and malformed page. Assert sequential page sizes of 50, at most 21 lookup calls, zero create calls on every failure, and sanitized domain errors.

- [ ] **Step 3: Run focused create tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'TestResolveLabelIDs|TestIssueCreate' -count=1
```

Expected: FAIL because label lookup and issue creation are absent.

- [ ] **Step 4: Implement bounded label resolution**

Page from 1 through 20 with `sdk.ListLabelsOptions{ListOptions: sdk.ListOptions{Page: page, PageSize: 50}}`. Stop on a short page. After a full page 20, request page 21 and accept only an empty page. Validate all returned labels before matching requested names; build name-to-ID only after proving IDs and names globally unique. Return a copied ID slice in requested order. Map missing/ambiguous/bound failures to a sanitized failed-precondition domain error before any write.

- [ ] **Step 5: Implement create dispatch and complete response normalization**

Dispatch `IssueCreate` from `RepositoryAdapter.Execute`. Resolve labels first, then invoke `CreateIssue` once. Normalize with the existing `normalizeIssue`, require canonical owner/name and issue kind, and project all issue fields so Task 3 receives a fully validated record. Keep SDK response types private to the provider package.

- [ ] **Step 6: Run provider create tests**

```bash
gofmt -w internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_create.go internal/provider/gitea/issue_create_test.go internal/provider/gitea/issue_normalize.go
go test -race ./internal/provider/gitea -run 'TestResolveLabelIDs|TestIssueCreate|TestRepositoryAdapter|TestSDKAPI' -count=1
```

Expected: PASS; every precondition failure has zero create calls and every accepted create has one.

- [ ] **Step 7: Commit issue creation**

```bash
git add internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_create.go internal/provider/gitea/issue_create_test.go internal/provider/gitea/issue_normalize.go
git commit -m "feat(gitea): create issues with bounded labels"
```

---

### Task 5: Add kind-explicit comments and ensure-state transitions

**Files:**
- Create: `internal/provider/gitea/issue_mutation.go`
- Create: `internal/provider/gitea/issue_mutation_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`

**Interfaces:**
- Consumes: predecessor `GetIssue`, `normalizeIssue`, `normalizeComment`, and Task 1 comment/close/reopen requests.
- Produces: SDK seam methods for `CreateIssueComment` and `EditIssue`.
- Produces: an internal mutation result carrying `transitioned *bool` for server audit metadata without adding it to public protocol records.

- [ ] **Step 1: Write failing issue-kind preflight tests**

For comment, close, and reopen, test nil/malformed/wrong-index/wrong-repository reads and pull-request markers. Require one read, zero writes, the existing corrective issue-kind domain error for pull requests, and sanitized failure otherwise. Include cancelled context before preflight and provider read failure.

- [ ] **Step 2: Write failing comment and state tables**

For comments, require one read followed by one `sdk.CreateIssueCommentOption{Body: body}` call and a fully normalized comment result.

For close/reopen, cover:

```go
[]struct {
	name        string
	initial     sdk.StateType
	target      sdk.StateType
	writeCalls  int
	transitioned bool
}{
	{"close open", sdk.StateOpen, sdk.StateClosed, 1, true},
	{"close closed", sdk.StateClosed, sdk.StateClosed, 0, false},
	{"reopen closed", sdk.StateClosed, sdk.StateOpen, 1, true},
	{"reopen open", sdk.StateOpen, sdk.StateOpen, 0, false},
}
```

Assert the transition uses only `sdk.EditIssueOption{State: &target}`, never title/body/assignee fields. Reject returned wrong index/repository/kind/state and malformed records.

- [ ] **Step 3: Run focused mutation tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'TestIssueKindPreflight|TestIssueComment|TestIssueState' -count=1
```

Expected: FAIL because comment and state write methods are absent.

- [ ] **Step 4: Extend the SDK seam and implement preflight**

Add:

```go
CreateIssueComment(context.Context, string, string, int64, sdk.CreateIssueCommentOption) (*sdk.Comment, error)
EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, error)
```

Create one helper that reads with canonical owner/name, maps 404 consistently, rejects pull requests through `rpcstatus.ErrIssueKind`, normalizes the issue, and verifies the requested index. Do not share a generic comment abstraction with future pull-request work.

- [ ] **Step 5: Implement comment and ensure-state dispatch**

Comment performs preflight then one comment write and `normalizeComment`. Close/reopen performs preflight, returns the projected issue with `transitioned=false` if already satisfied, otherwise invokes one state edit and validates the final normalized record with `transitioned=true`. Keep transition metadata internal; expose it through a narrow trusted context/result interface consumed by Task 7.

- [ ] **Step 6: Run provider mutation tests**

```bash
gofmt -w internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_mutation.go internal/provider/gitea/issue_mutation_test.go
go test -race ./internal/provider/gitea -run 'TestIssueKindPreflight|TestIssueComment|TestIssueState|TestRepositoryAdapter|TestSDKAPI' -count=1
```

Expected: PASS; read/write call order and zero-or-one transition behavior are exact.

- [ ] **Step 7: Commit comments and state transitions**

```bash
git add internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_mutation.go internal/provider/gitea/issue_mutation_test.go
git commit -m "feat(gitea): comment and transition issues"
```

---

### Task 6: Preserve trusted unknown outcomes and disable retries

**Files:**
- Create: `internal/provider/gitea/write_outcome.go`
- Create: `internal/provider/gitea/write_outcome_test.go`
- Modify: `internal/provider/gitea/issue_create.go`
- Modify: `internal/provider/gitea/issue_mutation.go`
- Modify: `internal/rpcstatus/status.go`
- Modify: `internal/rpcstatus/status_test.go`
- Modify: `internal/clientconfig/dial.go`
- Modify: `internal/clientconfig/dial_test.go`
- Modify: `internal/client/gitea/client.go`
- Create: `internal/client/gitea/write_outcome_test.go`

**Interfaces:**
- Consumes: `rpcstatus.ErrWriteOutcomeUnknown`, a trusted in-process domain sentinel that provider code can return without creating an import cycle.
- Produces: gRPC `Unavailable` with exact `ErrorInfo{Reason:"GITEA_WRITE_OUTCOME_UNKNOWN", Domain:"repowolf.dev/gitea"}`.
- Produces: `rpcstatus.IsGiteaWriteOutcomeUnknown(error) bool` for exact tuple recognition.
- Produces: client diagnostic `tea: write outcome unknown; inspect repository state before retrying`.

- [ ] **Step 1: Write the failing provider failure-injection matrix**

For create, comment, and state update, inject cancellation/transport/error-response/body-read/malformed-success failures immediately before invocation, during invocation, and after a returned value. Assert pre-invocation failures are known and have zero calls; once the fake write method is entered, every non-validated result satisfies `errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown)` and call count is exactly one. Assert error strings omit all injected sensitive markers.

- [ ] **Step 2: Implement the private attempt boundary**

Define `rpcstatus.ErrWriteOutcomeUnknown` beside the existing trusted domain sentinels. Use an unexported provider state object whose only write wrapper marks attempted immediately before invoking the SDK method. It returns that sentinel for any call error or validation error after that point. Do not inspect SDK error types/text to decide uncertainty, and do not wrap provider text into the returned error.

Apply it to create issue, create comment, and state edit only; label lookup and issue preflight remain known failures.

- [ ] **Step 3: Write failing status-detail tests**

Require `rpcstatus.Error(rpcstatus.ErrWriteOutcomeUnknown)` to have exact code, message, reason, and domain. Pass it through `rpcstatus.Error` twice and require the tuple survives. Feed arbitrary provider-created statuses with the same or different details and require canonicalization unless the trusted in-process sentinel is present. Test exact recognition rejects code/reason/domain mismatches and extra untrusted detail variants.

- [ ] **Step 4: Implement trusted status mapping**

Add the stable constants and map only `errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown)` to the detailed status. Keep arbitrary incoming gRPC errors canonicalized. Implement the client recognizer against the exact tuple; it must not use message substring matching.

- [ ] **Step 5: Write failing client uncertainty tests**

For a parsed mutation, assert the unknown diagnostic for the exact tuple and for `Unavailable`, `Canceled`, `DeadlineExceeded`, `Internal`, and response-limit failures after `Execute` starts. Assert known permission/precondition failures retain the generic sanitized operation diagnostic, issue-kind retains its corrective diagnostic, reads retain predecessor behavior, stdout is empty, and stderr never includes injected request/provider text.

- [ ] **Step 6: Make execution mutation-aware and disable configured retries**

Track whether a mutation RPC invocation has begun in `executeCommand`/`Run`. Classify the listed post-start delivery failures conservatively without affecting parse/config/dial failures. Add `grpc.WithDisableRetry()` to `clientconfig.Dial` and retain message limits and TLS credentials.

Use an in-process counting gRPC service in `dial_test.go` to assert one handler invocation for a mutation-shaped request that returns retryable `Unavailable`; allow transparent retry only where gRPC proves the remote handler did not process the request.

- [ ] **Step 7: Run uncertainty and no-retry tests**

```bash
gofmt -w internal/provider/gitea/write_outcome.go internal/provider/gitea/write_outcome_test.go internal/provider/gitea/issue_create.go internal/provider/gitea/issue_mutation.go internal/rpcstatus/status.go internal/rpcstatus/status_test.go internal/clientconfig/dial.go internal/clientconfig/dial_test.go internal/client/gitea/client.go internal/client/gitea/write_outcome_test.go
go test -race ./internal/provider/gitea ./internal/rpcstatus ./internal/clientconfig ./internal/client/gitea -run 'Test.*(WriteOutcome|Unknown|Retry|IssueCreate|IssueComment|IssueState)' -count=1
```

Expected: PASS; every provider write and processed service request is single-attempt.

- [ ] **Step 8: Commit uncertainty handling**

```bash
git add internal/provider/gitea/write_outcome.go internal/provider/gitea/write_outcome_test.go internal/provider/gitea/issue_create.go internal/provider/gitea/issue_mutation.go internal/rpcstatus/status.go internal/rpcstatus/status_test.go internal/clientconfig/dial.go internal/clientconfig/dial_test.go internal/client/gitea/client.go internal/client/gitea/write_outcome_test.go
git commit -m "feat(gitea): report uncertain write outcomes"
```

---

### Task 7: Enforce write authorization and terminal audit truth

**Files:**
- Modify: `internal/audit/event.go`
- Modify: `internal/audit/writer_test.go`
- Modify: `internal/server/audit.go`
- Modify: `internal/server/gitea.go`
- Modify: `internal/server/gitea_test.go`
- Create: `internal/server/gitea_write_test.go`
- Modify: `internal/server/gitea_response_limit_test.go`
- Modify: `internal/app/gitea_executor_test.go`

**Interfaces:**
- Consumes: Task 1 capability/operation names, Task 5 transition facts, and Task 6 trusted unknown sentinel.
- Produces: `audit.OutcomeUnknown` and optional `Event.Transitioned *bool`.
- Produces: exactly one accepted and one attempted terminal event after accepted audit succeeds.

- [ ] **Step 1: Write failing authorization and routing tests**

For each mutation, prove malformed requests fail before policy/provider work; `issues:read` without `issues:write` is denied; unknown repository, unauthorized repository, missing capability, and wrong provider kind remain client-indistinguishable; accepted requests route only through the adapter selected by trusted resolved provider ID. Assert request fields cannot select provider ID, authority, credential, or raw path.

- [ ] **Step 2: Write failing audit outcome tests**

Require canonical accepted operation names. Add terminal cases for completed create/comment, close/reopen no-op and transition, known pre-write failure, trusted unknown after write dispatch, denial, and cancellation before write. Assert:

```go
OutcomeUnknown Outcome = "unknown"
Transitioned *bool `json:"transitioned,omitempty"`
```

Only close/reopen include `transitioned`; no-op is false and validated update is true. Unknown write is `unknown`, known no-write cancellation is `cancelled`, and other pre-write failure is `failed`. Require stable canonical reasons and exactly one terminal attempt after accepted audit.

- [ ] **Step 3: Write leak and response-bound tests**

Inject issue bodies, comment bodies, labels, assignees, indices, provider errors/bodies, token, and authority markers. Assert none appears in status, diagnostics, or serialized audit. Force provider response, Protobuf response, and final-response delivery failures: provider uncertainty yields audit `unknown`; a fully validated provider completion followed by client delivery loss keeps server audit `completed` while the client reports unknown.

- [ ] **Step 4: Run focused server tests and confirm failure**

```bash
go test ./internal/audit ./internal/server ./internal/app -run 'Test.*(GiteaWrite|Unknown|Transitioned|IssueMutation|ResponseLimit)' -count=1
```

Expected: FAIL because `unknown` and transition metadata are not represented or classified.

- [ ] **Step 5: Extend the bounded audit schema and lifecycle**

Add `OutcomeUnknown` to the closed constants and `Transitioned *bool` to `audit.Event`. In server context metadata, store only a trusted unknown marker and optional transition boolean. Classify unknown by sentinel identity before generic status-code classification; do not classify from text or arbitrary client detail.

Mark provider completion before final gRPC serialization/delivery. Preserve predecessor accepted-audit failure behavior and terminal sink failure behavior.

- [ ] **Step 6: Run server, audit, and routing regressions**

```bash
gofmt -w internal/audit/event.go internal/audit/writer_test.go internal/server/audit.go internal/server/gitea.go internal/server/gitea_test.go internal/server/gitea_write_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go
go test -race ./internal/audit ./internal/server ./internal/app ./internal/provider/gitea -run 'Test.*(Gitea|Issue|Audit|Unknown|Transitioned|Provider)' -count=1
```

Expected: PASS; read operations and all other providers retain existing terminal outcomes.

- [ ] **Step 7: Commit authorization and audit semantics**

```bash
git add internal/audit/event.go internal/audit/writer_test.go internal/server/audit.go internal/server/gitea.go internal/server/gitea_test.go internal/server/gitea_write_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go
git commit -m "feat(gitea): audit terminal issue writes"
```

---

### Task 8: Prove basic writes against pinned Gitea and run repository gates

**Files:**
- Create: `integration/gitea_issue_writes_test.go`
- Modify: `integration/gitea_fixture_test.go`
- Modify: `integration/testdata/gitea-policy.yaml`

**Interfaces:**
- Consumes: packaged service/client binaries and digest-pinned Gitea `1.27.2` fixture.
- Produces: end-to-end proof of create, typed comment, close, no-op close, reopen, independent reads, audit truth, sanitization, and one-attempt writes.

- [ ] **Step 1: Grant the fixture write capability and add independent helpers**

Add `issues:write` beside `issues:read` for the authorized fixture repository. Add narrowly scoped HTTP helpers to read issue/comment state and count matching method/path requests in Gitea logs. This static YAML change gets direct integration verification, not a test that merely asserts YAML text.

- [ ] **Step 2: Write the end-to-end demonstration**

Create a label in setup, then execute the packaged client through TLS:

```text
tea issues create -r CanonicalOwner/CanonicalRepo -t "write integration" -d "private write body" -a CanonicalOwner -L bug -o json
tea comments add INDEX "private comment body" -r CanonicalOwner/CanonicalRepo -o json
tea issues close INDEX -r CanonicalOwner/CanonicalRepo -o json
tea issues close INDEX -r CanonicalOwner/CanonicalRepo -o json
tea issues reopen INDEX -r CanonicalOwner/CanonicalRepo -o json
tea issues INDEX -r CanonicalOwner/CanonicalRepo --comments -o json
```

Assert canonical mutation keys/types, independent API state/content, one create POST, one comment POST, one close PATCH, zero PATCH for the second close, and one reopen PATCH. Assert accepted/completed audit pairs and `transitioned` values false/true, with no bodies, labels, assignees, token, indices, or provider errors in audit/diagnostics.

- [ ] **Step 3: Add an ambiguous-response one-attempt integration case**

Use the fixture failure seam to let one write reach its handler and then lose/corrupt the response. Require exit 1, empty stdout, exact unknown diagnostic, one provider write, no automatic retry, and terminal audit `unknown` when provider completion cannot be proved. Add the post-completion/client-response-loss variant where server audit is `completed` but client output remains conservatively unknown.

- [ ] **Step 4: Run the tagged pinned-Gitea tests**

```bash
go test -race -tags gitea_integration ./integration -run 'TestRestrictedTea(ReadOperations|IssueWrites)AgainstGitea|TestGiteaWriteOutcomeUnknown' -count=1
```

Expected: PASS against the digest-pinned Gitea 1.27.2 image, with exact request counts.

- [ ] **Step 5: Run generated, static, and full Go verification**

```bash
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
git diff --check
```

Expected: every command exits zero. These commands directly verify generated/static structure rather than adding low-value tests for file text.

- [ ] **Step 6: Run packaging and release regression gates**

```bash
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
```

Expected: packages, OCI/release checks, GitHub, Git, Gitea reads, security, and integration-independent gates remain green.

- [ ] **Step 7: Confirm issue scope and commit the integration proof**

```bash
git status --short
git diff --name-only origin/agent/issue-10-gitea-roadmap-6-11-issue-read-operations...HEAD
git diff --check
git add integration/gitea_issue_writes_test.go integration/gitea_fixture_test.go integration/testdata/gitea-policy.yaml
git commit -m "test(gitea): prove basic issue writes end to end"
```

Expected: only issue-write protocol, client, provider, status, audit/server, and integration files are present; no issue editing, generic mutation framework, retry loop, or credential-bearing artifact appears.

- [ ] **Step 8: Verify the completed implementation branch**

```bash
git status --short
git log --oneline origin/agent/issue-10-gitea-roadmap-6-11-issue-read-operations..HEAD
```

Expected: clean worktree and eight scoped Conventional Commit task commits after the predecessor baseline.
