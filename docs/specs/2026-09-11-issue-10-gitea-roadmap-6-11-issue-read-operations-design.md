# Gitea issue read operations design

**Date:** 2026-09-11

**Status:** Approved for implementation planning

## Context

Issue #9 establishes the first complete Gitea API path: a restricted `tea` personality, typed `GiteaService`, provider-ID dispatch, SDK-native adapter, shared provider lifecycle, deterministic rendering, and one real Gitea integration fixture. The protocol currently contains only repository view.

This issue extends that path so an agent with `issues:read` can list and inspect issues, optionally including all comments within a documented bound. It adds no mutation.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-04, ID-06, ID-07, and ID-08 without changing them. Its implementation baseline includes the completed issue-#9 branch even if this specification branch predates that merge.

## Outcome

An agent can use the restricted `tea` client to:

- list one explicit page of issues in an authorized configured Gitea repository;
- select and order approved list fields;
- inspect one issue; and
- request comments on that issue, with the adapter retrieving explicit provider pages rather than relying on embedded or unbounded data.

Every operation uses typed gRPC messages, the trusted repository selected by policy, the matching provider instance, sanitized errors, bounded responses, and the existing accepted/terminal audit lifecycle. Pull requests are never returned as issues even though Gitea shares their index space.

## Approaches considered

### Extend the issue-#9 vertical slice with issue-specific types and helpers

This is the selected approach. Add issue branches to the existing Gitea protocol and service, issue methods to the SDK adapter, issue grammar and rendering to the Gitea client, and focused normalization/pagination units inside `internal/provider/gitea`.

It reuses proven transport, lifecycle, dispatch, and output primitives without introducing a generic forge model.

### Model issues and pull requests together

Gitea exposes both through a shared issue index and parts of a shared API model. A combined abstraction would reduce some code, but it would weaken kind validation and add pull-request behavior before the later pull-request read issue defines its local contract. This is rejected.

### Return raw SDK records and render on the client

This would reduce protocol work, but it would expose provider-version drift and fields outside the approved surface. It is rejected in favor of validated, projected Protobuf records.

## Existing behavior and affected components

The issue-#9 baseline has these relevant seams:

- `proto/repowolf/v1/gitea.proto` has one `repository_view` request/result branch and repository record.
- `internal/client/gitea` parses and renders only repository detail with `--repo` and `--output`.
- `internal/provider/gitea.RepositoryAdapter` executes one typed request through a narrow SDK seam.
- `internal/server/gitea.go` validates Gitea requests, maps capability and operation, resolves an exact repository, and applies shared lifecycle limits and audit metadata.
- `internal/app/gitea_executor.go` selects the adapter strictly from the trusted provider ID.
- `integration/gitea_repository_test.go` and the pinned Gitea fixture exercise the full client-to-provider path.

Affected areas are the Gitea Protobuf and generated code, Gitea client parser and renderer, Gitea provider operation validation and adapter, test SDK seams, server and response-limit tests, and the Gitea integration fixture. Shared lifecycle and provider HTTP behavior should not need a new abstraction.

## Restricted `tea` command contract

All commands still require `--repo`/`-r <owner>/<name>`. Output never depends on a TTY. Existing client-wide argv, UTF-8, NUL, timeout, diagnostic, and rendered-output bounds remain in force.

### List commands

The accepted list forms are:

```text
tea issues --repo OWNER/REPO [list flags]
tea issue  --repo OWNER/REPO [list flags]
tea i      --repo OWNER/REPO [list flags]
tea issues list --repo OWNER/REPO [list flags]
tea issues ls   --repo OWNER/REPO [list flags]
```

`issues`, `issue`, and `i` are aliases. With no positional index or subcommand they mean `issues list`. `list` and `ls` are aliases.

List flags are:

- `--state all|open|closed`, default `open`;
- `--keyword`/`-k`;
- `--author`/`-A`;
- `--assignee`/`-a`;
- `--mentions`/`-M`;
- `--from`/`-F` and `--until`/`-u`, each an RFC 3339 timestamp;
- `--owner`/`--org`, equivalent aliases for the configured repository owner filter;
- `--page`/`-p`, default `1`;
- `--limit`/`--lm`, default `30` and maximum `50`;
- `--fields <comma-separated-fields>`; and
- `--output`/`-o table|simple|json`, default `table`.

A page and limit must be positive. An owner filter must case-insensitively match the explicit repository owner; a mismatch succeeds with an empty page and makes no provider request. This preserves the filter meaning without allowing a repository-scoped grant to become repository enumeration. `from` later than `until` is invalid.

The issue field allowlist is:

```text
index, state, author, author-id, url, title, body, created, updated,
deadline, assignees, milestone, labels, comments, repo, owner, kind
```

`kind` is always `issue`. If `--fields` is absent, the fields are `index,title,state,author,milestone,labels,owner,repo`. If present, `--fields` must contain one or more unique approved names, replaces the defaults, and preserves caller order. Unknown, empty, or duplicate names are usage errors. `--kind`/`-K` is rejected; issue lists cannot request pull requests.

Filter strings are bounded to 255 UTF-8 bytes without NUL. Issue indices fit a positive signed 64-bit integer. Long and short forms of the same flag count as duplicates. `--flag=value`, unlisted flags, extra positionals, and misplaced subcommands are rejected.

### Detail commands

The accepted detail forms are:

```text
tea issues INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
tea issue  INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
tea i      INDEX --repo OWNER/REPO [--comments] [--output table|simple|json]
```

The default detail output is `simple`. `--comments` is a boolean flag accepted at most once. Detail does not accept `--fields`, list filters, `--page`, or `--limit`.

Without `--comments`, the adapter makes no comments call. With `--comments`, it retrieves comment pages explicitly as described below and returns the complete bounded set. This preserves the approved `tea 0.15.1` command surface; comment pagination is provider-side and does not invent a public comment-list command or page flag.

Usage errors exit 2 with a bounded diagnostic naming the accepted forms and values without echoing rejected input. Connection, RPC, provider, response-validation, rendering, and output-write failures exit 1 with stable sanitized diagnostics. Existing cancellation and signal exit behavior remains unchanged.

## Protocol and data contract

Extend `GiteaRequest.operation` and `GiteaResponse.result` additively with `issue_list` and `issue_view` branches.

### Requests

`GiteaIssueListRequest` contains:

- state enum with `UNSPECIFIED`, `OPEN`, `CLOSED`, and `ALL`;
- optional keyword, author, assignee, mentions, from, until, and owner filters;
- positive page and limit; and
- repeated issue-field enum values in requested output order.

`UNSPECIFIED` state is invalid at the server boundary; the client writes the default explicitly. Filter presence is retained so unset remains distinct from an explicitly empty value, which is rejected. Times use Protobuf timestamps.

`GiteaIssueViewRequest` contains a positive `int64` index and `include_comments`. The shared `RequestContext` carries only the explicit repository selector, as in issue #9.

### Records

`GiteaIssueRecord` exposes the approved data with typed fields:

- `index` and `author_id`: positive `int64`;
- `state`: issue-state enum;
- `kind`: issue-kind enum, required to be `ISSUE`;
- `author`, `url`, `title`, `body`, `repo`, and `owner`: strings;
- `created` and `updated`: required timestamps;
- `deadline`: optional timestamp;
- `assignees` and `labels`: repeated strings;
- `milestone`: optional string title;
- `comment_count`: non-negative `int64`; and
- `comments`: repeated `GiteaCommentRecord`, populated only by issue view with `include_comments`.

`GiteaCommentRecord` contains positive `id` and `author_id`, plus `author`, `url`, `body`, `created`, and `updated`. Comment timestamps are required.

The list result is a repeated issue field with no pagination envelope. The view result contains one issue. List records carry no hydrated comments; the selected `comments` output field renders `comment_count`. Detail JSON renders `comments` as an array when requested and omits the key otherwise.

List response projection follows requested fields: the adapter validates the complete SDK record but only populates fields needed for the selected output. Identity and kind values needed for semantic validation do not thereby become rendered output. This keeps Protobuf responses smaller when callers omit heavy fields such as `body`.

All new tags are fresh, enum zero values are `UNSPECIFIED`, and generated files change only through `scripts/generate.sh`. No raw SDK object, raw JSON, provider URL, token, arbitrary query, or generic key/value field enters the protocol.

## Rendering contract

### Lists

- `table` emits one tab-separated header row using selected field names and one row per issue.
- `simple` emits one space-separated row per issue with no header, matching the selected order.
- `json` emits a top-level array of flat objects with exactly the selected field names in selected order, followed by one newline.

JSON values are typed: indices and comment counts are numbers, assignees and labels are arrays, and times are RFC 3339 strings. `kind` and `state` render as lowercase strings. Table/simple arrays are comma-and-space joined. Control characters in table/simple strings become spaces; JSON uses standard escaping. An absent milestone or deadline is omitted from each JSON object, is an empty table cell, and contributes an empty simple value. Empty repeated values render as `[]` in JSON and an empty cell/value in text.

The client preserves provider issue order within the requested page. It does not sort issues, assignees, or labels.

### Detail

Detail `simple` uses labeled `key: value` lines in this fixed order:

```text
index, state, author, author-id, url, title, created, updated, deadline,
assignees, milestone, labels, comments, repo, owner, kind, body
```

Unset optionals and unrequested comments are skipped; `body` is last and verbatim. Other scalar values use the text sanitization above. When requested, comments appear under a `comments:` section in provider order, with each comment rendered in fixed `id,author,author-id,url,created,updated,body` order and body last/verbatim.

Detail `table` uses the same issue field order as columns and serializes requested comments as compact JSON in the `comments` cell so the output remains one record. Detail `json` is one typed object with the same names and comment objects, followed by a newline.

Before rendering, the client requires a non-empty request ID, the response branch matching the command, non-nil records, valid required enums and timestamps, and all semantic invariants described below. It prepares the entire output before writing and retains issue #9's 8 MiB rendered-output limit, so failures cannot emit a partial document.

## Request and provider flow

```text
restricted `tea issues`
  -> strict parser and local bounds
  -> typed GiteaRequest over authenticated TLS gRPC
  -> server operation/selector/filter/field validation
  -> shared lifecycle resolves exact issues:read grant
  -> trusted provider ID selects one immutable Gitea adapter
  -> canonical owner/name plus typed filters call the SDK
  -> adapter validates kind, presence, ordering, and provider values
  -> optional explicit comment-page loop for issue view
  -> projected typed response
  -> shared metadata and 8 MiB Protobuf response limit
  -> client response validation and deterministic rendering
```

List uses exactly one `ListRepoIssues` SDK call with canonical owner/name, `Type=issues`, and the requested page, limit, state, and filters. A page beyond the end returns an empty list successfully. The adapter preserves the provider page order and rejects a provider response longer than the requested limit or containing a pull request.

View uses one `GetIssue` call with canonical owner/name and index. A nil issue, mismatched positive index, pull-request marker, or invalid record is a provider failure after successful retrieval. Provider not-found remains a sanitized not-found operation failure.

For `include_comments`, the adapter calls `ListIssueComments` sequentially with page size 50, starting at page 1. A short or empty page ends retrieval. At most 20 full pages (1,000 comments) are accepted; after a twentieth full page, page 21 is requested at the same page size to distinguish exact completion from truncation. If another comment exists, the operation fails with the sanitized response-limit error rather than returning partial comments. Cancellation, deadline, any malformed page, repeated/non-increasing comment IDs, or any page error stops the loop with no partial response and no retry. The comments must remain in the strictly increasing provider order returned by Gitea.

Issue normalization requires valid UTF-8 without NUL, non-negative counts, positive IDs, non-empty title/author/URL/repository identity, canonical case-sensitive owner and repository names, valid timestamps, `updated >= created`, and optional deadline validity. Each assignee and label must be non-nil and have a non-empty valid name. Comment normalization applies corresponding ID, author, URL, body, and timestamp checks with `updated >= created`.

## Authorization, anti-enumeration, and errors

Both operations require exactly `issues:read`. Repository resolution, provider-kind enforcement, accepted-audit failure behavior, response metadata, response limits, and terminal audit metadata reuse the issue-#9 shared lifecycle unchanged.

Unknown repositories, unauthorized repositories, missing capability, and provider-kind mismatch remain indistinguishable: permission denied, no provider call, and no trusted provider/repository metadata before successful resolution. Client input cannot select a provider ID, authority, SDK client, credential, or raw endpoint.

Gitea's issue and pull-request index space is shared. List requests always send the provider's issue-only filter and reject any pull request returned. A detail lookup that resolves to a pull request fails with a sanitized corrective kind error equivalent to “index is a pull request; use tea pulls”; it never renders pull-request content or fetches comments. This distinction is allowed only after exact repository authorization and therefore does not weaken repository anti-enumeration.

Malformed typed requests are invalid argument errors before policy or provider work. Raw SDK errors, HTTP bodies, filter values, issue/comment content, and credentials never reach client diagnostics or audit. Provider authentication, timeout, cancellation, and unavailable responses retain the established sanitized mappings. There is no automatic retry.

Accepted audit operations are `gitea.issue_list` and `gitea.issue_view`. Audit records contain only the bounded lifecycle metadata established by issue #9. They do not contain indices, filters, selected fields, issue/comment data, pagination URLs, SDK errors, or tokens.

## Compatibility constraints

- Repository-view behavior from issue #9 and all GitHub and Git behavior remain unchanged.
- `GiteaService` remains conditionally registered only for complete Gitea runtimes; no new adapter registry is introduced.
- Existing secure authority, token injection, TLS roots, redirect rejection, two-minute deadline, 8 MiB provider-body budget, final Protobuf budget, concurrency, and audit contracts remain unchanged.
- Repository selectors remain case-insensitive against configured Gitea slugs; provider calls use canonical configured casing.
- The command surface remains a documented subset of `tea 0.15.1`; unsupported output formats, `--kind`, comment-list commands, inference, login state, and interactivity remain rejected.
- Protocol and JSON changes are additive within `v1`. No production artifact contains upstream `tea`.

## Verification strategy

These automated tests pass the Patchmill Testing Value Gate because they cover public parsing/rendering contracts, typed protocol presence, reusable normalization and pagination logic, authorization, anti-enumeration, provider boundaries, and response limits.

### Protocol, parser, and renderer tests

- Protocol contract tests lock fresh tags, enum values, optional presence, and branch names; `buf lint`, breaking checks, and generated-code freshness remain required.
- Parser tables cover command aliases, implicit and explicit list, detail indices, all list filters, defaults, field replacement/order, `--comments`, output formats, duplicate flags, timestamp relationships, numeric/string/argv bounds, and representative rejected commands and flags.
- Parser fuzzing proves arbitrary argv cannot panic, bypass the command/field allowlists, create invalid typed requests, or echo attacker input in diagnostics.
- Renderer tests prove exact table, simple, and typed JSON bytes; selected field order; default fields; optional omission; empty arrays/pages; control characters; comment sections; response-branch and enum validation; exact-limit success; over-limit rejection; and no partial writes.

### Adapter, server, and composition tests

- Adapter list tests assert one canonical SDK call, exact option mapping, issue-only type, page/limit behavior, provider-order preservation, selected-field projection, empty pages, and rejection of oversized pages, nil records, pull requests, invalid presence, bad timestamps, and malformed nested values.
- Adapter view tests assert one canonical issue call, no comment call by default, kind validation before comment retrieval, complete field mapping, optional presence, cancellation, safe status mapping, and no retry.
- Comment pagination tests cover empty/short pages, two pages, exactly 1,000 comments plus an empty page-21 probe, overflow detection, repeated/out-of-order IDs, cancellation between pages, aggregate response limits, and failure without partial results.
- Server tests prove `issues:read` enforcement, malformed-request rejection, repository anti-enumeration, corrective issue-kind errors only after authorization, canonical operation names, accepted/terminal event order, response metadata and size enforcement, and no adapter call after validation, policy, or accepted-audit failure.
- Application tests prove multiple Gitea provider records cannot cross-route clients, authorities, or credentials.

### Real Gitea integration

Extend the digest-pinned current Gitea `1.27.2` fixture with open and closed issues, optional fields, labels, assignees, and more than one provider page of ordered comments. Through the packaged `tea` personality and TLS broker, demonstrate:

```text
tea issues --repo OWNER/REPO --state all --page 1 --limit 2 --fields index,title,state,comments --output json
tea issues INDEX --repo OWNER/REPO --comments --output json
```

Assert exact typed output, issue-only filtering in the shared index space, explicit multiple comment requests, canonical upstream owner/repository, an empty page beyond the end, denied repository behavior, accepted/completed audit events, and absence of provider credentials and issue/comment bodies from sandbox environment, process arguments, diagnostics, and audit.

Existing repository-view, GitHub, Git, security, race, fuzz, integration, Nix/package, OCI, and release checks remain regression gates. Static documentation or packaging declarations need no new text-content test; built-artifact invocation and existing checks provide direct verification. Final verification includes `go vet`, `go test -race`, `buf lint`, breaking/generated checks, and `git diff --check`.

## Non-goals

This issue does not add:

- issue create, edit, comment, close, reopen, label mutation, or any other write;
- pull-request messages or commands;
- comment list, view, edit, or delete commands, or public comment page flags;
- rich label, milestone, repository, or user objects;
- repository inference, global issue search, arbitrary API requests, raw JSON forwarding, prompts, editors, stdin bodies, or login state;
- retries, provider health checks, diagnostics, metrics, reload, broad version certification, or the upstream `tea` oracle; or
- a provider registry, generic forge DTO, or provider-neutral CLI.

## Acceptance criteria

1. The restricted `tea` personality accepts only the documented issue list/detail forms, aliases, flags, fields, defaults, and bounds.
2. The additive protocol represents issue list/view requests, projected issue records, comment records, state/kind enums, pagination, filter presence, and optional provider data without raw or generic fields.
3. A granted list performs one issue-only SDK request against the canonical repository and returns at most the requested page in provider order.
4. A granted view returns exactly one validated issue; `--comments` retrieves complete ordered comments through explicit bounded provider pages, while the default makes no comments call.
5. Pull requests never appear as issues; a detail kind mismatch produces a corrective sanitized error only after exact repository authorization.
6. Unknown, unauthorized, wrong-provider, malformed, provider-failed, cancelled, and over-limit requests make no unintended call, expose no repository distinction before authorization, and never return partial output.
7. Table, simple, and JSON output is deterministic, field-selectable for lists, typed where applicable, presence-aware, non-TTY-dependent, and bounded to 8 MiB.
8. Accepted and terminal audit events use canonical operation names and contain no selectors, filters, fields, issue/comment content, provider details, or secrets.
9. Multiple configured Gitea instances cannot cross-route SDK clients, authorities, or credentials.
10. Unit, fuzz, service, response-limit, and one real pinned-Gitea integration test prove pagination, ordering, presence, anti-enumeration, and the complete client-to-provider demonstration.
11. Existing repository-view, GitHub, Git, generated-code, security, race, integration, Nix, OCI, and release checks remain green.
12. No issue mutation, pull-request operation, public comment-list operation, or production upstream `tea` dependency is added.

## Open questions

None.
