# Gitea pull-request read operations design

**Date:** 2026-09-27

**Status:** Approved for implementation planning

## Context

The completed Gitea repository and issue slices provide the restricted `tea` personality, typed `GiteaService`, provider-ID dispatch, strict repository authorization, deterministic rendering, bounded issue-comment pagination, and the shared accepted/terminal audit lifecycle. Pull-request commands are still rejected.

This issue extends that existing vertical slice so an agent with `pull_requests:read` can list and inspect pull requests, including ordinary comments and complete bounded review history. It adds no mutation.

The cross-cutting source of truth remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-04, ID-06, ID-07, and ID-08 without changing them.

## Outcome

An authorized agent can:

- list one explicit page of pull requests in a configured Gitea repository;
- select and order the approved list fields;
- inspect one pull request;
- optionally retrieve all ordinary comments within the existing bound; and
- inspect all reviews and requested review actors within a documented bound.

All operations use typed messages, canonical configured repository coordinates, the adapter selected by trusted provider ID, sanitized errors, bounded provider and Protobuf responses, and the existing audit lifecycle.

## Approaches considered

### Extend the existing Gitea vertical slice

This is the selected approach. Add pull-request protocol branches and records, parser/renderer units beside the issue units, and focused adapter normalization, raw-presence, comment, and review-pagination helpers. Reuse the established service lifecycle and issue timeline pagination behavior.

This keeps provider-specific semantics explicit and gives later pull-request writes a typed foundation without introducing a generic forge model.

### Represent pull requests as issues

Gitea shares issue indices, comments, and several fields with pull requests. Reusing `GiteaIssueRecord` would reduce code, but it cannot safely express branches, reviews, draft state, or boolean presence and would weaken kind validation. This is rejected.

### Bypass the SDK for pull reads

Direct HTTP decoding would make raw boolean presence easy to recover, but would create a second production API client and duplicate transport behavior. This is rejected. The SDK remains authoritative; a narrow Gitea transport observer recovers only the two raw presence bits that the SDK's non-pointer booleans discard.

## Existing behavior and affected components

The current implementation has these relevant seams:

- `proto/repowolf/v1/gitea.proto` contains repository and issue operations only.
- `internal/client/gitea` strictly parses and renders repository and issue commands.
- `internal/provider/gitea.RepositoryAdapter` wraps repository and issue SDK services behind one provider-specific executor.
- `internal/provider/gitea/comment_pagination.go` retrieves issue timeline pages of 50, accepts at most 1,000 entries, and verifies ordinary comment completeness against the provider count.
- `internal/server/gitea.go` validates, authorizes, audits, dispatches, and bounds every Gitea response.
- `internal/app/gitea_executor.go` routes only by the trusted provider ID.
- The digest-pinned Gitea 1.27.2 fixture exercises the full restricted-client path.

Affected components are the Gitea protocol and generated code, client parser and renderer, adapter SDK seams and normalization, Gitea-specific response observation, status mapping, server/response-limit tests, and the real Gitea fixture. Shared lifecycle and policy architecture do not need another abstraction.

## Restricted `tea` command contract

Every command requires `--repo`/`-r OWNER/REPO`. Existing argv, UTF-8, NUL, timeout, diagnostic, and rendered-output bounds remain unchanged.

### List commands

Accepted forms are:

```text
tea pulls --repo OWNER/REPO [list flags]
tea pull  --repo OWNER/REPO [list flags]
tea pr    --repo OWNER/REPO [list flags]
tea pulls list --repo OWNER/REPO [list flags]
tea pulls ls   --repo OWNER/REPO [list flags]
```

`pulls`, `pull`, and `pr` are aliases. No positional index or subcommand means `pulls list`; `list` and `ls` are aliases.

List flags are:

- `--state all|open|closed`, default `open`;
- `--page`/`-p`, default `1`;
- `--limit`/`--lm`, default `30`, maximum `50`;
- `--fields <comma-separated-fields>`; and
- `--output`/`-o table|simple|json`, default `table`.

The list field allowlist is:

```text
index, state, author, author-id, url, title, body, mergeable, base,
base-commit, head, created, updated, deadline, assignees, milestone,
labels, comments
```

The default fields are `index,title,state,author,milestone,updated,labels`. Explicit fields must be nonempty, unique, approved, and retain caller order. Page and limit must be positive. Unlisted filters, extra positionals, `--flag=value`, misplaced subcommands, and long/short duplicates are rejected.

### Detail commands

Accepted forms are:

```text
tea pulls INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
tea pull  INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
tea pr    INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
```

Detail defaults to `simple`. `--comments` is a boolean accepted once. Detail rejects fields, list pagination, and list filters.

Reviews and requested review actors are always hydrated for detail. `--comments` controls only ordinary conversation comments. Without it, the adapter makes no timeline call. This preserves the approved public command surface without inventing review or comment pagination flags.

Usage errors exit 2 with a bounded accepted-forms diagnostic that does not echo rejected input. Connection, RPC, provider, validation, rendering, and write failures exit 1 with stable sanitized diagnostics. Existing cancellation and signal behavior remains unchanged.

## Protocol and data contract

Extend `GiteaRequest.operation` and `GiteaResponse.result` additively with `pull_list` and `pull_view` branches using fresh tags.

### Requests

`GiteaPullListRequest` contains a required pull-state enum (`OPEN`, `CLOSED`, or `ALL`), positive page and limit, and repeated pull-field enum values in requested order. `UNSPECIFIED` is invalid at the server boundary; the client sends the default explicitly.

`GiteaPullViewRequest` contains a positive `int64` index and `include_comments`. Repository selection remains only in `RequestContext`.

### Pull records

`GiteaPullRecord` contains:

- positive `index` and `author_id`;
- required pull state;
- `author`, `url`, `title`, and `body`;
- `draft`;
- optional `mergeable` and optional `allow_maintainer_edit` booleans;
- `base`, `base_commit`, and `head` strings;
- required `created` and `updated` timestamps and optional `deadline`;
- repeated assignee and label names and optional milestone title;
- non-negative ordinary `comment_count` and optional hydrated comments;
- repeated requested-review actors; and
- repeated hydrated reviews.

`base` is the base branch name, `base_commit` is its commit ID, and `head` uses Gitea's stable label (`owner:branch` when an owner is present). The configured repository must be the pull request's base repository. Cross-repository heads may be read but are not created or modified by this issue.

List responses project only selected fields plus the identity/state required for client semantic validation. They never contain hydrated comments, requested-review actors, or reviews. The `comments` list field is the ordinary comment count. `allow_maintainer_edit`, `draft`, requested reviewers, and reviews are detail-only.

### Review actors and reviews

`GiteaReviewActor` has exactly one typed actor branch:

- user: positive ID and nonempty login; or
- team: positive ID and nonempty name.

The same actor type represents requested reviewers and review authors. A record with neither or both branches is invalid.

`GiteaPullReviewRecord` contains positive ID, one actor, required review-state enum (`APPROVED`, `PENDING`, `COMMENT`, `REQUEST_CHANGES`, or `REQUEST_REVIEW`), body, commit ID, stale/official/dismissed booleans, non-negative code-comment count, required submitted timestamp, and URL. Review code-comment bodies are not hydrated.

The view result always uses non-nil repeated collections. All new enum zero values are `UNSPECIFIED`. No SDK object, raw JSON, token, provider endpoint, arbitrary query, or generic map enters the protocol. Generated files change only through `scripts/generate.sh`.

## Presence recovery

The pinned SDK represents `mergeable` and `allow_maintainer_edit` as plain booleans, collapsing absent, JSON `null`, and `false`. The protocol must preserve provider presence.

Add a Gitea-specific response observer around the existing bounded transport. Pull list/view SDK calls attach a per-call collector through context. The observer copies only the already-bounded successful response bytes while the SDK reads them, then extracts each pull number and the raw presence/value of exactly `mergeable` and `allow_maintainer_edit`. It does not retain bodies after the call.

The adapter joins observed presence to SDK records by unique pull index and rejects malformed JSON, non-boolean non-null values, missing/duplicate indices, count mismatches, or disagreement between a present raw value and the SDK value. JSON `null` and an absent key both become unset protocol optionals; present `false` remains present false. The collector is request-scoped and safe for concurrent calls. Existing authority, token, TLS, redirect, deadline, and 8 MiB response accounting remain authoritative and unchanged.

## Rendering contract

List rendering follows the established issue contract:

- `table`: selected headers and tab-separated rows;
- `simple`: selected space-separated values, one row per pull; and
- `json`: a top-level array of flat objects with exactly the selected keys in selected order.

Numbers, booleans, arrays, and timestamps remain typed in JSON. An absent `mergeable`, milestone, or deadline is omitted from each JSON object and rendered as an empty text cell/value. Provider pull, assignee, and label order is preserved.

Detail uses this fixed order:

```text
index, state, draft, author, author-id, url, title, mergeable,
allow-maintainer-edit, base, base-commit, head, created, updated,
deadline, assignees, milestone, labels, comments, requested-reviewers,
reviews, body
```

Unset optionals and unrequested comments are skipped. JSON emits typed actor and review objects and non-nil arrays. Simple output uses labeled lines; comments and reviews use nested fixed-order sections, and multiline bodies remain last within their containing record. Table emits one pull row and compact JSON for hydrated collections.

Before rendering, the client validates the request ID, matching result branch, all semantic invariants, comment and review bounds, and ordering. It prepares the complete output before writing and retains the 8 MiB rendered-output limit.

## Request and provider flow

```text
restricted `tea pulls`
  -> strict parser and local bounds
  -> typed GiteaRequest over authenticated TLS gRPC
  -> server operation/selector/field validation
  -> shared lifecycle resolves exact pull_requests:read grant
  -> trusted provider ID selects one immutable Gitea adapter
  -> canonical owner/name call the SDK through bounded transport
  -> raw observer restores two boolean presence bits
  -> adapter validates, hydrates detail reviews, and optionally loads comments
  -> projected typed response
  -> shared metadata and final Protobuf response limit
  -> client semantic validation and deterministic rendering
```

List makes exactly one `ListRepoPullRequests` call with canonical owner/name, explicit state, page, and limit. A page past the end succeeds with an empty result. More records than the requested limit, nil entries, duplicate indices, malformed values, or raw/SDK mismatch fail the whole operation. Provider order is not sorted.

View first calls `GetIssue` for the authorized repository and index to validate the shared-index kind. A normal issue produces a structured corrective error equivalent to `index is an issue; use tea issues`; a pull marker permits one `GetPullRequest` call. The adapter then validates the complete pull and requires matching indices and canonical base repository identity before any dependent hydration.

After pull validation, view retrieves every review sequentially with `ListPullReviews`, page size 50. It accepts at most 20 full pages (1,000 reviews) and uses an empty page-21 probe after a full twentieth page to distinguish exact completion from truncation. A short page ends retrieval. Review IDs must be unique and strictly increasing in provider order. Any malformed review, actor, page, cancellation, or overflow fails without a partial response or retry.

When `include_comments` is true, view reuses explicit issue-timeline pagination: pages of 50, no more than 1,000 timeline entries, an overflow probe after page 20, retaining only typed ordinary comments, strict increasing comment IDs, and equality with the pull's ordinary comment count. Review code comments do not count as ordinary comments and are not fetched. Hydrated collections are attached only after every requested call succeeds.

Pull normalization requires valid UTF-8 without NUL; positive IDs; known states; non-negative counts; nonempty title, author, URL, branches, and commit IDs; canonical base repository owner/name; valid timestamps with `updated >= created`; valid optional deadline; and valid nested assignees, labels, milestone, actors, comments, and reviews. Empty bodies remain valid.

## Authorization, kind handling, and errors

Both operations require exactly `pull_requests:read`. Repository resolution, provider-kind enforcement, accepted-audit failure behavior, final limits, and terminal metadata reuse the existing lifecycle.

Unknown repositories, unauthorized repositories, missing capability, and provider-kind mismatch remain indistinguishable: permission denied, no provider call, and no trusted provider/repository metadata before resolution. Client input cannot select a provider ID, authority, credential, or raw endpoint.

Kind correction occurs only after exact repository authorization. A pull view of a normal issue fails with the safe corrective diagnostic and does not fetch pull detail, reviews, or comments. Issue commands retain their existing inverse correction. Provider not-found remains sanitized not-found.

Malformed requests fail before policy or provider work. Raw SDK errors, HTTP bodies, pull content, filters, review/comment content, and credentials never enter client diagnostics or audit. Cancellation, deadlines, provider failure, and response limits return established sanitized statuses. No read path retries automatically.

Accepted audit operations are `gitea.pull_list` and `gitea.pull_view`. Audit records contain only existing bounded lifecycle metadata; they exclude indices, selected fields, pull/comment/review content, pagination URLs, raw presence data, SDK errors, and tokens.

## Compatibility constraints

- Existing repository and issue behavior, GitHub behavior, and Git behavior remain unchanged.
- `GiteaService` registration and provider-ID dispatch remain unchanged; no registry is introduced.
- Existing transport security, two-minute deadline, 8 MiB provider-response bound, final Protobuf bound, concurrency, and audit contracts remain unchanged.
- Repository selectors remain case-insensitive against configuration; provider calls and base-repository validation use canonical configured casing.
- The command surface remains a documented subset of `tea 0.15.1`; unsupported pull mutations, review actions, checkout, merge, diff, patch, CI, inference, login state, and interactivity remain rejected.
- Protocol and JSON changes are additive within `v1`. No production artifact contains upstream `tea`.

## Verification strategy

These tests pass the Patchmill Testing Value Gate because they cover public parser/renderer contracts, protocol presence, provider normalization, pagination, ordering, authorization, anti-enumeration, response limits, and credential boundaries.

### Protocol, parser, and renderer

- Descriptor tests lock fresh branch tags, enum values, actor oneofs, optional boolean presence, field types, and repeated review/comment structure; marshal tests distinguish unset, present false, and present true.
- Parser tables cover aliases, implicit/explicit list, detail indices, defaults, state/page/limit/fields, `--comments`, output formats, duplicate flags, numeric/argv bounds, and representative rejected commands.
- Parser fuzzing proves arbitrary argv cannot panic, escape allowlists, or echo attacker input in diagnostics.
- Renderer tests prove exact table/simple/JSON bytes, selected order, typed booleans, optional omission, empty pages/arrays, nested user/team actors, reviews/comments, branch mismatch rejection, exact-limit success, over-limit rejection, and no partial writes.

### Adapter, transport, server, and composition

- Presence-observer tests use bounded fake HTTP responses for absent, null, false, and true booleans; list/object joins; malformed or mismatched data; concurrent calls; cancellation; and existing response-limit/error redaction behavior.
- List adapter tests assert one canonical SDK call, exact options, provider-order preservation, projection, empty pages, and rejection of oversized pages, nil/duplicate records, bad repositories, malformed nesting, and presence mismatch.
- View tests assert kind validation before pull retrieval, one pull call, review hydration on every valid detail, no timeline call by default, timeline calls only with `--comments`, complete mapping, no retry, and no partial result.
- Review pagination tests cover empty/short/two-page results, user and team actors, exactly 1,000 reviews plus an empty overflow probe, overflow, repeated/out-of-order IDs, malformed states, and cancellation between pages.
- Comment tests reuse and extend the established timeline cases for pull comment counts and mixed non-comment events. Server tests enforce `pull_requests:read`, anti-enumeration, safe kind correction, operation names, audit order, and aggregate response limits.
- Application tests prove multiple Gitea providers cannot cross-route clients, raw collectors, authorities, or credentials.

### Real Gitea integration

Extend the digest-pinned Gitea 1.27.2 fixture with open and closed pull requests, optional fields, user and team review actors, more than one review page, and more than one ordinary-comment page. Through the packaged `tea` personality and TLS broker, demonstrate:

```text
tea pulls --repo OWNER/REPO --state all --page 1 --limit 2 \
  --fields index,title,state,mergeable,base,head,comments --output json
tea pulls INDEX --repo OWNER/REPO --comments --output json
```

Assert exact typed output, false-versus-unset boolean presence, canonical base repository, ordered complete comments and reviews, user/team actors, bounded explicit page requests, an empty list page, kind correction, denied-repository behavior, accepted/completed audits, and absence of credentials and collaboration content from sandbox environment, process arguments, diagnostics, and audit.

Existing Gitea repository/issue, GitHub, Git, race, fuzz, integration, generated-code, Nix/package, OCI, and release checks remain regression gates. Static documentation or packaging declarations receive direct verification rather than new text-content tests. Final verification includes `go vet`, `go test -race`, `buf lint`, breaking/generated checks, and `git diff --check`.

## Non-goals

This issue does not add:

- pull-request create, edit, comment, close, reopen, review, merge, checkout, or any mutation;
- public comment/review list commands or pagination flags;
- review code-comment hydration, diff, patch, files, commits, CI/statuses, or merge operations;
- issue behavior changes beyond the inverse safe kind diagnostic;
- arbitrary API access, raw JSON forwarding, repository inference, prompts, editors, stdin bodies, or login state;
- retries, provider health checks, diagnostics, metrics, reload, broad version certification, or an upstream `tea` runtime dependency; or
- a provider registry, generic forge DTO, or provider-neutral CLI.

## Acceptance criteria

1. The restricted `tea` personality accepts only the documented pull list/detail aliases, flags, fields, defaults, and bounds.
2. Additive typed protocol branches represent pull lists/views, optional booleans, comments, requested reviewers, reviews, and user/team actors without raw or generic fields.
3. A granted list performs one canonical paginated SDK request and returns at most the requested page in provider order.
4. A granted view validates pull kind, returns one complete pull, always hydrates all bounded reviews, and retrieves ordinary comments only with `--comments`.
5. `mergeable` and `allow_maintainer_edit` preserve absent/null versus present false/true despite the SDK's plain booleans.
6. Review and comment hydration is sequential, ordered, cancellation-aware, capped at 1,000 provider entries each, and fails rather than truncating or returning partial data.
7. User and team actors are represented distinctly and validated in requested-review and review records.
8. Unauthorized, wrong-provider, wrong-kind, malformed, provider-failed, cancelled, and over-limit requests expose no unintended data, make no unintended call, and return sanitized errors.
9. Table, simple, and JSON output is deterministic, typed, presence-aware, non-TTY-dependent, and bounded to 8 MiB.
10. Audit events use canonical pull operation names and contain no selector, pull, comment, review, raw response, provider error, or secret content.
11. Unit, fuzz, service, response-limit, and one pinned-Gitea integration test prove ordering, presence, pagination, anti-enumeration, provider isolation, and the complete client-to-provider demonstration.
12. Existing Gitea repository/issue, GitHub, Git, security, generated-code, race, integration, Nix, OCI, and release checks remain green, and no pull mutation is added.

## Open questions

None.
