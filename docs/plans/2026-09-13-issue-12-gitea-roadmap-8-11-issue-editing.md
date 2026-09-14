# Gitea Issue Editing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authorized restricted `tea` client edit one Gitea issue's title, body, assignees, and labels with deterministic bounded planning, one reconciliation pass, and truthful partial/unknown outcomes.

**Architecture:** Add one typed `issue_edit` protocol branch and normalize CLI precedence before RPC. Keep family predicates and delta construction in a pure focused planner, while a provider executor performs bounded catalogs, sequential narrow SDK calls, fresh final reads, and at most one corrective plan; trusted status and audit layers distinguish complete, partial, and uncertain effects.

**Tech Stack:** Go 1.26, Protocol Buffers/gRPC, `gitea.dev/sdk` pinned by `go.mod`, the existing restricted `tea` client and provider HTTP transport, Docker-backed Gitea 1.27.2 integration tests, Nix.

**Spec:** `docs/specs/2026-09-13-issue-12-gitea-roadmap-8-11-issue-editing-design.md`

## Global Constraints

- Start from issue #11 commit `89d007a`; preserve repository view, issue reads, create/comment/close/reopen, GitHub, and Git behavior.
- No `AGENTS.md` exists in this worktree or its repository parents. Use `.github/workflows/ci.yml`, `scripts/check-generated.sh`, and `scripts/check-release.sh` as validation authority.
- `issue_edit` requires exactly `issues:write`; validation precedes policy resolution and repository authorization precedes every provider read/write.
- Keep the existing 64-argument, 64-KiB argv, 8-MiB Protobuf/provider-response, 8-MiB rendered-output, two-minute operation deadline, concurrency, TLS, redirect, token-authority, cancellation, and gRPC no-retry controls.
- Accept only `tea issues edit|e INDEX --repo OWNER/REPO MUTATION...`, optional `--output|-o simple|table|json`, and the spec's title, description, assignee, and label flags; reject stdin, prompts, editor fallback, `--flag=value`, extra positionals, unsupported flags, and pull-request editing.
- Title is non-empty and at most 255 Unicode characters. Description has presence, may be empty, and is at most 64 KiB. Lists have at most 25 unique case-sensitive non-empty names of at most 255 Unicode characters, except `--set-assignees ""` means exact empty replacement. Reject invalid UTF-8 and NUL.
- Validate every supplied flag before E1 normalization. Effective precedence is assignee `set > add > remove` and label `add > remove`; lower-precedence values have no provider effect.
- Preflight is sequential: one normalized issue read, at most one assignable-user collection read, and only when a label write may occur at most 20 pages of 50 labels plus the page-21 completion probe. A preflight failure makes zero writes.
- Execute each pass in fixed order: one text patch, assignee removal, assignee addition, label addition, then individual label removals sorted by case-sensitive name. Maximum normal work is three issue reads and two passes, each bounded to 1 text, 2 assignee, 1 label-add, and 25 label-delete writes.
- Success requires a normalized final snapshot satisfying every effective family predicate. A no-op returns the validated preflight snapshot with zero writes. Permit one freshly computed reconciliation plan and no third plan.
- Never blindly retry an uncertain provider invocation. Make one issue read when context permits; return success only if it proves all predicates, otherwise return trusted unknown. Confirmed but incomplete effects return trusted partial.
- Diagnostics and audit records exclude issue text, names, IDs, indices, catalog/provider data, authorities, and credentials. Output stays the canonical `index,title,state,url` mutation shape.
- Generate Protobuf code only with `scripts/generate.sh`; do not hand-edit `gen/`.
- Apply the Testing Value Gate: the planned descriptor, parser/fuzz, pure planner, catalog, provider failure/concurrency, server/audit, and pinned integration tests prove public behavior, bounded calls, security, or risky reusable logic. Do not add tests that merely assert workflow YAML, static policy text, dependency versions, generated text, or docs; verify those with lint, generation freshness, integration execution, and repository gates.

## File Structure

### New files

- `internal/client/gitea/issue_edit_parse_test.go`: edit aliases, all flags, E1 precedence, presence/bounds, rejection, and no-dial behavior.
- `internal/provider/gitea/issue_edit_plan.go`: pure intent predicates, stable family deltas, and fixed-order plan representation.
- `internal/provider/gitea/issue_edit_plan_test.go`: family predicate, no-op, exact/add/remove, ordering, and recomputation tables.
- `internal/provider/gitea/issue_edit_catalog.go`: bounded assignable-user and repository-label collection/validation.
- `internal/provider/gitea/issue_edit_catalog_test.go`: collection need, validation, pagination, cancellation, bounds, and zero-write tests.
- `internal/provider/gitea/issue_edit.go`: preflight, sequential execution, final reads, one reconciliation pass, and uncertain-write recovery.
- `internal/provider/gitea/issue_edit_test.go`: exact SDK call shapes, concurrency, maxima, response validation, and failure matrices.
- `internal/client/gitea/issue_edit_outcome_test.go`: exact partial diagnostic and retained unknown behavior.
- `internal/server/gitea_edit_test.go`: authorization, anti-enumeration, routing, response bounds, and terminal audit truth.
- `integration/gitea_issue_edit_test.go`: packaged client-to-Gitea edit, reconciliation, partial, and unknown demonstration.

### Modified files

- `proto/repowolf/v1/gitea.proto`: additive edit request/result branch, optional text, and typed action wrappers.
- `gen/repowolf/v1/gitea.pb.go`, `gen/repowolf/v1/gitea_grpc.pb.go`: generated output only.
- `internal/protocol/gitea_contract_test.go`: descriptor tags, presence, oneofs, empty replacement, and closed-surface assertions.
- `internal/provider/gitea/operation.go`: edit validation, `issues:write`, and `gitea.issue_edit`.
- `internal/provider/gitea/issue_mutation_operation_test.go`: malformed edit matrices and operation/capability checks.
- `internal/client/gitea/parse.go`: dispatch and normalize the closed edit grammar.
- `internal/client/gitea/fuzz_test.go`: accepted edit invariants and bounded non-echoing failures.
- `internal/client/gitea/render.go`: dispatch edit success through existing canonical issue mutation rendering.
- `internal/client/gitea/issue_mutation_render_test.go`: edit response branch, exact output, and malformed/atomic rejection.
- `internal/provider/gitea/adapter.go`: narrow SDK methods and edit dispatch.
- `internal/provider/gitea/adapter_test.go`: SDK wrapper/dispatch coverage.
- `internal/provider/gitea/issue_normalize.go`: reject invalid/duplicate assignee and label identities/names in snapshots.
- `internal/provider/gitea/issue_normalize_test.go`: nested ID/name validation and duplicate regression tests.
- `internal/provider/gitea/write_outcome.go`: reuse the write-attempt boundary from issue #11.
- `internal/rpcstatus/status.go`, `internal/rpcstatus/status_test.go`: trusted exact `GITEA_EDIT_PARTIAL` status.
- `internal/client/gitea/client.go`: edit-specific partial diagnostic before generic mutation handling.
- `internal/audit/event.go`, `internal/audit/writer_test.go`: closed `partial` outcome.
- `internal/server/audit.go`: trusted partial terminal classification.
- `internal/server/gitea.go`: preserve provider completion only after satisfying final edit state.
- `internal/server/gitea_write_test.go`, `internal/server/gitea_response_limit_test.go`: retain predecessor write and response-limit behavior.
- `integration/gitea_fixture_test.go`: controlled concurrent actor and step-failure hooks.
- `integration/testdata/gitea-policy.yaml`: no semantic change expected; existing `issues:write` grant is used and verified through integration.

---

### Task 1: Add the typed issue-edit protocol and operation contract

**Files:**
- Modify: `proto/repowolf/v1/gitea.proto`
- Modify (generated): `gen/repowolf/v1/gitea.pb.go`
- Modify (generated): `gen/repowolf/v1/gitea_grpc.pb.go`
- Modify: `internal/protocol/gitea_contract_test.go`
- Modify: `internal/provider/gitea/operation.go`
- Modify: `internal/provider/gitea/issue_mutation_operation_test.go`

**Interfaces:**
- Consumes: issue #11 request/result tags 10-16, `GiteaIssueRecord`, and existing mutation text/list validators.
- Produces: `GiteaStringList`, `GiteaIssueEditRequest`, `GiteaIssueEditResult`, request/result branch tag 17, `Capability == config.IssuesWrite`, and operation `gitea.issue_edit`.
- Produces: optional title/description presence plus distinct assignee-action and label-action oneofs.

- [ ] **Step 1: Write failing descriptor tests**

Require all predecessor branch tags to remain unchanged and add `issue_edit = 17` on request and response. Assert `title` and `description` are optional strings, the request's `assignee_action` oneof has set/add/remove message fields, its `label_action` oneof has add/remove message fields, and the list wrapper is repeated string rather than a map. Round-trip an empty `set_assignees` wrapper and prove it stays present while an absent action stays absent. Extend the forbidden-field scan to reject maps, raw JSON, provider IDs, endpoint selectors, authorities, retry/concurrency controls, and SDK-shaped fields.

- [ ] **Step 2: Write failing operation-validation tables**

Add a valid row shaped as:

```go
request := &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueEdit{
	IssueEdit: &repowolfv1.GiteaIssueEditRequest{
		Index: 7,
		Title: proto.String("new title"),
		AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{
			SetAssignees: &repowolfv1.GiteaStringList{Values: []string{}},
		},
	},
}}
```

Assert `issues:write` and `gitea.issue_edit`. Reject nil edit/list wrappers, non-positive index, no mutation family, present empty title, title over 255 runes, description over 64 KiB, invalid UTF-8/NUL, empty add/remove lists, empty non-set members, duplicate names, 26 names, and malformed oneof values. Keep set-empty valid.

- [ ] **Step 3: Run the focused tests and confirm failure**

```bash
go test ./internal/protocol ./internal/provider/gitea -run 'TestGitea.*(Protocol|Edit|Operation|Validate)' -count=1
```

Expected: FAIL because the edit messages and branch do not exist.

- [ ] **Step 4: Add the additive protocol and strict validation**

Add the following shapes without renumbering existing fields:

```proto
message GiteaStringList { repeated string values = 1; }
message GiteaIssueEditRequest {
  int64 index = 1;
  optional string title = 2;
  optional string description = 3;
  oneof assignee_action {
    GiteaStringList set_assignees = 4;
    GiteaStringList add_assignees = 5;
    GiteaStringList remove_assignees = 6;
  }
  oneof label_action {
    GiteaStringList add_labels = 7;
    GiteaStringList remove_labels = 8;
  }
}
message GiteaIssueEditResult { GiteaIssueRecord issue = 1; }
```

Add `issue_edit = 17` to both top-level oneofs, run `scripts/generate.sh`, and extend `ValidateRequest`, `Capability`, and `OperationName` with explicit closed switches. Reuse rune/byte/name checks but add a validator parameter that permits only the set-assignees wrapper to be empty.

- [ ] **Step 5: Validate and commit the protocol contract**

```bash
gofmt -w internal/protocol/gitea_contract_test.go internal/provider/gitea/operation.go internal/provider/gitea/issue_mutation_operation_test.go
go tool buf lint
scripts/check-generated.sh
go test ./internal/protocol ./internal/provider/gitea -run 'TestGitea.*(Protocol|Edit|Operation|Validate)' -count=1
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go internal/protocol/gitea_contract_test.go internal/provider/gitea/operation.go internal/provider/gitea/issue_mutation_operation_test.go
git commit -m "feat(gitea): add typed issue edit protocol"
```

Expected: all commands pass and generated output is reproducible.

---

### Task 2: Parse and render the closed issue-edit command

**Files:**
- Modify: `internal/client/gitea/parse.go`
- Create: `internal/client/gitea/issue_edit_parse_test.go`
- Modify: `internal/client/gitea/fuzz_test.go`
- Modify: `internal/client/gitea/render.go`
- Modify: `internal/client/gitea/issue_mutation_render_test.go`

**Interfaces:**
- Consumes: Task 1's typed request/result and existing `parsePositive`, repository/output parsing, mutation text checks, `renderIssueMutation`, and `command.mutation`.
- Produces: exact `issues edit|e` grammar with all supplied values validated before E1 normalization.
- Produces: an edit success routed only from `GiteaResponse_IssueEdit` to canonical `index,title,state,url` output.

- [ ] **Step 1: Write failing accepted-command and precedence tables**

Cover both aliases, long and short aliases, arbitrary legal flag ordering, default/simple/table/JSON output, explicit empty description, and empty set-assignees. Assert exact request presence and normalized oneofs for:

```text
issues edit 7 -r Owner/Repo --title title --description ""
issues e 7 --repo Owner/Repo --set-assignees "" -o json
issues edit 7 -r Owner/Repo --set-assignees alice --add-assignees bob --remove-assignees carol
issues edit 7 -r Owner/Repo --add-labels bug --remove-labels stale
issues edit 7 -r Owner/Repo -a alice -L bug
```

For precedence rows, require all lower-precedence supplied values to be locally validated but only set-assignees and add-labels to enter the request.

- [ ] **Step 2: Write failing boundary, rejection, and no-dial tables**

Accept exact 255-rune names/title and 64-KiB description. Reject no mutation, zero/negative/overflow/multiple indices, extra positionals, missing/invalid repo, duplicate long/short aliases, `--flag=value`, over-limit values, invalid UTF-8/NUL, malformed CSV, duplicate case-sensitive names, 26 names, empty add/remove lists, empty title, unsupported create/state/milestone/deadline/referenced-version/PR flags, stdin/editor/prompt flags, and more than 64 args/64-KiB argv. Through `Run`, assert representative usage errors exit 2 with empty stdout, bounded static stderr, and zero dial/RPC calls.

- [ ] **Step 3: Write failing renderer tests**

Provide a full canonical issue record under `GiteaResponse_IssueEdit`; require exact existing simple/table/JSON bytes and key order. Reject nil/wrong branches, malformed records, empty request ID, invalid format, and over-limit output atomically with zero bytes written.

- [ ] **Step 4: Run client tests and confirm failure**

```bash
go test ./internal/client/gitea -run 'Test(ParseIssueEdit|IssueEdit|RenderIssueMutation)' -count=1
```

Expected: FAIL because edit parsing and result dispatch are absent.

- [ ] **Step 5: Implement focused parser dispatch and E1 normalization**

Dispatch `edit|e` before issue-index view parsing and keep mutation parsing out of the already-large generic branch where practical:

```go
func parseIssueEdit(args []string) (command, error)
func parseEditCSV(value string, allowEmpty bool) ([]string, error)
```

Normalize aliases before duplicate detection. Store each supplied family candidate until all flags pass validation, then choose `set > add > remove` and `add-labels > remove-labels`. Build one typed request and set `mutation: true`; never send discarded lower-precedence values.

In `render.go`, add an explicit edit branch that requires the response's edit result and calls `renderIssueMutation`. Do not add changed-field output or weaken mutation-record validation.

- [ ] **Step 6: Extend fuzz invariants and run client regressions**

Seed every edit flag/alias and malformed representative. On successful parsing assert `provider/gitea.ValidateRequest`, one positive index, at least one effective family, at most one action per collection family, count/text bounds, and no ambiguity. On failures assert no panic and no attacker input in diagnostics.

```bash
gofmt -w internal/client/gitea/parse.go internal/client/gitea/issue_edit_parse_test.go internal/client/gitea/fuzz_test.go internal/client/gitea/render.go internal/client/gitea/issue_mutation_render_test.go
go test -race ./internal/client/gitea -count=1
git add internal/client/gitea/parse.go internal/client/gitea/issue_edit_parse_test.go internal/client/gitea/fuzz_test.go internal/client/gitea/render.go internal/client/gitea/issue_mutation_render_test.go
git commit -m "feat(tea): parse and render issue edits"
```

Expected: all client tests pass and predecessor command output is unchanged.

---

### Task 3: Build pure family predicates and deterministic edit plans

**Files:**
- Create: `internal/provider/gitea/issue_edit_plan.go`
- Create: `internal/provider/gitea/issue_edit_plan_test.go`
- Modify: `internal/provider/gitea/issue_normalize.go`
- Modify: `internal/provider/gitea/issue_normalize_test.go`

**Interfaces:**
- Consumes: Task 1's normalized oneof request, existing `normalizedIssue`, and validated name-to-ID catalogs.
- Produces: private `editIntent`, `editCatalogs`, `issueEditPlan`, `normalizeEditIntent`, `editSatisfied`, and `planIssueEdit`.
- Produces: sorted, de-duplicated narrow steps; planner performs no I/O and exposes no public/provider-neutral DTO.

- [ ] **Step 1: Write failing snapshot-normalization regressions**

Add provider issue fixtures with nil nested values, non-positive assignee/label IDs, duplicate assignee/label IDs, duplicate assignee usernames, and duplicate label names, including repeated names with different IDs. Require normalization failure rather than silent set coalescing. Retain provider-owned ordering for valid snapshots; IDs are validation evidence and label-catalog keys, not public output.

- [ ] **Step 2: Write failing postcondition tables**

For each effective family prove:

```go
// text equality; set exact equality; add subset; remove disjointness
editSatisfied(intent, snapshot)
```

Cover empty exact assignee replacement, order-independent set equality, exact case matching, unrelated members under add/remove, multiple simultaneous families, and one false family making the aggregate false. Invalid duplicate snapshots must never reach this function.

- [ ] **Step 3: Write failing deterministic-plan tables**

Use concrete intents/snapshots/catalogs to require: no-op empty plan; one combined text patch; preservation of current body/title when its field is absent; set removals as current-minus-target and additions as target-minus-current; add/remove only necessary members; stable case-sensitive sorting; label add IDs resolved by name; label removals sorted by name then represented by stable positive IDs; and fixed family order.

Represent the plan narrowly:

```go
type issueEditPlan struct {
	text              *issueTextEdit
	removeAssignees   []string
	addAssignees      []string
	addLabelIDs       []int64
	removeLabelIDs    []int64
}
type issueTextEdit struct { title string; body *string }
```

Require recomputation from a changed snapshot to contain only newly unmet predicates. For text, carry the current title whenever only body changes because the SDK title field lacks presence; leave `Body` nil when body is unrequested because that SDK field is a pointer, and use a non-nil pointer (including `""`) only when body is requested.

- [ ] **Step 4: Run planner tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'Test(NormalizeIssueRejectsDuplicateMembers|EditSatisfied|PlanIssueEdit)' -count=1
```

Expected: FAIL because duplicate detection and the planner are absent.

- [ ] **Step 5: Implement the pure planner in a focused module**

Normalize the Protobuf branch once into explicit private modes (`none`, `set`, `add`, `remove`) and copied name slices. Build temporary sets only after validated snapshots/catalogs. Sort copied deltas with `sort.Strings`; never mutate request or snapshot slices. Keep predicate evaluation shared by initial planning, reconciliation, uncertain recovery, and completion.

Extend `normalizeIssue` to require positive unique assignee/label IDs and unique case-sensitive assignee/label names while retaining provider order in `normalizedIssue` and projected records. Keep `issue_edit_plan.go` focused on domain rules and below split pressure; do not put SDK calls in it.

- [ ] **Step 6: Run and commit planner behavior**

```bash
gofmt -w internal/provider/gitea/issue_edit_plan.go internal/provider/gitea/issue_edit_plan_test.go internal/provider/gitea/issue_normalize.go internal/provider/gitea/issue_normalize_test.go
go test -race ./internal/provider/gitea -run 'Test(NormalizeIssue|EditSatisfied|PlanIssueEdit)' -count=1
git add internal/provider/gitea/issue_edit_plan.go internal/provider/gitea/issue_edit_plan_test.go internal/provider/gitea/issue_normalize.go internal/provider/gitea/issue_normalize_test.go
git commit -m "feat(gitea): plan deterministic issue edits"
```

Expected: all planner/normalization tables pass without provider calls.

---

### Task 4: Collect only required assignee and label catalogs

**Files:**
- Create: `internal/provider/gitea/issue_edit_catalog.go`
- Create: `internal/provider/gitea/issue_edit_catalog_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`
- Modify: provider test fakes implementing `giteaAPI` as required by compilation.

**Interfaces:**
- Consumes: Task 3 intent/snapshot, `RepositoriesService.GetAssignees`, existing bounded `ListRepoLabels`, canonical owner/name, and the 8-MiB provider transport budget.
- Produces: narrow provider methods `GetAssignees(context.Context,string,string) ([]*sdk.User,error)`, `AddIssueAssignees`, `DeleteIssueAssignees`, `AddIssueLabels`, and `DeleteIssueLabel`.
- Produces: `collectEditCatalogs(ctx, owner, repo string, intent editIntent, issue *normalizedIssue) (editCatalogs, error)` with validated case-sensitive maps.

- [ ] **Step 1: Extend the SDK seam and write failing wrapper tests**

Add exact SDK-facing signatures:

```go
GetAssignees(context.Context, string, string) ([]*sdk.User, error)
AddIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error)
DeleteIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error)
AddIssueLabels(context.Context, string, string, int64, sdk.IssueLabelsOption) ([]*sdk.Label, error)
DeleteIssueLabel(context.Context, string, string, int64, int64) error
```

Wire them respectively to `client.Repositories.GetAssignees` and `client.Issues` methods, discarding only SDK response metadata. Assert contexts and canonical arguments pass through exactly.

- [ ] **Step 2: Write failing collection-need and assignee tests**

Require zero assignable-user reads for text-only, all remove-assignee actions, and empty set replacement. Require exactly one unpaginated read for non-empty set/add, copied positive IDs, non-empty valid unique case-sensitive usernames and IDs, exact-case resolution of every requested target, and rejection of nil users, missing names, duplicates, conflicting IDs, invalid strings, cancellation, provider error, and an aggregate collection exceeding the existing 8-MiB response budget. Every failure occurs before writes.

- [ ] **Step 3: Write failing label catalog tables**

Require zero label reads for no label action, add requests whose targets are already present, and remove requests whose targets are already absent from the issue. When missing add targets or currently present removal targets require IDs, reuse pages of 50 through at most 20 full pages plus an empty page-21 probe. Cover short pages, exactly 1,000 plus probe, overflow, >50 entries/page, nil labels, non-positive/duplicate IDs, empty/duplicate names, case mismatch, missing add target, missing currently present removal target, cancellation, provider error, and deterministic name-to-ID output. Assert at most 21 calls and zero writes on all errors.

- [ ] **Step 4: Run focused catalog tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'Test(SDK.*Edit|CollectEditCatalogs|EditAssigneeCatalog|EditLabelCatalog)' -count=1
```

Expected: FAIL because the SDK edit seam and catalogs are absent.

- [ ] **Step 5: Implement bounded catalogs without broadening the create contract**

Place edit-specific collection logic in `issue_edit_catalog.go`. Reuse a focused internal label-page collector with `resolveLabelIDs` only if the refactor preserves issue-create ordering/errors/tests; otherwise keep the edit catalog separate to avoid changing create behavior. Validate complete provider collections before resolving targets. Return only sanitized trusted sentinels (`ErrProviderFailure`, `ErrFailedPrecondition`, `ErrResourceExhausted`, or context error), never names/provider text.

- [ ] **Step 6: Run provider regressions and commit catalog support**

```bash
gofmt -w internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_edit_catalog.go internal/provider/gitea/issue_edit_catalog_test.go
go test -race ./internal/provider/gitea -run 'Test(SDK|IssueCreate|ResolveLabelIDs|CollectEditCatalogs|Edit.*Catalog)' -count=1
git add internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/issue_edit_catalog.go internal/provider/gitea/issue_edit_catalog_test.go
git commit -m "feat(gitea): collect bounded edit catalogs"
```

Expected: all catalog and predecessor create tests pass; unnecessary catalogs make zero calls.

---

### Task 5: Execute bounded plans, final reads, and one reconciliation pass

**Files:**
- Create: `internal/provider/gitea/issue_edit.go`
- Create: `internal/provider/gitea/issue_edit_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`
- Modify: `internal/provider/gitea/write_outcome.go` only if a narrow generic write helper is needed.

**Interfaces:**
- Consumes: Task 3 planner/predicates, Task 4 catalogs/SDK seam, existing `issuePreflight`, `invokeWrite`, normalization/projection, and provider response bounds.
- Produces: `RepositoryAdapter.issueEdit` and edit dispatch returning exactly one `GiteaIssueEditResult` after a satisfying snapshot.
- Produces: private execution facts (`attempted`, `confirmed`) sufficient for Task 6 to classify unknown versus partial without exposing changed families.

- [ ] **Step 1: Write failing exact call-order and no-op tests**

For an already-satisfied multi-family request require one issue read, only catalogs logically required by preflight validation, zero writes, no final reread, and the preflight snapshot as result. For an unmet request assert this exact sequence:

```text
GetIssue -> optional GetAssignees -> optional ListRepoLabels pages
-> EditIssue(text)
-> DeleteIssueAssignees(batch) -> AddIssueAssignees(batch)
-> AddIssueLabels(batch) -> DeleteIssueLabel(each)
-> GetIssue(final)
```

Assert `sdk.EditIssueOption` protects current unrequested text, assignee calls use sorted names, label addition uses sorted-name IDs, removals use sequential sorted-name IDs, every successful returned issue/label collection is fully validated before the next step, and the response projects the satisfying final read rather than an intermediate response.

- [ ] **Step 2: Write failing concurrent-change and reconciliation tests**

Inject changes after initial writes and before the first final read. Prove add/remove operations preserve unrelated concurrent members, exact set removes unrelated concurrent assignees, only unmet families are included in the corrective plan, one correction can succeed, and stable ordering is recomputed from the fresh snapshot. Persistent contention must stop after the reconciliation final read: exactly three issue reads and no third plan.

Add a maximum-call fixture with 25 label removals on both passes and assert the spec's exact ceilings: three issue reads, one assignee collection, 21 label pages, two text patches, four assignee batch calls, two label-add calls, and 50 sequential label-delete calls.

- [ ] **Step 3: Write the complete failure and uncertainty matrix**

Inject failures before invocation, during invocation, malformed success responses, between confirmed steps, on each final read, and during/after reconciliation. Assert:

- preflight/catalog/planning failure: zero writes and known failure;
- an entered provider write error or invalid returned response: stop writes, make at most one recovery issue read when context permits, succeed only if it satisfies every predicate, otherwise trusted unknown;
- no uncertain call is repeated and no reconciliation follows unresolved uncertainty;
- a later pre-invocation/cancellation/read failure after a confirmed step is marked confirmed-incomplete for Task 6;
- second final snapshot with unmet predicates is marked confirmed-incomplete;
- no error returns a response record, provider content, names, IDs, or index.

- [ ] **Step 4: Run focused executor tests and confirm failure**

```bash
go test ./internal/provider/gitea -run 'TestIssueEdit(NoOp|Execution|Reconciliation|Bounds|Failures|Unknown)' -count=1
```

Expected: FAIL because edit execution is absent.

- [ ] **Step 5: Implement the sequential bounded executor**

`issueEdit` must: normalize intent; call `issuePreflight`; collect only required catalogs; plan; return immediately on no-op; execute one plan sequentially; read/normalize final state; return if `editSatisfied`; execute one newly built plan; then make one last read and return only if satisfied. Build the public response last and enforce `proto.Size <= 8<<20`.

Wrap each provider write at the invocation boundary with `invokeWrite`. Validate intermediate issue identity/kind/index and the step's immediate relevant result; validate label collections for unique positive IDs/non-empty unique names. Check `ctx.Err()` before each step so cancellation between writes is never mistaken for an attempted write.

For any `ErrWriteOutcomeUnknown`, perform only the bounded recovery read when context allows. If it proves all predicates, return success; otherwise preserve unknown. Record confirmed effects only after a successful validated provider response. Keep this orchestration separate from the pure planner and catalog modules so no new file becomes a provider god module.

- [ ] **Step 6: Run provider and race regressions, then commit**

```bash
gofmt -w internal/provider/gitea/issue_edit.go internal/provider/gitea/issue_edit_test.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/write_outcome.go
go test -race ./internal/provider/gitea -count=1
git add internal/provider/gitea/issue_edit.go internal/provider/gitea/issue_edit_test.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/write_outcome.go
git commit -m "feat(gitea): reconcile bounded issue edits"
```

Expected: exact call counts/order pass and all issue #11 provider behavior remains green.

---

### Task 6: Report trusted partial outcomes through client, server, and audit

**Files:**
- Modify: `internal/rpcstatus/status.go`
- Modify: `internal/rpcstatus/status_test.go`
- Modify: `internal/provider/gitea/issue_edit.go`
- Modify: `internal/client/gitea/client.go`
- Create: `internal/client/gitea/issue_edit_outcome_test.go`
- Modify: `internal/audit/event.go`
- Modify: `internal/audit/writer_test.go`
- Modify: `internal/server/audit.go`
- Modify: `internal/server/gitea.go`
- Create: `internal/server/gitea_edit_test.go`
- Modify: `internal/server/gitea_write_test.go`
- Modify: `internal/server/gitea_response_limit_test.go`

**Interfaces:**
- Consumes: Task 5 execution facts and existing trusted unknown status/lifecycle metadata.
- Produces: `rpcstatus.ErrEditPartial`, exact `FailedPrecondition` detail `{Reason:"GITEA_EDIT_PARTIAL", Domain:"repowolf.dev/gitea"}`, and `IsGiteaEditPartial(error) bool`.
- Produces: audit `OutcomePartial = "partial"` and exact client diagnostic `tea: issue edit partially applied; inspect issue state before retrying`.

- [ ] **Step 1: Write failing trusted-status tests**

Require `rpcstatus.Error(ErrEditPartial)` to emit one exact `ErrorInfo` detail and stable sanitized message. Require round-trip preservation only for the trusted in-process wrapper, while arbitrary provider-created statuses are canonicalized. `IsGiteaEditPartial` must reject code/message/reason/domain/detail-count mismatches and never use substring matching. Retain exact unknown tuple tests.

- [ ] **Step 2: Write failing client diagnostic tests**

For edit commands only, require exact partial diagnostic, exit 1, empty stdout, and no request/provider markers. Unknown status and ambiguous transport errors must retain the issue #11 unknown diagnostic. Pre-write known failures use the generic Gitea diagnostic; issue-kind keeps its corrective message; create/comment/state mutation behavior is unchanged.

- [ ] **Step 3: Write failing authorization, routing, and audit tests**

Prove malformed edit requests fail before policy/provider work; `issues:read` alone is denied; unknown repository, unauthorized repository, missing capability, and wrong provider kind remain indistinguishable; trusted resolved provider ID chooses the adapter; request fields cannot select authority/credential/provider.

After accepted audit, cover no-op/completed, reconciled/completed, pre-write failed/cancelled, confirmed-incomplete partial, unresolved write unknown, and response-limit paths. Assert exactly one accepted and terminal event, operation `gitea.issue_edit`, terminal `partial` only from trusted `ErrEditPartial`, and completed only after a satisfying normalized snapshot. Serialize marker-rich requests/errors and prove audits contain no text, names, IDs, issue index, catalogs, provider data, authority, or credentials.

- [ ] **Step 4: Run focused status/client/server tests and confirm failure**

```bash
go test ./internal/rpcstatus ./internal/client/gitea ./internal/audit ./internal/server -run 'Test.*(EditPartial|IssueEdit|Partial|GiteaWrite|ResponseLimit)' -count=1
```

Expected: FAIL because partial status/outcome and edit service lifecycle are absent.

- [ ] **Step 5: Implement precedence-safe outcome classification**

Add the trusted status with the same wrapper pattern as unknown. In the provider, return `ErrEditPartial` only when at least one step was confirmed and complete postconditions are not proved because of a later known failure or exhausted reconciliation. An uncertain invocation remains unknown unless its recovery read proves complete success; unknown takes precedence over earlier confirmed steps.

In `client.go`, check exact partial status before generic mutation-unknown handling. In audit, add `OutcomePartial` to the closed enum and classify trusted partial before status-code fallback. Do not add changed-family audit metadata. In `gitea.go`, set provider-completed only after Task 5 returns a satisfying response; preserve the predecessor distinction where post-completion client delivery loss can be unknown to the client while server audit remains completed.

- [ ] **Step 6: Run security/server regressions and commit outcome semantics**

```bash
gofmt -w internal/rpcstatus/status.go internal/rpcstatus/status_test.go internal/provider/gitea/issue_edit.go internal/client/gitea/client.go internal/client/gitea/issue_edit_outcome_test.go internal/audit/event.go internal/audit/writer_test.go internal/server/audit.go internal/server/gitea.go internal/server/gitea_edit_test.go internal/server/gitea_write_test.go internal/server/gitea_response_limit_test.go
go test -race ./internal/rpcstatus ./internal/client/gitea ./internal/audit ./internal/server ./internal/provider/gitea -count=1
git add internal/rpcstatus/status.go internal/rpcstatus/status_test.go internal/provider/gitea/issue_edit.go internal/client/gitea/client.go internal/client/gitea/issue_edit_outcome_test.go internal/audit/event.go internal/audit/writer_test.go internal/server/audit.go internal/server/gitea.go internal/server/gitea_edit_test.go internal/server/gitea_write_test.go internal/server/gitea_response_limit_test.go
git commit -m "feat(gitea): report partial issue edits"
```

Expected: all trusted status, leak, authorization, response-bound, and predecessor write tests pass.

---

### Task 7: Prove editing against pinned Gitea and run repository gates

**Files:**
- Create: `integration/gitea_issue_edit_test.go`
- Modify: `integration/gitea_fixture_test.go`
- Modify only if fixture behavior requires it: `integration/testdata/gitea-policy.yaml`

**Interfaces:**
- Consumes: packaged `tea`/service binaries, existing TLS/policy fixture, and digest-pinned Gitea 1.27.2.
- Produces: end-to-end proof of text clearing/editing, assignee set/add/remove, label add/remove, deterministic reconciliation, partial classification, unknown retention, canonical output, independent final reads, and bounded calls.

- [ ] **Step 1: Extend fixture controls without adding static-text tests**

Add narrowly scoped hooks that can pause after a selected initial provider write, let a controlled actor mutate issue assignees/labels, and fail a selected later step after an earlier confirmed write. Reuse the existing `issues:write` policy grant and setup token; if YAML must change, verify it by exercising authorization rather than asserting YAML text. Keep provider tokens out of `tea` argv/environment and logs.

- [ ] **Step 2: Write the shortest successful demonstration**

Create an issue and two labels/users in setup, then run packaged commands through TLS:

```text
tea issues edit INDEX -r CanonicalOwner/CanonicalRepo --title "edited title" --description "edited body" --set-assignees CanonicalOwner -L bug -o json
tea issues e INDEX -r CanonicalOwner/CanonicalRepo --description "" --add-assignees second --remove-assignees CanonicalOwner --add-labels urgent --remove-labels bug -o table
```

Split conflicting-precedence family demonstrations across commands so each effective action is independently observable. Read the issue directly from Gitea after each command; assert body clearing, exact/add/remove semantics, canonical output keys/order, no changed-fields output, one accepted/completed audit pair, and call counts within one issue preflight, required catalogs, writes, and final read.

- [ ] **Step 3: Add controlled reconciliation, partial, and unknown cases**

Pause between initial write and final read, have a concurrent actor add unrelated assignee/label members, and assert add/remove preserves them while a separate set-assignees case converges to exact equality. Assert only unmet predicates are corrected and total calls remain within two passes/three reads.

Inject a known failure after an earlier confirmed family write; require exit 1, empty stdout, exact partial diagnostic, terminal `partial`, independently observed partial state, and no later/unplanned calls. Retain or extend response-loss coverage so an uncertain invocation is called once, recovery can prove success when predicates hold, and otherwise the exact unknown diagnostic/audit outcome remains.

- [ ] **Step 4: Run the tagged pinned-Gitea tests**

```bash
go test -race -tags=gitea_integration ./integration -run 'TestRestrictedTea(ReadOperations|IssueWrites|IssueEdit)AgainstGitea|TestGiteaIssueEdit(Concurrent|Partial|Unknown)' -count=1 -v
```

Expected: PASS against pinned Gitea 1.27.2 with deterministic call counts and independently verified final state.

- [ ] **Step 5: Run generated, static, and full Go gates**

```bash
go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
git diff --check
```

Expected: every command exits zero. These directly verify generated/static structure; no low-value tests are added for workflow, dependency, policy, or documentation text.

- [ ] **Step 6: Run Nix, OCI, and release regression gates**

```bash
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
```

Expected: packages, OCI/release artifacts, GitHub, Git, Gitea predecessor operations, security boundaries, and integration-independent checks remain green.

- [ ] **Step 7: Check scope and commit the integration proof**

```bash
git status --short
git diff --name-only 89d007a...HEAD
git diff --check
git add integration/gitea_issue_edit_test.go integration/gitea_fixture_test.go
# Add integration/testdata/gitea-policy.yaml only if its runtime grant changed.
git commit -m "test(gitea): prove issue editing end to end"
git status --short
git log --oneline 89d007a..HEAD
```

Expected: a clean worktree with seven scoped Conventional Commit implementation commits; no pull-request editing, generic forge model, unbounded retry, provider selector, credential artifact, or unrelated refactor is present.
