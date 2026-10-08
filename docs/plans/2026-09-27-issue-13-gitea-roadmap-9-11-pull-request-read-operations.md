# Gitea Pull-Request Read Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an authorized agent list one bounded page of Gitea pull requests and inspect one pull request with presence-aware booleans, optional ordinary comments, and complete bounded review hydration through the restricted `tea` client.

**Architecture:** Extend the existing provider-specific Gitea vertical slice with additive pull request protocol branches and focused parser, renderer, normalization, raw-presence, and review-pagination modules. Keep the SDK authoritative, wrap its already bounded HTTP response body only long enough to recover the two boolean presence bits, and reuse the existing repository authorization, issue-timeline comments, provider-ID dispatch, response bounds, and accepted/terminal audit lifecycle.

**Tech Stack:** Go 1.26, Protocol Buffers/gRPC, `gitea.dev/sdk` revision `492bc71e2f1d`, `buf`, Docker-backed Gitea 1.27.2 integration tests, and Nix.

**Spec:** `docs/specs/2026-09-27-issue-13-gitea-roadmap-9-11-pull-request-read-operations-design.md`

## Global Constraints

- Start from the completed issue-#12 baseline in this worktree; preserve repository view, issue reads/writes, GitHub, Git, packaging, and conditional Gitea service registration behavior.
- No `AGENTS.md` exists in this worktree. Use `.github/workflows/ci.yml`, `scripts/check-generated.sh`, and `scripts/check-release.sh` as validation authority.
- Keep `repowolf.v1` additive: retain request/result tags `10` through `17`, use fresh tags, give every new enum an `UNSPECIFIED` zero value, and generate Go only with `scripts/generate.sh`.
- Require `--repo`/`-r OWNER/REPO` for every pull command and exactly `pull_requests:read` after strict typed-request validation.
- Accept only the aliases, list/detail forms, flags, fields, defaults, and bounds in the approved spec. Do not add pull mutations, review actions, checkout, diff, patch, CI, repository inference, login state, prompts, editors, or public comment/review pagination flags.
- Preserve the existing maximum 64 arguments, 64-KiB argv, two-minute deadline, 8-MiB provider response, final Protobuf response, and rendered-output bounds; preserve cancellation, TLS 1.3, authority pinning, redirect rejection, token injection, concurrency, and no-retry behavior.
- List makes exactly one canonical `ListRepoPullRequests` call with explicit state/page/limit, returns at most `limit` records, preserves provider order, and returns an initialized empty result for a page past the end.
- View first makes exactly one canonical `GetIssue` call to validate shared-index kind, then one `GetPullRequest` call. A normal issue gets the safe corrective status before pull, review, or comment calls.
- Every valid detail view hydrates all reviews sequentially in pages of 50, accepts at most 1,000 reviews, and probes empty page 21 only after a full page 20. Review IDs are unique and strictly increasing in provider order.
- Ordinary comments are hydrated only with `--comments`, through the existing issue-timeline pages of 50, at most 1,000 timeline entries plus the completion probe, and must equal the pull's ordinary comment count. Review code comments are neither counted nor fetched.
- Recover only raw `mergeable` and `allow_maintainer_edit` presence from successful, already bounded SDK response bytes. Absent and JSON `null` become unset; present `false` remains present false. Collectors are request-scoped, concurrency-safe, and discard bytes after parsing.
- Validate complete provider records before projection or dependent hydration. Require canonical configured base repository casing; cross-repository heads are readable. Never return partial comments, reviews, or pull records.
- Audit only `gitea.pull_list` and `gitea.pull_view` plus existing bounded lifecycle metadata. Exclude selector/index/fields, pull/comment/review content, raw response data, pagination URLs, SDK/provider errors, authorities, and credentials.
- Do not introduce a provider registry, generic forge DTO, generic map/raw JSON protocol field, second production HTTP client, upstream `tea` runtime, retry loop, or pull mutation.

## Testing Value Gate

Every planned automated test covers public parsing/rendering, protocol presence, reusable provider normalization, raw-response presence recovery, pagination/order, authorization, anti-enumeration, sanitization, response limits, provider isolation, or the real client-to-Gitea path. Each test can fail for a meaningful production regression and benefits future maintainers. Do not add tests that merely inspect workflow YAML, dependency versions, generated source text, policy text, documentation text, or image declarations; verify those directly with `buf`, generation freshness, tagged integration execution, builds, Nix/OCI/release checks, and `git diff --check`.

## File Structure

### New files

- `internal/client/gitea/pull_fields.go`: pull field registry, defaults, and fixed detail order.
- `internal/client/gitea/pull_parse_test.go`: accepted aliases/defaults/flags and closed-grammar rejection tables.
- `internal/client/gitea/pull_render.go`: pull list/detail semantic validation and deterministic table/simple/JSON rendering.
- `internal/client/gitea/pull_render_test.go`: exact bytes, typed actor/review values, presence, ordering, bounds, and atomic rejection.
- `internal/provider/gitea/pull_presence.go`: request-scoped raw response collector and strict two-field JSON extraction/join.
- `internal/provider/gitea/pull_presence_test.go`: absent/null/false/true, malformed/mismatch, concurrency, cancellation, and response-bound tests.
- `internal/provider/gitea/pull_normalize.go`: complete SDK pull, actor, and review validation plus Protobuf projection.
- `internal/provider/gitea/pull_normalize_test.go`: malformed nesting, repository/branch, actor, review, and projection tests.
- `internal/provider/gitea/pull_list_test.go`: exact SDK options/call count, provider order, page limit, presence join, and failures.
- `internal/provider/gitea/pull_view_test.go`: kind-first flow, detail hydration order, optional comments, and no-partial-result behavior.
- `internal/provider/gitea/review_pagination.go`: bounded sequential review hydration.
- `internal/provider/gitea/review_pagination_test.go`: termination/probe/overflow/order/cancellation and actor/state validation.
- `internal/server/gitea_pull_test.go`: capability, anti-enumeration, kind correction, status, audit, and response-limit behavior.
- `integration/gitea_pulls_test.go`: pinned-Gitea list/detail demonstration with presence, comments, reviews, teams, denial, and audits.

### Modified files

- `proto/repowolf/v1/gitea.proto`: additive pull enums, requests, actors, reviews, records, and list/view branches.
- `gen/repowolf/v1/gitea.pb.go`, `gen/repowolf/v1/gitea_grpc.pb.go`: generated protocol output only.
- `internal/protocol/gitea_contract_test.go`: descriptor-level tags, enum values, oneofs, optional booleans, repeated structures, and closed-surface coverage.
- `internal/client/gitea/parse.go`, `fuzz_test.go`: dispatch pull aliases while retaining shared argv/repository/output bounds.
- `internal/client/gitea/render.go`, `client.go`, and focused tests: dispatch pull results and recognize the inverse safe kind correction after the provider defines its trusted status.
- `internal/provider/gitea/client.go`, `client_test.go`: install the response observer outside the existing authenticated/bounded transport without changing its authority.
- `internal/provider/gitea/adapter.go`, `adapter_test.go`: add narrow pull SDK methods and pull list/view dispatch.
- `internal/provider/gitea/comment_pagination.go`, `comment_pagination_test.go`: reuse the loop for pull ordinary-comment completeness without changing issue behavior.
- `internal/provider/gitea/operation.go`, `issue_mutation_operation_test.go`: validate pull requests, map `pull_requests:read`, and name operations.
- `internal/rpcstatus/status.go`, `status_test.go`: add the trusted inverse kind status while preserving issue correction.
- `internal/server/gitea_response_limit_test.go`: exact-limit/over-limit pull list and hydrated-view responses.
- `internal/app/gitea_executor_test.go`: pull request isolation across configured provider IDs.
- `integration/gitea_fixture_test.go`, `integration/testdata/gitea-policy.yaml`, `.github/workflows/ci.yml`: seed users/team/reviews and run the expanded tagged read gate.

---

### Task 1: Add the typed pull-request protocol and operation contract

**Files:**
- Modify: `proto/repowolf/v1/gitea.proto`
- Modify (generated): `gen/repowolf/v1/gitea.pb.go`
- Modify (generated): `gen/repowolf/v1/gitea_grpc.pb.go`
- Modify: `internal/protocol/gitea_contract_test.go`
- Modify: `internal/provider/gitea/operation.go`
- Modify: `internal/provider/gitea/issue_mutation_operation_test.go`

**Interfaces:**
- Consumes: existing request/result tags `10`-`17`, `GiteaCommentRecord`, `RequestContext`, and `ResponseMeta`.
- Produces: request/result branches `pull_list = 18` and `pull_view = 19`.
- Produces: `GiteaPullState`, `GiteaPullField`, `GiteaPullListRequest`, `GiteaPullViewRequest`, `GiteaReviewUser`, `GiteaReviewTeam`, `GiteaReviewActor`, `GiteaPullReviewState`, `GiteaPullReviewRecord`, `GiteaPullRecord`, and list/view results.
- Produces: `Capability == config.PullRequestsRead`, operation names `gitea.pull_list` and `gitea.pull_view`.

- [ ] **Step 1: Write failing descriptor and marshal tests**

Extend `internal/protocol/gitea_contract_test.go` using `ProtoReflect`, not source-text matching. Require tags `18`/`19`, exact enum ordering with zero `UNSPECIFIED`, the actor `oneof actor { user = 1; team = 2; }`, optional presence for both pull booleans and milestone, timestamp message kinds, ordered repeated comments/reviewers/reviews, and no map/raw JSON/token/endpoint/query/provider fields. Marshal and unmarshal records proving unset, present false, and present true remain distinct and proving user/team branches remain exclusive.

- [ ] **Step 2: Add the additive Protobuf surface**

Add these exact request and identity shapes, with `GiteaPullField` values in the spec's allowlist order (`INDEX=1` through `COMMENTS=18`):

```proto
enum GiteaPullState {
  GITEA_PULL_STATE_UNSPECIFIED = 0;
  GITEA_PULL_STATE_OPEN = 1;
  GITEA_PULL_STATE_CLOSED = 2;
  GITEA_PULL_STATE_ALL = 3;
}
message GiteaPullListRequest {
  GiteaPullState state = 1;
  int32 page = 2;
  int32 limit = 3;
  repeated GiteaPullField fields = 4;
}
message GiteaPullViewRequest {
  int64 index = 1;
  bool include_comments = 2;
}
message GiteaReviewUser { int64 id = 1; string login = 2; }
message GiteaReviewTeam { int64 id = 1; string name = 2; }
message GiteaReviewActor {
  oneof actor {
    GiteaReviewUser user = 1;
    GiteaReviewTeam team = 2;
  }
}
```

Define review-state values `APPROVED=1`, `PENDING=2`, `COMMENT=3`, `REQUEST_CHANGES=4`, and `REQUEST_REVIEW=5`. Define `GiteaPullReviewRecord` fields as `id`, `actor`, `state`, `body`, `commit_id`, `stale`, `official`, `dismissed`, `code_comment_count`, `submitted`, and `url`. Define `GiteaPullRecord` fields in this stable order: `index`, `state`, `author_id`, `author`, `url`, `title`, `body`, `draft`, optional `mergeable`, optional `allow_maintainer_edit`, `base`, `base_commit`, `head`, `created`, `updated`, `deadline`, `assignees`, optional `milestone`, `labels`, `comment_count`, `comments`, `requested_reviewers`, and `reviews`. Add list/view result wrappers and branches `18`/`19` to both top-level oneofs.

- [ ] **Step 3: Write failing operation validation tables**

Require explicit valid state, positive page/limit no greater than 50, nonempty unique fields in the closed enum range, positive view index, and a non-nil branch wrapper. Assert both operations require only `config.PullRequestsRead` and use the canonical audit names. Reject zero/unknown enum values, duplicate fields, nil wrappers, over-limit pages, and malformed oneofs before policy/provider work.

- [ ] **Step 4: Generate, implement validation, and verify compatibility**

Run:

```bash
scripts/generate.sh
gofmt -w internal/protocol/gitea_contract_test.go internal/provider/gitea/operation.go internal/provider/gitea/issue_mutation_operation_test.go
go test ./internal/protocol ./internal/provider/gitea -run 'TestGitea.*(Pull|Protocol|Operation|Validate)' -count=1
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
git diff --check
```

Expected: all commands exit 0; the protocol diff is additive and generated output is reproducible.

- [ ] **Step 5: Commit the protocol contract**

```bash
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go internal/protocol/gitea_contract_test.go internal/provider/gitea/operation.go internal/provider/gitea/issue_mutation_operation_test.go
git commit -m "feat(gitea): add pull read protocol"
```

---

### Task 2: Parse the closed pull list and detail grammar

**Files:**
- Create: `internal/client/gitea/pull_fields.go`
- Create: `internal/client/gitea/pull_parse_test.go`
- Modify: `internal/client/gitea/parse.go`
- Modify: `internal/client/gitea/fuzz_test.go`
- Modify: `internal/client/gitea/client.go`
- Modify: focused client parser/usage tests as required by compilation

**Interfaces:**
- Consumes: shared `validateArgv`, `parseSlug`, `parsePositive`, `parseOutput`, `requestFor`, and output formats.
- Produces: `parsePulls(args []string) (command, error)`, `parsePullList`, `parsePullView`, and `parsePullFields`.
- Produces: defaults `state=open,page=1,limit=30,format=table,fields=index,title,state,author,milestone,updated,labels`; detail defaults to `simple`.

- [ ] **Step 1: Write failing accepted-command tables**

Cover `pulls`, `pull`, and `pr`; implicit list; `list`/`ls`; detail index; `--repo`/`-r`; `--page`/`-p`; `--limit`/`--lm`; state; fields in caller order; `--output`/`-o`; and one boolean `--comments`. Include a typed assertion:

```go
parsed, err := Parse([]string{"pr", "list", "-r", "Owner/Repo", "--state", "all", "-p", "2", "--lm", "50", "--fields", "title,index,mergeable", "-o", "json"})
request := parsed.request.GetPullList()
// Assert state ALL, page 2, limit 50, requested field order, canonical selector,
// JSON format, and mutation == false.
```

Assert every alias produces the same typed branch and detail `--comments` affects only `include_comments`.

- [ ] **Step 2: Write failing rejection and no-dial tables**

Reject zero/negative/overflow indices/pages/limits, limit 51, empty/unknown/duplicate fields, duplicate long/short aliases, repeated `--comments`, `--flag=value`, misplaced subcommands, extra positionals, list flags on detail, comments on list, fields on detail, filters not in the contract, mutation/review/merge/checkout/diff/patch commands, invalid repository selectors, invalid UTF-8/NUL, 65 arguments, and argv over 64 KiB. Through `Run`, assert representative usage errors exit 2, write bounded static usage text, echo no attacker value, and make no dial/RPC call.

- [ ] **Step 3: Implement a focused pull parser and field registry**

Keep the 585-line `parse.go` from growing into another monolith: add only top-level dispatch there and place pull field descriptors/defaults in `pull_fields.go`; if parser implementation exceeds roughly 200 meaningful lines, place it in `pull_parse.go`. Use semantic duplicate keys so short/long aliases collide. Build only typed requests:

```go
func parsePulls(args []string) (command, error)
func parsePullList(args []string, start int) (command, error)
func parsePullView(args []string, index int64) (command, error)
func parsePullFields(value string) ([]repowolfv1.GiteaPullField, error)
```

Do not reuse issue fields or issue records. Update the usage constant to list bounded repository, issue, and pull accepted forms without embedding rejected input.

- [ ] **Step 4: Extend fuzz invariants**

Seed every alias and flag. For successful pull parses assert `providergitea.ValidateRequest`, a valid selector, non-`UNSPECIFIED` unique fields, bounded page/limit or positive detail index, no mutation bit, and no unexpected operation branch. Retain no-panic and non-echoing failure guarantees.

- [ ] **Step 5: Format, test, and commit parsing**

```bash
gofmt -w internal/client/gitea/parse.go internal/client/gitea/pull_fields.go internal/client/gitea/pull_parse*.go internal/client/gitea/pull_parse_test.go internal/client/gitea/fuzz_test.go internal/client/gitea/client.go
go test -race ./internal/client/gitea -run 'Test(ParsePull|ParseIssue|ParseRepository|Run.*Usage)|FuzzParse' -count=1
git diff --check
git add internal/client/gitea
git commit -m "feat(tea): parse pull read commands"
```

---

### Task 3: Render deterministic bounded pull output

**Files:**
- Create: `internal/client/gitea/pull_render.go`
- Create: `internal/client/gitea/pull_render_test.go`
- Modify: `internal/client/gitea/render.go`
- Modify: focused renderer tests as required

**Interfaces:**
- Consumes: Task 2's pull fields, existing `orderedField`, `marshalOrderedObject`, `joinText`, `cell`, `validTime`, and shared comment validation.
- Produces: `renderPullList(command, *GiteaPullListResult)`, `renderPullView(command, *GiteaPullViewResult)`, and semantic pull/actor/review validators.
- Preserves: complete-buffer-before-write and `maxRenderedBytes == 8<<20`.

- [ ] **Step 1: Write failing exact-byte list tests**

Assert headers/rows for table, space-separated simple rows, and ordered typed JSON objects. Cover selected field order, empty pages, provider order, typed numbers/booleans/arrays/timestamps, present false versus absent `mergeable`, empty text cells, JSON omission of absent optionals, and no hydrated comments/reviewers/reviews in list records. Require list identity/state validation even when not selected and reject more records than the request limit.

- [ ] **Step 2: Write failing detail and nested-value tests**

Use the fixed detail order from the spec. Assert JSON user actors as `{"user":{"id":7,"login":"alice"}}`, team actors as `{"team":{"id":9,"name":"reviewers"}}`, typed review objects in fixed field order, non-nil empty arrays, nested simple sections, compact JSON collections in table output, and body last in pull/comment/review text sections. Skip unrequested comments and absent deadline/booleans; always include reviews and requested reviewers.

Reject nil/wrong branches, empty request ID, malformed timestamps/order, invalid strings/IDs/counts/states, both/neither actor branches, duplicate/out-of-order review IDs, comments/reviews over 1,000, comments present when unrequested, incomplete requested comments, list hydration, and malformed optional presence. Reuse exact-limit/one-byte-over writers to prove no partial output.

- [ ] **Step 3: Implement focused pull rendering**

Keep pull behavior out of the already 421-line issue renderer. Define:

```go
func renderPullList(command, *repowolfv1.GiteaPullListResult) ([]byte, error)
func renderPullView(command, *repowolfv1.GiteaPullViewResult) ([]byte, error)
func validateProjectedPull(*repowolfv1.GiteaPullRecord, []repowolfv1.GiteaPullField) error
func validatePullRecord(*repowolfv1.GiteaPullRecord, bool) error
func pullFieldValue(*repowolfv1.GiteaPullRecord, repowolfv1.GiteaPullField) (any, bool, error)
func reviewActorFields(*repowolfv1.GiteaReviewActor) ([]orderedField, error)
func pullReviewFields(*repowolfv1.GiteaPullReviewRecord) ([]orderedField, error)
```

Encode ordered JSON without maps. Preserve provider assignee/label/reviewer/review/comment order. Render `head` exactly as normalized by the provider and lowercase public state strings.

- [ ] **Step 4: Dispatch only matching pull responses**

Add list/view branches in `render.go` only when the parsed request branch matches the response branch. Preserve repository, issue, and mutation rendering byte-for-byte; leave inverse kind diagnostics to Task 7 after Task 6 defines the trusted provider status.

- [ ] **Step 5: Format, test, and commit rendering**

```bash
gofmt -w internal/client/gitea/pull_render.go internal/client/gitea/pull_render_test.go internal/client/gitea/render.go
go test -race ./internal/client/gitea -count=1
git diff --check
git add internal/client/gitea
git commit -m "feat(tea): render pull read output"
```

---

### Task 4: Recover pull boolean presence through the bounded SDK transport

**Files:**
- Create: `internal/provider/gitea/pull_presence.go`
- Create: `internal/provider/gitea/pull_presence_test.go`
- Modify: `internal/provider/gitea/client.go`
- Modify: `internal/provider/gitea/client_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`

**Interfaces:**
- Consumes: SDK call contexts and the existing `errorResponseRedactingTransport` wrapped around `providerhttp`'s authenticated/bounded transport.
- Produces: request-scoped `pullPresenceCollector`, transport observer, and `map[int64]pullPresence` where each boolean is a pointer/presence pair.
- Produces: pull SDK seam methods that return SDK values and their parsed raw-presence evidence together.

- [ ] **Step 1: Write failing raw-presence extraction tests**

Feed bounded fake successful HTTP responses for one pull object and pull arrays. Cover absent, JSON `null`, false, true, mixed list values, whitespace, reordered keys, unknown unrelated fields, duplicate indices, missing/non-positive indices, duplicate JSON keys for observed fields, non-boolean values, malformed/trailing JSON, object/array shape mismatch, and count mismatch. Require no raw body retention after `finish()` and no inclusion of unrelated content in errors.

- [ ] **Step 2: Write failing transport and concurrency tests**

Prove only a context carrying a collector captures a successful response; unrelated repository/issue/review/comment calls are untouched. Prove two concurrent calls cannot cross-contaminate collectors. Cover cancellation, short reads, SDK decode failure, non-2xx redaction, exact 8-MiB success, one-byte-over provider failure, and body close errors. The existing bounded transport and its errors remain authoritative.

- [ ] **Step 3: Implement a narrow response observer**

Install the observer outside the `providerhttp.New` transport returned in `newClient`, so it sees bytes only as the SDK reads the already bounded body and does not alter authentication, authority, redirect, deadline, or size controls. Use an unexported context key and a teeing `io.ReadCloser`; never perform a second request or unbounded `io.ReadAll`.

```go
type pullPresence struct {
    mergeable          *bool
    allowMaintainerEdit *bool
}
type pullPresenceCollector struct { /* mutex, bounded buffer, terminal state */ }
func withPullPresenceCollector(context.Context) (context.Context, *pullPresenceCollector)
func (c *pullPresenceCollector) finish(expect pullPresenceShape) (map[int64]pullPresence, error)
```

Decode with a token-aware or `json.RawMessage` representation that explicitly rejects duplicate observed keys and distinguishes absent, null, false, and true. Clear the collector buffer on every terminal path.

- [ ] **Step 4: Extend only the pull SDK seam**

Add `pullRequestSDKClient` methods for `ListRepoPullRequests`, `GetPullRequest`, and `ListPullReviews`. In `sdkAPI`, attach a fresh collector only around list/get pull calls, parse after the SDK has consumed/closed the body, and return raw evidence with the SDK value. Reviews do not attach a collector. Keep existing `NewRepositoryAdapter(*sdk.Client)` and issue/repository SDK seams compatible.

- [ ] **Step 5: Format, test, and commit presence recovery**

```bash
gofmt -w internal/provider/gitea/pull_presence.go internal/provider/gitea/pull_presence_test.go internal/provider/gitea/client.go internal/provider/gitea/client_test.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go
go test -race ./internal/provider/gitea -run 'Test(PullPresence|PresenceTransport|NewClient|SDK.*Pull|RepositoryAdapter)' -count=1
git diff --check
git add internal/provider/gitea
git commit -m "feat(gitea): preserve pull boolean presence"
```

---

### Task 5: Normalize and list pull requests in provider order

**Files:**
- Create: `internal/provider/gitea/pull_normalize.go`
- Create: `internal/provider/gitea/pull_normalize_test.go`
- Create: `internal/provider/gitea/pull_list_test.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/adapter_test.go`

**Interfaces:**
- Consumes: `ListRepoPullRequests(ctx, owner, repo, sdk.ListPullRequestsOptions)` plus Task 4's presence map.
- Produces: `normalizedPull`, `normalizePull`, `projectPull`, `normalizeReviewActor`, and complete nested validation.
- Produces: one `GiteaPullListResult` preserving provider order and selected-field projection.

- [ ] **Step 1: Write failing normalization tests**

Cover positive pull/author IDs; open/closed state; non-negative ordinary comment count; nonempty valid author/URL/title/base/base commit/head; optional deadline; `updated >= created`; canonical base repository owner/name/full name; provider-order assignees/labels; milestone; requested user/team actors; and UTF-8/NUL rejection. Require `head` to use SDK `Base.Name`/`Head.Name` labels as supplied by Gitea and permit a noncanonical cross-repository head repository.

Reject nil nested values, duplicate assignee/label/actor identities, unknown state, malformed repository/branch/commit data, invalid timestamps, and raw/SDK boolean disagreement. Assert absent/null produces nil protocol optionals and present false/true produces non-nil matching pointers. List projection always retains index/state but sets only selected public fields and never detail-only draft/maintainer/reviewer/review/comment hydration.

- [ ] **Step 2: Write failing list adapter tests**

Assert exactly one call with canonical `Owner/Repo` and:

```go
sdk.ListPullRequestsOptions{
    ListOptions: sdk.ListOptions{Page: 2, PageSize: 50},
    State: sdk.StateAll,
}
```

Cover every state mapping, empty page, provider-order preservation, exact-limit success, `limit+1` rejection, nil/duplicate pull records, raw count/index/presence mismatch, malformed records, cancellation, sanitized provider error, and no retry. Assert fields are projected only after every SDK record and raw-presence entry validates, so failures return no partial result.

- [ ] **Step 3: Implement complete normalization before projection**

Use a private value carrying validated presence and copied slices:

```go
type normalizedPull struct {
    index, authorID, commentCount int64
    state repowolfv1.GiteaPullState
    author, url, title, body, base, baseCommit, head string
    draft bool
    mergeable, allowMaintainerEdit *bool
    created, updated time.Time
    deadline *time.Time
    assignees, labels []string
    milestone *string
    requestedReviewers []*repowolfv1.GiteaReviewActor
}
```

Do not reuse `normalizedIssue`; pull kind/branches/reviewers/presence are distinct responsibilities. Keep this module focused below split pressure, extracting review normalization into `review_pagination.go` if needed.

- [ ] **Step 4: Dispatch list with exact bounds**

Add the pull-list branch to `RepositoryAdapter.Execute`. Map typed state explicitly, make one SDK call, require `len(values) <= limit`, join raw entries by unique pull index, normalize all records before building the response, and preserve provider order. A provider error, malformed body, or mismatch returns only trusted context/provider/limit errors.

- [ ] **Step 5: Format, test, and commit pull listing**

```bash
gofmt -w internal/provider/gitea/pull_normalize.go internal/provider/gitea/pull_normalize_test.go internal/provider/gitea/pull_list_test.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go
go test -race ./internal/provider/gitea -run 'Test(PullList|NormalizePull|ProjectPull|RepositoryAdapter|SDK.*Pull)' -count=1
git diff --check
git add internal/provider/gitea
git commit -m "feat(gitea): list validated pull requests"
```

---

### Task 6: View pulls with bounded reviews and optional ordinary comments

**Files:**
- Create: `internal/provider/gitea/review_pagination.go`
- Create: `internal/provider/gitea/review_pagination_test.go`
- Create: `internal/provider/gitea/pull_view_test.go`
- Modify: `internal/provider/gitea/pull_normalize.go`
- Modify: `internal/provider/gitea/adapter.go`
- Modify: `internal/provider/gitea/comment_pagination.go`
- Modify: `internal/provider/gitea/comment_pagination_test.go`
- Modify: `internal/rpcstatus/status.go`
- Modify: `internal/rpcstatus/status_test.go`

**Interfaces:**
- Consumes: `GetIssue`, `GetPullRequest`, `ListPullReviews`, existing `ListIssueTimeline`, and Task 5 normalization.
- Produces: `loadPullReviews(ctx, api, owner, repo, index) ([]*GiteaPullReviewRecord, error)`.
- Produces: trusted `rpcstatus.ErrPullKind` with exact reason `GITEA_PULL_KIND_ISSUE`, domain `repowolf.dev/gitea`, and safe corrective message.
- Produces: a complete `GiteaPullViewResult` built only after pull, reviews, and requested comments all succeed.

- [ ] **Step 1: Write failing review normalization and pagination tests**

Cover empty/short/two-page review sets; user and team actors; all five review states; body/commit/stale/official/dismissed/code-count/submitted/URL mapping; exactly 1,000 reviews followed by empty page 21; overflow; page longer than 50; nil/malformed reviews; neither/both actor kinds; repeated/decreasing IDs within/across pages; cancellation before/between pages; provider failures; no retry; and nil result on every failure.

Use these constants and semantics:

```go
const (
    reviewPageSize = 50
    maximumReviewPages = 20
    maximumReviews = reviewPageSize * maximumReviewPages
)
```

A short page completes. A full page 20 triggers one empty page-21 probe; any page-21 record returns `runner.ErrOutputLimit` and none is appended.

- [ ] **Step 2: Write failing kind-first view tests**

Assert call order `GetIssue -> GetPullRequest -> ListPullReviews pages -> optional ListIssueTimeline pages`. A normal issue returns the trusted inverse kind error after only `GetIssue`. Cover issue/pull index mismatch, pull not found/provider failure, canonical base mismatch, raw presence mismatch, no timeline call by default, timeline only with `include_comments`, review hydration on every valid detail, ordinary count equality, and no result on any dependent failure.

- [ ] **Step 3: Implement actor/review normalization and bounded review loop**

Require exactly one positive-ID, nonempty valid user login or team name. Require positive unique increasing review IDs, known state, valid UTF-8/NUL-free body/commit/URL, non-negative code-comment count, and valid submitted timestamp. Copy actor/review values into non-nil slices. Check `ctx.Err()` before every page and classify provider errors without preserving SDK text.

- [ ] **Step 4: Generalize ordinary comment completion narrowly**

Refactor `loadIssueComments` only enough to accept an expected non-negative ordinary comment count or a narrow record interface, then call it from both issue and pull views. Preserve the existing timeline-entry page termination, typed-comment filtering, increasing comment IDs, 1,000-entry cap, empty overflow probe, and issue tests byte-for-byte. Do not count or fetch review code comments.

- [ ] **Step 5: Implement pull view without partial responses**

Add `rpcstatus.ErrPullKind` as a trusted structured `FailedPrecondition` status and exact tuple recognizer; reject provider-created lookalikes. Call `GetIssue` once and recognize only its trusted numeric 404 separately from sanitized error text. If `PullRequest == nil`, return `ErrPullKind`. Otherwise call `GetPullRequest` once, require matching requested index and canonical base repository, join raw presence, and fully normalize. Load reviews unconditionally; load ordinary comments only when requested. Attach all hydrated collections and construct the response only after every operation succeeds.

- [ ] **Step 6: Format, test, and commit pull detail**

```bash
gofmt -w internal/provider/gitea/review_pagination.go internal/provider/gitea/review_pagination_test.go internal/provider/gitea/pull_view_test.go internal/provider/gitea/pull_normalize.go internal/provider/gitea/adapter.go internal/provider/gitea/comment_pagination.go internal/provider/gitea/comment_pagination_test.go internal/rpcstatus/status.go internal/rpcstatus/status_test.go
go test -race ./internal/provider/gitea ./internal/rpcstatus -run 'Test(PullView|PullReview|ReviewPagination|CommentPagination|IssueView|.*PullKind)' -count=1
git diff --check
git add internal/provider/gitea internal/rpcstatus/status.go internal/rpcstatus/status_test.go
git commit -m "feat(gitea): hydrate pull request details"
```

---

### Task 7: Enforce pull authorization, kind status, audit, limits, and provider isolation

**Files:**
- Create: `internal/server/gitea_pull_test.go`
- Modify: `internal/server/gitea_response_limit_test.go`
- Modify: `internal/app/gitea_executor_test.go`
- Modify: `internal/client/gitea/client.go`
- Modify: `internal/client/gitea/client_test.go`
- Modify focused server tests as required

**Interfaces:**
- Consumes: existing `giteaService.Execute`, `providerLifecycle.Resolve/Complete`, trusted provider-ID executor, and Tasks 1/6 operation errors.
- Consumes: Task 6's `rpcstatus.ErrPullKind` exact trusted tuple.
- Produces: client handling for `index is an issue; use tea issues` before generic failure handling.
- Preserves: issue-kind status, anti-enumeration, accepted/terminal audit order, and final response-size enforcement.

- [ ] **Step 1: Write failing client kind-correction tests**

Feed `reportExecutionError` Task 6's exact trusted pull-kind status and require empty stdout, exit 1, and only `tea: index is an issue; use tea issues\n` on stderr. Require provider-created code/message/reason/domain/detail-count lookalikes to retain the generic diagnostic, and preserve the existing issue-to-pull correction unchanged.

- [ ] **Step 2: Write failing service authorization and audit tests**

For list/view, prove malformed requests fail before policy/provider work; valid requests require `pull_requests:read`; repository/issue-only grants, unknown repositories, unauthorized repositories, and wrong provider kinds are indistinguishable permission denial with zero adapter calls. Prove case-folded selectors resolve to canonical owner/name, accepted-audit failure prevents adapter use, and success/failure/cancellation produce the established lifecycle order.

Only an authorized exact-repository normal-issue index may return the inverse kind correction. Assert audits use `gitea.pull_list`/`gitea.pull_view` and contain none of marker-rich index/field/pull/comment/review/raw/provider-error/token values.

- [ ] **Step 3: Add pull response-limit and isolation coverage**

Construct exact-limit and one-byte-over list/detail responses, including aggregate comments, reviewers, reviews, and metadata. Require no oversized response delivery. Extend provider-ID dispatch tests with pull list/view requests and two recording adapters; prove only trusted resolved provider ID selects client/collector/authority/credential and request fields cannot cross-route them.

- [ ] **Step 4: Route the trusted inverse kind status without lifecycle forks**

In `client.go`, recognize Task 6's exact `IsPullKindStatus` tuple before generic failure handling and emit `tea: index is an issue; use tea issues\n`; never match a message substring. No server lifecycle fork should be needed: pull capability/operation from Task 1 must flow through the existing validation → selector → resolve/accepted audit → trusted executor → final-size/metadata → complete sequence. If production server changes become necessary, keep them branch-agnostic and prove all predecessor branches still pass.

- [ ] **Step 5: Format, test, and commit lifecycle behavior**

```bash
gofmt -w internal/server/gitea_pull_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go internal/client/gitea/client.go internal/client/gitea/client_test.go
go test -race ./internal/rpcstatus ./internal/server ./internal/app ./internal/client/gitea ./internal/provider/gitea -run 'Test.*(Gitea|Pull|IssueKind|Status|ProviderLifecycle|ResponseLimit)' -count=1
git diff --check
git add internal/server/gitea_pull_test.go internal/server/gitea_response_limit_test.go internal/app/gitea_executor_test.go internal/client/gitea/client.go internal/client/gitea/client_test.go
git commit -m "feat(gitea): authorize pull request reads"
```

---

### Task 8: Prove the complete restricted `tea pulls` path and run repository gates

**Files:**
- Create: `integration/gitea_pulls_test.go`
- Modify: `integration/gitea_fixture_test.go`
- Modify: `integration/testdata/gitea-policy.yaml`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: packaged `tea`, TLS broker, digest-pinned `docker.gitea.com/gitea:1.27.2`, setup-only token, shared audit assertions, and request-log counting.
- Produces: one tagged end-to-end proof of list/detail, presence, comments, reviews, user/team actors, authorization, audit, bounds, and leak resistance.

- [ ] **Step 1: Extend the reusable pinned-Gitea fixture**

Add a separate organization-owned `CanonicalOrg/PullRepo` fixture for pull tests so team review requests are real, while keeping the existing user-owned `CanonicalOwner/CanonicalRepo` repository, pinned image digest, TLS 1.3, setup-token isolation, cleanup, issue/repository tests, and failure-proxy behavior unchanged. Add a distinct policy repository entry/grant for `CanonicalOrg/PullRepo` with `pull_requests:read`; verify it through runtime authorization rather than a YAML text test.

- [ ] **Step 2: Seed representative bounded data**

Create one normal issue plus open and closed pull requests in `CanonicalOrg/PullRepo` with branch labels/base commits, labels/assignees/milestone/deadline, present false and unavailable/null boolean cases, 51 ordinary comments, 51 ordered reviews, one requested user, and one requested organization team. Use direct setup API calls only. Record issue/pull/review/comment IDs and query Gitea directly to establish expected raw presence and page sizes before invoking restricted `tea`.

- [ ] **Step 3: Write the shortest end-to-end demonstration**

Run exactly:

```text
tea pulls --repo CanonicalOrg/PullRepo --state all --page 1 --limit 2 --fields index,title,state,mergeable,base,head,comments --output json
tea pulls INDEX --repo CanonicalOrg/PullRepo --comments --output json
```

Assert exact typed values/key order, provider ordering, false-versus-unset presence, canonical base and stable head label, all 51 increasing ordinary comments and reviews, distinct user/team actor objects, non-nil empty collections where appropriate, an empty list page, and no mutation surface. Use bounded Gitea request logs to prove explicit pull page options, review pages 1/2, comment timeline pages 1/2, no review-code-comment calls, and no redundant timeline call for detail without `--comments`.

- [ ] **Step 4: Prove denial, kind correction, audit, and leak boundaries**

Assert denied repository output is empty with the generic diagnostic and zero provider call. Assert an authorized normal issue viewed through `tea pulls` emits only `tea: index is an issue; use tea issues`, after one issue-kind request and before pull/review/comment requests. Assert accepted/completed or accepted/failed audit pairs use canonical pull operations. Search sandbox environment, process arguments, diagnostics, broker stderr, and audit for token, pull body/title, comment/review bodies, actor markers, raw presence data, and provider errors.

- [ ] **Step 5: Run the tagged integration tests and update CI directly**

Update the existing Gitea read step regex to include pull tests; do not add a test that parses workflow YAML. Run:

```bash
go test -race -tags=gitea_integration ./integration -run '^TestRestrictedTea(Pull|ReadOperations|RepositoryView)AgainstGitea$' -count=1 -v
```

Expected: PASS with Docker available, including explicit two-page comment/review evidence and predecessor repository/issue behavior.

- [ ] **Step 6: Run generated, static, full Go, Nix, OCI, and release gates**

Run the exact project/spec gates:

```bash
go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
REPOWOLF_GITEA_INTEGRATION=1 go test ./integration -run '^TestPinnedGiteaGitInteroperability$' -count=1 -v
go test -race -tags=gitea_integration ./integration -run '^TestRestrictedTea(Pull|ReadOperations|RepositoryView)AgainstGitea$' -count=1 -v
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
git diff --check
```

Expected: every command exits 0 and formatting/diff checks print no output. If Docker or Nix is unavailable locally, record the exact unrun environment-specific gate instead of claiming success and rely on CI for that gate.

- [ ] **Step 7: Check scope and commit the integration proof**

```bash
git status --short
git diff --check
git add integration/gitea_pulls_test.go integration/gitea_fixture_test.go integration/testdata/gitea-policy.yaml .github/workflows/ci.yml
git commit -m "test(gitea): prove pull request reads end to end"
git status --short
```

Expected: a clean worktree with eight scoped Conventional Commit implementation commits and no pull mutation, generic forge abstraction, unbounded read, credential artifact, or unrelated refactor.
