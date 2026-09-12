# Gitea Issue Read Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authorized agent list one page of Gitea issues and inspect one issue, optionally with every comment within the documented bound, through the restricted `tea` client.

**Architecture:** Extend the issue-#9 Gitea vertical slice additively: typed Protobuf branches feed a strict issue parser, the existing trusted provider-ID dispatcher, and one serialized SDK adapter. Keep parsing/rendering, SDK normalization/projection, comment pagination, and server lifecycle validation in focused units; reuse the established repository authorization, response limits, audit lifecycle, transport, and runtime composition.

**Tech Stack:** Go 1.26, Protocol Buffers/gRPC, `code.gitea.io/sdk/gitea` v0.25.1, `buf`, Docker, Nix, and the existing RepoWolf integration harness.

**Spec:** `docs/specs/2026-09-11-issue-10-gitea-roadmap-6-11-issue-read-operations-design.md`

## Global Constraints

- Begin implementation from the merged issue-#9 repository-view baseline. The files under `internal/client/gitea`, `internal/provider/gitea`, `internal/server/gitea.go`, `internal/app/gitea_executor.go`, and `integration/gitea_repository_test.go` must be present before Task 1; integrate the predecessor rather than recreating it.
- Keep `repowolf.v1` additive: use fresh tags, `UNSPECIFIED` enum zero values, and generate Go only with `scripts/generate.sh`.
- Require `--repo`/`-r OWNER/REPO` for every issue command and exactly `issues:read` after strict typed-request validation.
- Accept only the list/detail forms, aliases, flags, fields, defaults, output formats, and bounds in the approved spec; reject mutation, pull-request, comment-command, inference, interactive, and raw API surfaces.
- Keep argv at most 64 arguments and 64 KiB; require valid UTF-8 without NUL. Keep every filter at most 255 UTF-8 bytes without NUL.
- Use canonical configured owner/name and trusted provider ID for SDK calls. Client input cannot choose authority, credentials, SDK client, endpoint, or raw query.
- List makes exactly one issue-only SDK request and preserves provider order. An owner-filter mismatch returns an empty page after authorization without an SDK call.
- View makes exactly one issue call. It makes no comment call unless requested and validates issue kind before comment retrieval.
- Retrieve comments sequentially in pages of 50, accept at most 1,000 comments, and probe page 21 after a full page 20 to distinguish exact completion from overflow. Never retry or return partial comments.
- Reject pull requests from both list and view. Emit the corrective view diagnostic only after exact repository authorization.
- Validate complete SDK records before projection. Always carry list identity/kind needed for semantic validation, but render exactly the caller-selected fields in caller order.
- Preserve issue-#9 TLS, redirects, token injection, two-minute deadline, 8 MiB provider-body limit, final Protobuf limit, rendered-output limit, concurrency, cancellation, and accepted/terminal audit behavior.
- Audit only `gitea.issue_list` or `gitea.issue_view` plus the bounded lifecycle metadata. Never audit indices, selectors, filters, fields, issue/comment data, pagination URLs, SDK errors, provider details, or tokens.
- Preserve repository view, GitHub, Git, packaging, and conditional Gitea service registration behavior. Do not add a registry, generic forge DTO, upstream `tea` production dependency, mutation, pull-request operation, or public comment pagination flag.
- No `AGENTS.md` exists in this worktree or its repository parents. Use `.github/workflows/ci.yml`, `scripts/check-generated.sh`, and `scripts/check-release.sh` as validation authority.

## Testing Value Gate

Every planned automated test exercises production parsing/rendering, protocol presence, reusable validation/projection, SDK request mapping, pagination/order, authorization, anti-enumeration, error sanitization, response limits, provider isolation, or the real client-to-Gitea path. Each can fail on a meaningful regression and is worth rerunning. Do not add tests that merely inspect workflow YAML, generated file text, dependency versions, image declarations, or documentation copy; verify those directly with `buf`, generation freshness, builds, tagged integration execution, Nix/release checks, and `git diff --check`.

## File Structure

### New files

- `internal/protocol/gitea_contract_test.go`: descriptor-level branch/tag, enum, presence, and typed-field contract tests.
- `internal/client/gitea/issue_parse_test.go`: issue grammar tables and typed-request assertions.
- `internal/client/gitea/issue_render.go`: issue list/detail projection and deterministic output encoding.
- `internal/client/gitea/issue_render_test.go`: exact list/detail bytes, presence, semantic validation, and output-limit tests.
- `internal/provider/gitea/issue_normalize.go`: complete SDK issue/comment validation and Protobuf normalization.
- `internal/provider/gitea/issue_normalize_test.go`: malformed issue/comment and projection tests.
- `internal/provider/gitea/issue_list_test.go`: list option mapping, order, kind, owner-filter, and page-limit tests.
- `internal/provider/gitea/issue_view_test.go`: detail call, kind, status, and no-comment-default tests.
- `internal/provider/gitea/comment_pagination.go`: bounded sequential comment retrieval.
- `internal/provider/gitea/comment_pagination_test.go`: page termination, overflow probe, ordering, cancellation, and partial-result tests.
- `integration/gitea_issues_test.go`: digest-pinned end-to-end issue list/view demonstration.

### Modified files

- `proto/repowolf/v1/gitea.proto`: additive issue enums, requests, records, and list/view branches.
- `gen/repowolf/v1/gitea.pb.go`, `gen/repowolf/v1/gitea_grpc.pb.go`: generated protocol output.
- `internal/client/gitea/parse.go`, `parse_test.go`, `fuzz_test.go`: route repository and issue grammars while retaining global bounds.
- `internal/client/gitea/render.go`, `render_test.go`: dispatch repository versus issue rendering and retain common sanitization/limit helpers.
- `internal/client/gitea/client.go`, `client_test.go`: issue usage diagnostics and corrective kind-mismatch handling.
- `internal/provider/gitea/adapter.go`, `adapter_test.go`: one serialized SDK seam and operation dispatch for repository/list/view.
- `internal/provider/gitea/operation.go`: strict typed validation, `issues:read`, and canonical operation names.
- `internal/server/gitea.go`, `gitea_test.go`, `gitea_response_limit_test.go`: issue lifecycle, anti-enumeration, status, audit, and final-size coverage.
- `internal/rpcstatus/status.go`, `status_test.go`: sanitized not-found and issue-kind statuses.
- `internal/app/gitea_executor_test.go`: issue request isolation across configured Gitea providers.
- `integration/gitea_repository_test.go`: share pinned container/bootstrap helpers without changing repository-view assertions.
- `integration/testdata/gitea-policy.yaml`: grant `issues:read` to the existing authorized fixture repository.
- `.github/workflows/ci.yml`: run the expanded tagged Gitea read integration gate.

---

### Task 1: Add the typed issue protocol

**Files:**
- Modify: `proto/repowolf/v1/gitea.proto:17-54`
- Generate: `gen/repowolf/v1/gitea.pb.go`
- Generate: `gen/repowolf/v1/gitea_grpc.pb.go`
- Create: `internal/protocol/gitea_contract_test.go`

**Interfaces:**
- Consumes: existing `GiteaRequest.context`, `GiteaResponse.meta`, and tag `10` repository branches.
- Produces: request/result tags `11` (`issue_list`) and `12` (`issue_view`).
- Produces: `GiteaIssueState`, `GiteaIssueKind`, `GiteaIssueField`, `GiteaIssueListRequest`, `GiteaIssueViewRequest`, `GiteaIssueRecord`, and `GiteaCommentRecord`.

- [ ] **Step 1: Write the additive Protobuf messages and enums**

Add fresh branches and the exact typed surface below; preserve all repository-view tags and names:

```proto
enum GiteaIssueState {
  GITEA_ISSUE_STATE_UNSPECIFIED = 0;
  GITEA_ISSUE_STATE_OPEN = 1;
  GITEA_ISSUE_STATE_CLOSED = 2;
  GITEA_ISSUE_STATE_ALL = 3;
}
enum GiteaIssueKind {
  GITEA_ISSUE_KIND_UNSPECIFIED = 0;
  GITEA_ISSUE_KIND_ISSUE = 1;
}
enum GiteaIssueField {
  GITEA_ISSUE_FIELD_UNSPECIFIED = 0;
  GITEA_ISSUE_FIELD_INDEX = 1;
  GITEA_ISSUE_FIELD_STATE = 2;
  GITEA_ISSUE_FIELD_AUTHOR = 3;
  GITEA_ISSUE_FIELD_AUTHOR_ID = 4;
  GITEA_ISSUE_FIELD_URL = 5;
  GITEA_ISSUE_FIELD_TITLE = 6;
  GITEA_ISSUE_FIELD_BODY = 7;
  GITEA_ISSUE_FIELD_CREATED = 8;
  GITEA_ISSUE_FIELD_UPDATED = 9;
  GITEA_ISSUE_FIELD_DEADLINE = 10;
  GITEA_ISSUE_FIELD_ASSIGNEES = 11;
  GITEA_ISSUE_FIELD_MILESTONE = 12;
  GITEA_ISSUE_FIELD_LABELS = 13;
  GITEA_ISSUE_FIELD_COMMENTS = 14;
  GITEA_ISSUE_FIELD_REPO = 15;
  GITEA_ISSUE_FIELD_OWNER = 16;
  GITEA_ISSUE_FIELD_KIND = 17;
}
message GiteaIssueListRequest {
  GiteaIssueState state = 1;
  optional string keyword = 2;
  optional string author = 3;
  optional string assignee = 4;
  optional string mentions = 5;
  google.protobuf.Timestamp from = 6;
  google.protobuf.Timestamp until = 7;
  optional string owner = 8;
  int32 page = 9;
  int32 limit = 10;
  repeated GiteaIssueField fields = 11;
}
message GiteaIssueViewRequest {
  int64 index = 1;
  bool include_comments = 2;
}
message GiteaCommentRecord {
  int64 id = 1;
  int64 author_id = 2;
  string author = 3;
  string url = 4;
  string body = 5;
  google.protobuf.Timestamp created = 6;
  google.protobuf.Timestamp updated = 7;
}
message GiteaIssueRecord {
  int64 index = 1;
  GiteaIssueState state = 2;
  GiteaIssueKind kind = 3;
  int64 author_id = 4;
  string author = 5;
  string url = 6;
  string title = 7;
  string body = 8;
  string repo = 9;
  string owner = 10;
  google.protobuf.Timestamp created = 11;
  google.protobuf.Timestamp updated = 12;
  google.protobuf.Timestamp deadline = 13;
  repeated string assignees = 14;
  optional string milestone = 15;
  repeated string labels = 16;
  int64 comment_count = 17;
  repeated GiteaCommentRecord comments = 18;
}
message GiteaIssueListResult { repeated GiteaIssueRecord issues = 1; }
message GiteaIssueViewResult { GiteaIssueRecord issue = 1; }
```

Add `GiteaIssueListRequest issue_list = 11;` and `GiteaIssueViewRequest issue_view = 12;` to `GiteaRequest.operation`; add matching result messages at tags `11` and `12` to `GiteaResponse.result`.

- [ ] **Step 2: Write descriptor-level protocol contract tests**

Create `internal/protocol/gitea_contract_test.go` using generated `ProtoReflect` descriptors, not source-text matching. Assert request/result branch names and fresh tags `11`/`12`, enum numeric values and zero `UNSPECIFIED` values, optional presence for string filters and milestone, timestamp message types for `from`, `until`, `created`, `updated`, and `deadline`, repeated ordered fields/issues/comments, and absence of raw JSON, endpoint, token, arbitrary-query, or generic map fields. Include a marshal/unmarshal presence test proving unset and explicitly empty optional filters remain distinct.

- [ ] **Step 3: Generate and validate compatibility**

Run:

```bash
scripts/generate.sh
go test ./internal/protocol -run 'TestGiteaIssue' -count=1
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
git diff -- proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go internal/protocol/gitea_contract_test.go
```

Expected: all commands exit 0; the diff is additive, generated output is stable, and tests exercise the generated API contract rather than reparsing static `.proto` text.

- [ ] **Step 4: Commit the protocol**

```bash
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go internal/protocol/gitea_contract_test.go
git commit -m "feat(gitea): add issue read protocol"
```

---

### Task 2: Parse the closed issue list and detail grammar

**Files:**
- Modify: `internal/client/gitea/parse.go:12-123`
- Modify: `internal/client/gitea/parse_test.go`
- Create: `internal/client/gitea/issue_parse_test.go`
- Modify: `internal/client/gitea/fuzz_test.go`

**Interfaces:**
- Consumes: `Parse(args []string) (command, error)`, `RequestContext`, and the protocol enums from Task 1.
- Produces: `command.fields []GiteaIssueField` for list rendering and typed list/view request branches.
- Produces: default fields `index,title,state,author,milestone,labels,owner,repo`; list default `state=open,page=1,limit=30,output=table`; detail default `output=simple`.

- [ ] **Step 1: Write failing parser tables**

Use table-driven tests that assert all aliases (`issues`, `issue`, `i`; `list`, `ls`), implicit list, detail indices, every long/short flag, explicit optional-string presence, RFC 3339 conversion, defaults, and field order. Include a typed assertion shaped as:

```go
parsed, err := Parse([]string{
    "issues", "list", "--repo", "Owner/Repo", "--state", "all",
    "-k", "needle", "-F", "2026-09-01T00:00:00Z",
    "-u", "2026-09-10T00:00:00Z", "-p", "2", "--lm", "50",
    "--fields", "title,index,comments", "-o", "json",
})
request := parsed.request.GetIssueList()
if err != nil || request.GetState() != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_ALL ||
    request.GetPage() != 2 || request.GetLimit() != 50 || !request.HasKeyword() ||
    !reflect.DeepEqual(request.GetFields(), []repowolfv1.GiteaIssueField{
        repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_TITLE,
        repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_INDEX,
        repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_COMMENTS,
    }) || parsed.format != outputJSON {
    t.Fatalf("Parse() = %#v, %v", parsed, err)
}
```

Add rejection rows for duplicate long/short aliases, `--flag=value`, empty/unknown/duplicate fields, `--kind`/`-K`, state outside `all|open|closed`, zero/negative/overflow index/page/limit, limit 51, empty or over-255-byte filters, invalid UTF-8/NUL, `from > until`, list-only flags on detail, `--comments` on list, repeated comments, mutation/comment/pull commands, misplaced subcommands, extra positionals, repository grammar regressions, 65 arguments, and argv over 64 KiB. Assert parser errors never contain rejected filter/index text.

- [ ] **Step 2: Run the parser tests and observe failure**

```bash
go test ./internal/client/gitea -run 'TestParseIssue|TestParseRepository|TestParseRejectsClosedGrammar' -count=1
```

Expected: FAIL because issue commands are unsupported.

- [ ] **Step 3: Split bounded preprocessing from command-specific parsers**

Retain `parseSlug` and add focused helpers with these signatures:

```go
var defaultIssueFields = []repowolfv1.GiteaIssueField{
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_INDEX,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_TITLE,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_STATE,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_AUTHOR,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_MILESTONE,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_LABELS,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_OWNER,
    repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_REPO,
}

type command struct {
    request *repowolfv1.GiteaRequest
    format  outputFormat
    fields  []repowolfv1.GiteaIssueField
}

func validateArgv(args []string) error
func parseRepository(args []string) (command, error)
func parseIssues(args []string) (command, error)
func parseIssueList(args []string, start int) (command, error)
func parseIssueView(args []string, index int64) (command, error)
func parseIssueFields(value string) ([]repowolfv1.GiteaIssueField, error)
func parsePositive(value string, maximum int64) (int64, error)
func validateFilter(value string) error
```

`Parse` calls `validateArgv` once, switches only on the first token, and dispatches repository grammar unchanged or issue grammar. Use a `seen` map keyed by semantic flag so long/short aliases collide. Parse integers with `strconv.ParseInt`, timestamps with `time.Parse(time.RFC3339)`, preserve optional string presence with local pointers, copy default fields before use, and populate only owner/name in `RequestContext.Repository`.

- [ ] **Step 4: Strengthen fuzz invariants**

Extend `FuzzParse` so every successful issue request has a valid selector, positive index or positive bounded page/limit, non-`UNSPECIFIED` state/fields, unique fields, and no unexpected operation branch. Keep the existing no-panic and bounded-input seed coverage; never place arbitrary fuzz input in a failure message returned to users.

- [ ] **Step 5: Format, test, and commit parsing**

```bash
gofmt -w internal/client/gitea/parse.go internal/client/gitea/parse_test.go internal/client/gitea/issue_parse_test.go internal/client/gitea/fuzz_test.go
go test -race ./internal/client/gitea -run 'TestParse|FuzzParse' -count=1
git diff --check
git add internal/client/gitea/parse.go internal/client/gitea/parse_test.go internal/client/gitea/issue_parse_test.go internal/client/gitea/fuzz_test.go
git commit -m "feat(tea): parse issue read commands"
```

---

### Task 3: Render deterministic bounded issue output

**Files:**
- Create: `internal/client/gitea/issue_render.go`
- Create: `internal/client/gitea/issue_render_test.go`
- Modify: `internal/client/gitea/render.go:12-97`
- Modify: `internal/client/gitea/render_test.go`
- Modify: `internal/client/gitea/client.go:13-88`
- Modify: `internal/client/gitea/client_test.go`

**Interfaces:**
- Consumes: `command.fields`, `GiteaIssueListResult`, and `GiteaIssueViewResult`.
- Produces: `renderIssueList(command, *GiteaIssueListResult) ([]byte, error)` and `renderIssueView(command, *GiteaIssueViewResult) ([]byte, error)`.
- Preserves: complete-buffer-before-write and `maxRenderedBytes == 8<<20`.

- [ ] **Step 1: Write exact-byte renderer tests**

Build canonical issue/comment fixtures with UTC-normalized timestamps, absent/present deadline and milestone, empty arrays, controls, multiline bodies, and requested/unrequested comments. Assert:

```text
index\ttitle\tstate\tcomments
7\tA title\topen\t2
```

```text
7 A title open 2
```

```json
[{"title":"A title","index":7,"labels":["bug"],"deadline":"2026-09-20T00:00:00Z"}]
```

Also assert empty list bytes for each format, JSON key order matching `--fields`, typed number/array values, lowercase state/kind, empty text cells, JSON omission of absent deadline/milestone, and no list comments hydration. For detail, assert fixed issue order, skipped absent/unrequested values, body last and verbatim, fixed comment field order with body last, table comments as compact JSON, and JSON omission of `comments` unless requested.

Add malformed-response rows for nil response/result/record, empty request ID, wrong result branch, invalid/unknown required enums, non-positive identity IDs, invalid selected-field timestamps, `updated < created`, missing selected required strings, invalid selected owner/repo, list length greater than request limit, hydrated list comments, comments present when not requested, and repeated/non-increasing comment IDs. For projected list records, always validate index/state/kind and validate other values only when their field was selected; detail validates the complete record. Reuse the exact-limit/one-byte-over test to prove no partial write.

- [ ] **Step 2: Run renderer tests and observe failure**

```bash
go test ./internal/client/gitea -run 'TestRenderIssue|TestRenderFormats|TestRenderRejects|TestRenderEnforces' -count=1
```

Expected: FAIL because `render` accepts only repository view.

- [ ] **Step 3: Implement field-driven list values and fixed detail values**

Create focused helpers rather than expanding repository rendering into one switch:

```go
func renderIssueList(parsed command, result *repowolfv1.GiteaIssueListResult) ([]byte, error)
func renderIssueView(parsed command, result *repowolfv1.GiteaIssueViewResult) ([]byte, error)
func validateIssueRecord(issue *repowolfv1.GiteaIssueRecord, commentsRequested bool) error
func issueFieldName(field repowolfv1.GiteaIssueField) (string, error)
func issueFieldValue(issue *repowolfv1.GiteaIssueRecord, field repowolfv1.GiteaIssueField) (any, bool, error)
func commentFields(comment *repowolfv1.GiteaCommentRecord) ([]orderedField, error)
func marshalOrderedObject(fields []orderedField) ([]byte, error)

type orderedField struct {
    name    string
    value   any
    present bool
}
```

Do not use a Go map for ordered JSON. Encode object members in order with `json.Marshal` for each name/value. Render text arrays with `strings.Join(values, ", ")`, JSON arrays as non-nil slices, timestamps as UTC RFC 3339, and text scalars through the existing `cell` control-character sanitizer. Keep issue and comment bodies verbatim only in the documented body positions.

- [ ] **Step 4: Dispatch response branches and stabilize diagnostics**

Make `render` require non-empty request metadata first, then require the branch matching `command.request`; leave repository rendering byte-identical. Update the bounded usage diagnostic to name the accepted repository and issue forms without echoing input. In `executeCommand`, recognize only the trusted corrective gRPC status introduced in Task 6 and emit `tea: index is a pull request; use tea pulls\n`; all other RPC/provider/validation/render/write failures retain `tea: Gitea operation failed\n` and exit 1.

- [ ] **Step 5: Format, test, and commit rendering**

```bash
gofmt -w internal/client/gitea/issue_render.go internal/client/gitea/issue_render_test.go internal/client/gitea/render.go internal/client/gitea/render_test.go internal/client/gitea/client.go internal/client/gitea/client_test.go
go test -race ./internal/client/gitea -count=1
git diff --check
git add internal/client/gitea
git commit -m "feat(tea): render issue read output"
```

---

### Task 4: Map and normalize issue list operations

**Files:**
- Modify: `internal/provider/gitea/adapter.go:12-112`
- Modify: `internal/provider/gitea/adapter_test.go`
- Create: `internal/provider/gitea/issue_normalize.go`
- Create: `internal/provider/gitea/issue_normalize_test.go`
- Create: `internal/provider/gitea/issue_list_test.go`
- Modify: `internal/provider/gitea/operation.go:1-29`

**Interfaces:**
- Consumes: SDK `ListRepoIssues(owner, repo string, sdk.ListIssueOption)` and the canonical `policy.ResolvedRepository`.
- Produces: one shared `giteaAPI` that serializes all uses of SDK `SetContext` and clears request context after every call.
- Produces: `normalizeIssue(*sdk.Issue, owner, repo string) (*normalizedIssue, error)` and `projectIssue(*normalizedIssue, fields []GiteaIssueField) *GiteaIssueRecord`.

- [ ] **Step 1: Write failing operation and list adapter tests**

Add request-validation tables for list state, optional-filter presence/content, valid timestamps/order, page/limit, nonempty unique approved fields, and closed operation oneof. Assert `Capability(list)==config.IssuesRead` and `OperationName(list)=="gitea.issue_list"`.

Use a fake API to assert exactly one call with canonical `Owner/Repo` and:

```go
sdk.ListIssueOption{
    ListOptions: sdk.ListOptions{Page: 2, PageSize: 50},
    State: sdk.StateAll,
    Type: sdk.IssueTypeIssue,
    KeyWord: "needle",
    Since: from,
    Before: until,
    CreatedBy: "alice",
    AssignedBy: "bob",
    MentionedBy: "carol",
    Owner: "Owner",
}
```

Assert provider-order preservation, empty pages, selected-field projection, identity/kind retention, owner-filter case match, owner-filter mismatch with zero API calls, and rejection of more records than `limit`, nil issues, pull-request markers, mismatched repository identity, invalid states/timestamps/counts/IDs/strings, nil/empty assignees or labels, and malformed milestone data. Verify SDK/provider error text is replaced by trusted sentinels and no retry occurs.

- [ ] **Step 2: Run adapter tests and observe failure**

```bash
go test ./internal/provider/gitea -run 'TestGiteaOperation|TestIssueList|TestNormalizeIssue' -count=1
```

Expected: FAIL because list dispatch, normalization, and SDK calls do not exist.

- [ ] **Step 3: Serialize the expanded SDK seam**

Replace the repository-only getter seam with one adapter-owned API interface:

```go
type issueCommentPage struct {
    entryCount int
    comments   []*sdk.Comment
}

type giteaAPI interface {
    GetRepo(context.Context, string, string) (*sdk.Repository, error)
    ListRepoIssues(context.Context, string, string, sdk.ListIssueOption) ([]*sdk.Issue, error)
    GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error)
    ListIssueTimeline(context.Context, string, string, int64, sdk.ListIssueCommentOptions) (issueCommentPage, error)
}

type sdkClient interface {
    SetContext(context.Context)
    GetRepo(string, string) (*sdk.Repository, *sdk.Response, error)
    ListRepoIssues(string, string, sdk.ListIssueOption) ([]*sdk.Issue, *sdk.Response, error)
    GetIssue(string, string, int64) (*sdk.Issue, *sdk.Response, error)
    ListIssueTimeline(string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.TimelineComment, *sdk.Response, error)
}
```

Use one `serializedSDKAPI{client, slot}` per `RepositoryAdapter`; each method waits on the same cancellation-aware slot, sets the request context, invokes one SDK method, resets to `context.Background()` in `defer`, and releases the slot. Its custom `GetIssue` returns the trusted numeric `sdk.Response.StatusCode` separately from the redacted error so Task 5 can recognize 404 without inspecting error text or response bodies. Keep `NewRepositoryAdapter(*sdk.Client)` and repository-view behavior compatible.

- [ ] **Step 4: Implement complete normalization before projection**

Use an internal value that preserves validated presence:

```go
type normalizedIssue struct {
    index, authorID, commentCount int64
    state repowolfv1.GiteaIssueState
    author, url, title, body, owner, repo string
    created, updated time.Time
    deadline *time.Time
    assignees, labels []string
    milestone *string
}
```

`normalizeIssue` rejects nil, `PullRequest != nil`, non-positive index/author ID, negative comments, unknown state, empty required author/title/HTML URL/repository identity, noncanonical case-sensitive owner/repo, invalid UTF-8/NUL in every projected string, zero/invalid timestamps, `updated < created`, invalid deadline, nil nested values, and empty nested names. Copy slices. `projectIssue` always sets positive `Index`, valid `State`, and `Kind=ISSUE` for client semantic validation, then sets only other requested fields; `comments` maps to `CommentCount`, not hydrated records.

- [ ] **Step 5: Dispatch list with exact options and bounds**

In `RepositoryAdapter.Execute`, switch on the validated operation branch. For list, compare an optional owner filter with canonical owner using `strings.EqualFold`; return an initialized empty list result on mismatch. Otherwise map every typed filter to `sdk.ListIssueOption`, force `Type=sdk.IssueTypeIssue`, make one call, reject `len(issues)>limit`, normalize every record before projecting any response, and preserve slice order.

- [ ] **Step 6: Format, test, and commit list operations**

```bash
gofmt -w internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/provider/gitea/operation.go internal/provider/gitea/issue_normalize.go internal/provider/gitea/issue_normalize_test.go internal/provider/gitea/issue_list_test.go
go test -race ./internal/provider/gitea -run 'TestGiteaOperation|TestRepositoryAdapter|TestSDK|TestIssueList|TestNormalizeIssue' -count=1
git diff --check
git add internal/provider/gitea
git commit -m "feat(gitea): list validated issues"
```

---

### Task 5: Add issue view and bounded comment pagination

**Files:**
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/issue_normalize.go`
- Create: `internal/provider/gitea/issue_view_test.go`
- Create: `internal/provider/gitea/comment_pagination.go`
- Create: `internal/provider/gitea/comment_pagination_test.go`

**Interfaces:**
- Consumes: `GetIssue`, paginated `ListIssueTimeline`, and normalization from Task 4.
- Produces: `normalizeComment(*sdk.Comment) (*GiteaCommentRecord, error)`.
- Produces: `loadIssueComments(context.Context, giteaAPI, owner, repo string, index int64) ([]*GiteaCommentRecord, error)`.

- [ ] **Step 1: Write failing issue-view tests**

Assert one canonical `GetIssue("Owner","Repo",7)` call, full detail mapping and optional presence, no comment calls by default, provider not-found mapping, cancellation/deadline passthrough, provider-error sanitization, no retry, nil/mismatched-index rejection, and pull-request rejection before any comment call. Assert `Capability(view)==config.IssuesRead` and `OperationName(view)=="gitea.issue_view"`.

- [ ] **Step 2: Write failing comment pagination tests**

Use a page-recording fake and table rows for empty page 1, short page 1, two pages, exactly 1,000 increasing comments followed by empty page 21, page-21 overflow, a provider page longer than 50, nil/malformed comments, repeated or decreasing IDs within/across pages, cancellation before page 1 and between pages, page errors, and aggregate response-limit propagation. Every failure must return a nil result, make no retry, and stop at the failing page.

- [ ] **Step 3: Run view and pagination tests and observe failure**

```bash
go test ./internal/provider/gitea -run 'TestIssueView|TestCommentPagination|TestNormalizeComment' -count=1
```

Expected: FAIL because view/comment operations are not implemented.

- [ ] **Step 4: Normalize comments and implement the bounded loop**

`normalizeComment` requires positive comment/author IDs, non-nil author, nonempty author login and HTML URL, valid UTF-8 without NUL for author/URL/body, required valid timestamps, and `updated >= created`. Use Gitea's issue timeline endpoint because the pinned Gitea 1.27.2 issue-comments endpoint does not apply `page` or `limit`, while the timeline endpoint applies both. Retain only timeline entries typed as comments, use the raw timeline entry count for page termination, and require the normalized total to match the issue's validated comment count.

Implement these constants and loop semantics:

```go
const (
    commentPageSize = 50
    maximumCommentPages = 20
    maximumComments = commentPageSize * maximumCommentPages
)

for page := 1; page <= maximumCommentPages+1; page++ {
    if err := ctx.Err(); err != nil { return nil, err }
    values, err := api.ListIssueTimeline(ctx, owner, repo, index, sdk.ListIssueCommentOptions{
        ListOptions: sdk.ListOptions{Page: page, PageSize: commentPageSize},
    })
    if err != nil { return nil, classifyProviderError(ctx, err) }
    if values.entryCount > commentPageSize { return nil, rpcstatus.ErrProviderFailure }
    if page == maximumCommentPages+1 {
        if values.entryCount != 0 { return nil, runner.ErrOutputLimit }
        return completeIssueComments(issue, comments)
    }
    // Count all timeline entries for page termination, normalize only typed
    // comment entries, require each comment ID to exceed the previous ID, then
    // append the page atomically.
    if values.entryCount < commentPageSize { return completeIssueComments(issue, comments) }
}
```

When page 20 is short, return without page 21. When page 20 is full, page 21 is a probe only; any record means overflow and none is appended.

- [ ] **Step 5: Dispatch issue view without partial responses**

Call `GetIssue` exactly once and use only the separately returned trusted HTTP status code to map 404 to `rpcstatus.ErrNotFound`; never parse SDK error text. Return `rpcstatus.ErrIssueKind` when `PullRequest != nil`, then normalize the issue and require its index to equal the requested positive index. Only after kind/record validation, call `loadIssueComments` when `include_comments`; assign comments to the normalized full-detail record only after the complete loop succeeds. Construct `GiteaIssueViewResult` last so no failure exposes a partial record.

- [ ] **Step 6: Format, test, and commit view operations**

```bash
gofmt -w internal/provider/gitea/adapter.go internal/provider/gitea/issue_normalize.go internal/provider/gitea/issue_view_test.go internal/provider/gitea/comment_pagination.go internal/provider/gitea/comment_pagination_test.go
go test -race ./internal/provider/gitea -run 'TestIssueView|TestCommentPagination|TestNormalizeComment|TestRepositoryAdapter|TestSDK' -count=1
git diff --check
git add internal/provider/gitea
git commit -m "feat(gitea): view issues with bounded comments"
```

---

### Task 6: Enforce issue authorization, status, audit, limits, and provider isolation

**Files:**
- Modify: `internal/rpcstatus/status.go:12-75`
- Modify: `internal/rpcstatus/status_test.go`
- Modify: `internal/server/gitea.go:12-69`
- Modify: `internal/server/gitea_test.go`
- Modify: `internal/server/gitea_response_limit_test.go`
- Modify: `internal/app/gitea_executor_test.go`

**Interfaces:**
- Consumes: `ValidateRequest`, `Capability`, and `OperationName` from Tasks 4-5.
- Produces: `rpcstatus.ErrNotFound` mapped to `codes.NotFound, "not found"` and `rpcstatus.ErrIssueKind` mapped to `codes.FailedPrecondition, "index is a pull request; use tea pulls"`.
- Preserves: `providerLifecycle.Resolve` before adapter dispatch and `providerLifecycle.Complete` after a complete typed response.

- [ ] **Step 1: Write failing status and service tests**

Add exact status assertions for the two trusted sentinels and canonicalize incoming untrusted gRPC statuses without preserving arbitrary provider text.

For both issue branches, prove malformed requests fail before policy/provider work; valid requests require `issues:read`; repository-read-only, unknown repository, ungranted repository, and wrong provider kind all return indistinguishable permission denial with zero adapter calls. Prove case-folded selector resolution passes canonical owner/name, accepted audit failure stops before adapter use, adapter failure has accepted then terminal lifecycle metadata, successful responses receive request ID, and nil/over-limit responses fail without output.

For kind mismatch, assert unauthorized requests remain permission denied and never call the adapter; only an authorized exact repository can return the corrective `FailedPrecondition` status. Assert audit records use only `gitea.issue_list`/`gitea.issue_view` and contain none of the index/filter/field/content/provider-error marker strings.

- [ ] **Step 2: Run service tests and observe failure**

```bash
go test ./internal/rpcstatus ./internal/server ./internal/app -run 'Test.*(Gitea|Issue|Status)' -count=1
```

Expected: FAIL until issue capabilities/operations and trusted statuses flow through the service.

- [ ] **Step 3: Add only trusted sanitized status mappings**

Extend `rpcstatus` with:

```go
var (
    ErrNotFound = errors.New("not found")
    ErrIssueKind = errors.New("issue kind mismatch")
)
```

Map them before generic provider failure. Add `codes.NotFound` and `codes.FailedPrecondition` canonical cases whose messages contain no incoming status text. The corrective message is fixed source text and is never built from an index or provider response.

- [ ] **Step 4: Route issue operations through the existing lifecycle**

Keep `giteaService.Execute` ordering exactly: validate typed request, compute capability/operation, validate selector, resolve exact repository and write accepted audit, dispatch trusted executor, attach metadata/check final Protobuf size, then return. Do not special-case authorization in the adapter or expose repository metadata before `Resolve` succeeds.

Update `giteaResponseWithFinalSize` to construct exact-limit and one-byte-over issue list/view responses as well as repository responses. Test projected body and aggregate comments near the limit, including metadata bytes.

- [ ] **Step 5: Prove provider-ID dispatch remains isolated**

Extend `TestGiteaExecutorDispatchesOnlyTrustedProviderID` with list and view requests and two recording adapters. Assert the resolved repository provider ID alone selects the adapter, and request fields cannot cross-route clients, authorities, or credentials. No production dispatcher change is expected; if the existing test passes unchanged, commit only the meaningful issue-operation assertions.

- [ ] **Step 6: Format, test, and commit lifecycle changes**

```bash
gofmt -w internal/rpcstatus/status.go internal/rpcstatus/status_test.go internal/server/gitea.go internal/server/gitea_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go
go test -race ./internal/rpcstatus ./internal/server ./internal/app ./internal/provider/gitea -run 'Test.*(Gitea|Issue|Status|ProviderLifecycle)' -count=1
git diff --check
git add internal/rpcstatus internal/server/gitea.go internal/server/gitea_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go
git commit -m "feat(gitea): authorize issue read operations"
```

---

### Task 7: Prove the complete restricted `tea` issue path

**Files:**
- Create: `integration/gitea_issues_test.go`
- Modify: `integration/gitea_repository_test.go`
- Modify: `integration/testdata/gitea-policy.yaml`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: packaged `tea`, TLS broker, digest-pinned `docker.gitea.com/gitea:1.27.2`, and shared integration helpers.
- Produces: one tagged end-to-end gate that demonstrates list/detail, multiple comment pages, authorization, audit, and leak boundaries.

- [ ] **Step 1: Refactor only reusable pinned-Gitea fixture setup**

Move container/network/certificate/user/token/repository/broker bootstrap from `TestRestrictedTeaRepositoryViewAgainstGitea` into a test-local fixture used by both repository and issue tests. Keep image digest, canonical owner/repository, TLS settings, cleanup, repository-view output, cancellation, and leak assertions unchanged. Add `issues:read` to the authorized policy grant.

- [ ] **Step 2: Seed representative issue data through operator HTTP setup**

Create one open issue with body, assignee, labels, milestone, deadline, and 51 ordered comments; create one closed issue; create one pull request so the shared index contains a non-issue. Use a setup token with only the scopes required to create fixture data. Record comment IDs and expected issue indices in the fixture; never pass the provider token to `tea` or its environment.

- [ ] **Step 3: Write the failing end-to-end demonstration**

Invoke the built client exactly as:

```bash
tea issues --repo CanonicalOwner/CanonicalRepo --state all --page 1 --limit 2 --fields index,title,state,comments --output json
tea issues INDEX --repo CanonicalOwner/CanonicalRepo --comments --output json
```

Assert exact typed JSON fields and values, provider issue order, omission of the pull request, open/closed filtering, an empty page beyond the end, canonical upstream owner/repository paths, and all 51 comments in increasing provider order. Directly verify that the paginated timeline endpoint returns 50 comments on page 1 and one on page 2, then use bounded Gitea request logging to assert the restricted client made those explicit timeline requests; do not infer pagination from returned content alone.

Also assert denied-repository output is empty with the generic diagnostic, authorized pull-request index gets only the fixed corrective diagnostic and no content/comments request, accepted/completed audit pairs use canonical operations, and token/body/filter/comment markers are absent from client environment, process arguments, diagnostics, broker stderr, and audit.

- [ ] **Step 4: Run the tagged integration test**

```bash
go test -tags=gitea_integration ./integration -run '^TestRestrictedTea(ReadOperations|RepositoryView)AgainstGitea$' -count=1 -v
```

Expected: PASS with Docker available; the issue test observes two explicit comment page requests and no pull request rendered as an issue.

- [ ] **Step 5: Update CI by direct verification**

Change the existing tagged Gitea read step to run the regex from Step 4. Do not add a test that parses workflow YAML. Verify the edited workflow through the same tagged command and final repository checks.

- [ ] **Step 6: Run final validation from the merged predecessor baseline**

Run the exact project gates selected from `.github/workflows/ci.yml` and the approved spec:

```bash
go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
REPOWOLF_GITEA_INTEGRATION=1 go test ./integration -run '^TestPinnedGiteaGitInteroperability$' -count=1 -v
go test -tags=gitea_integration ./integration -run '^TestRestrictedTea(ReadOperations|RepositoryView)AgainstGitea$' -count=1 -v
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
git diff --check
```

Expected: every command exits 0; formatting and diff checks print no output. If Docker or Nix is unavailable locally, record the exact unavailable gate rather than claiming it passed, and rely on CI for that environment-specific command.

- [ ] **Step 7: Commit the integration gate**

```bash
git add integration/gitea_issues_test.go integration/gitea_repository_test.go integration/testdata/gitea-policy.yaml .github/workflows/ci.yml
git commit -m "test(gitea): verify issue reads end to end"
```
