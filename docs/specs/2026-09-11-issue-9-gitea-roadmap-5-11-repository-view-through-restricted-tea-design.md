# Repository view through restricted `tea` design

**Date:** 2026-09-11

**Status:** Approved for implementation planning

## Context

Issue #8 constructs one bounded Gitea SDK client per configured provider record but exposes no Gitea RPC or sandbox command. RepoWolf now needs the first complete Gitea API slice: inspect one configured repository through a restricted `tea` personality without exposing the provider token.

This slice is also the first point at which GitHub and Gitea need the same provider-request lifecycle. The implementation will extract only the proven server lifecycle seam while keeping provider parsing, validation, execution, and rendering separate.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-04, ID-05, ID-06, ID-07, and ID-08 without creating an exception.

## Outcome

With a Gitea repository configured and granted `repository:read`, an agent can run the supported repository-detail command through the `tea` personality. RepoWolf parses a bounded command, sends a typed request over authenticated TLS gRPC, authorizes the exact repository, calls the configured Gitea SDK client, normalizes the repository, renders deterministic output, and writes safe accepted and terminal audit events.

The production path contains no `tea` executable and no raw Gitea API forwarding.

## Approaches considered

### Provider-specific slice with a shared server lifecycle

This is the selected approach. A new Gitea protocol, client package, service, and SDK adapter own Gitea behavior. A narrow internal server helper owns behavior already shared with GitHub: policy resolution, provider-kind enforcement, accepted audit, response metadata, response-size enforcement, and bounded terminal audit annotations.

This delivers the vertical slice while satisfying ID-05 without creating a generic forge model.

### Duplicate the GitHub service lifecycle

Copying the GitHub service would be initially smaller, but authorization, audit, and response-limit behavior would immediately have two implementations that could drift. It is rejected now that two API providers prove the seam.

### Introduce a provider-neutral repository API

A common repository DTO and adapter interface would hide meaningful GitHub and Gitea output differences and anticipate future operations. It is rejected as premature abstraction.

## Existing behavior and affected components

The implementation baseline includes the merged secure-transport work from issue #8, even if this specification branch predates that merge.

- `proto/repowolf/v1/github.proto` and `internal/server/github.go` provide the existing typed unary-provider shape.
- `internal/app/providers.go` retains one bounded Gitea SDK client per provider ID but has no Gitea executor.
- `internal/provider/gitea` constructs secure SDK clients but has no repository operation.
- `cmd/repowolf-client` recognizes only `gh` and `repowolf-git-ssh` personalities.
- `internal/client/github` demonstrates strict parsing and bounded rendering, but its grammar and output vocabulary remain GitHub-specific.
- The unary audit interceptor writes terminal outcomes, while provider services separately resolve policy and write accepted events. Terminal events currently cannot consistently carry the resolved provider, repository, canonical operation, or bounded message sizes.

Affected areas are the Gitea Protobuf and generated code, Gitea client parser/renderer, Gitea SDK adapter, application composition, server registration/lifecycle, audit tests, client packaging aliases, and integration fixtures. Issue and pull-request messages are not affected.

## Protocol contract

Add `proto/repowolf/v1/gitea.proto` in package `repowolf.v1` with:

- `GiteaService.Execute(GiteaRequest) returns (GiteaResponse)`;
- `GiteaRequest.context` using the existing `RequestContext`;
- one request branch, `repository_view`, containing an empty `GiteaRepositoryViewRequest`;
- one response branch, `repository_view`, containing `GiteaRepositoryViewResult`;
- `GiteaResponse.meta` using the existing `ResponseMeta`; and
- `GiteaRepositoryRecord` containing only the approved repository fields.

The repository record exposes these stable JSON names and types:

| Field | Type |
| --- | --- |
| `full_name`, `description`, `default_branch`, `url`, `ssh_url`, `clone_url` | string |
| `private`, `archived`, `fork`, `mirror`, `empty` | boolean |
| `stars`, `forks`, `open_issues`, `size` | non-negative integer; `size` is KiB |
| `topics` | repeated string |
| `created`, `updated` | Protobuf timestamp, rendered as RFC 3339 |

No issue, pull-request, comment, pagination, generic path, raw JSON, or arbitrary argument field is added. Normal `v1` additive-only rules apply, and generated files change only through `scripts/generate.sh`.

## Restricted `tea` command

The client remains the existing multicall `repowolf-client` binary. Invoking it with basename `tea` selects the new personality; packages and sandbox images expose a `tea` link to that binary. No upstream `tea` binary is shipped or invoked.

This issue accepts only:

```text
tea repos <owner>/<name> --repo <owner>/<name> [--output table|simple|json]
tea repo  <owner>/<name> --repo <owner>/<name> [--output table|simple|json]
```

`repos` is canonical and `repo` is its alias. The positional selector and required `--repo`/`-r` selector must identify the same case-folded Gitea slug; disagreement is rejected. Requiring both preserves the roadmap's repository-detail grammar and its rule that every remote command carries an explicit `--repo`. The typed request carries one selector only. The server resolves it against configured canonical casing and remains authoritative.

`--output`/`-o` accepts exactly `table`, `simple`, or `json`; the default is `simple`. Long and short spellings of one flag count as duplicates. Bare `tea repos`, repository enumeration, `--login`, `--remote`, output-field selection, `--help`, `--version`, `--flag=value`, current-directory inference, remote inference, and every unlisted command, positional value, or flag are rejected.

The parser applies the existing client-wide bounds of at most 64 arguments and 64 KiB of argument bytes, requires valid UTF-8 without NUL bytes, and accepts the configured Gitea owner/name grammar only. It has no prompt, editor, stdin, environment-based endpoint, or TTY-dependent path.

Usage failures exit 2 and print a bounded diagnostic naming the accepted repository-view shape without echoing rejected input. Configuration, connection, RPC, provider, response-validation, rendering, and output-write failures exit 1 with stable sanitized diagnostics. Cancellation preserves the existing signal exit behavior.

## Rendering contract

Rendering first verifies that the response has a non-empty request ID, the `repository_view` result, and a non-nil repository record. It never renders partial or mismatched responses.

- `simple` prints one `key: value` line per field in the table order below.
- `table` prints one tab-separated header row and one data row in the same order.
- `json` prints one object followed by a newline, with exactly the snake-case keys below and native JSON strings, booleans, numbers, and arrays.

The fixed order is:

```text
full_name, description, default_branch, url, ssh_url, clone_url,
private, archived, fork, mirror, empty, stars, forks, open_issues,
size, topics, created, updated
```

Simple and table scalar values replace control characters with spaces; topics are comma-and-space joined. JSON uses normal escaping and renders topics as an array. Times use UTC RFC 3339. Output does not depend on terminal detection. Rendered output is limited to 8 MiB, and output is fully prepared before any byte is written so validation or limit failures cannot produce a partial success document.

## Request and data flow

```text
sandbox `tea`
  -> strict repository-view parser
  -> typed GiteaRequest over TLS gRPC
  -> authentication and shared request bounds
  -> Gitea validation: operation, selector, capability, audit name
  -> shared provider lifecycle resolves exact repository:read grant
  -> provider ID selects its immutable Gitea SDK client
  -> SDK GetRepo(canonical owner, canonical name)
  -> provider transport enforces authority, token, TLS, deadline, and 8 MiB body limit
  -> Gitea adapter validates and normalizes the repository
  -> shared lifecycle adds request ID and enforces the final 8 MiB protobuf limit
  -> client validates and renders the selected format
```

The adapter interface is the narrow operation surface used by the service: execute a typed Gitea request for one already-resolved repository. Application composition builds an adapter around each issue-#8 SDK client and dispatches strictly by the trusted provider ID from `policy.ResolvedRepository`. Missing or mismatched instances fail closed.

The adapter calls only `GetRepo` with the configured canonical owner and name. It does not accept a URL, host, path, token, or SDK options from the client request. It rejects a nil SDK result and semantically invalid data before constructing the typed response. The returned `full_name` must exactly equal the configured canonical `owner/name`; strings and topics must be valid UTF-8 without NUL; counters and size must be non-negative; URLs must be non-empty; timestamps must be non-zero; and `updated` must not precede `created`. The transport and final Protobuf limits bound the aggregate response. Provider-returned ordering is preserved only for topics; all object field order is renderer-defined.

## Shared provider lifecycle and audit

Extract a private server lifecycle abstraction rather than a public provider framework. GitHub and Gitea services still own provider-specific request validation, capability mapping, selector construction, operation naming, and executor dispatch. The shared lifecycle owns:

1. authenticated principal and request-ID lookup;
2. exact policy resolution for one expected provider kind and capability;
3. accepted-event emission before provider execution;
4. response request-ID attachment;
5. final serialized Protobuf response-size enforcement; and
6. bounded terminal metadata supplied to the existing unary audit interceptor.

The accepted Gitea event uses provider `gitea`, the trusted repository ID, operation `gitea.repository_view`, and outcome `accepted`. If that write fails, the SDK is not called.

For a validated provider request, the terminal event uses the canonical operation rather than only the gRPC method and may contain only existing bounded audit-schema fields: request ID, principal, provider kind, trusted repository ID, outcome, sanitized reason, duration, serialized input bytes, and serialized output bytes. Input and output byte counts are Protobuf sizes, not provider HTTP byte counts; output is zero when no response is returned. No selector text, repository description, topic, URL, token, HTTP body, SDK error, or rendered output is audited.

Before successful resolution, provider and repository remain empty so audit output cannot become a repository-enumeration channel. Early interceptor rejections retain their current gRPC-method operation and empty provider metadata. Event order remains accepted then terminal for executed provider calls, and a terminal event remains the only event for rejection before execution.

GitHub adopts the same lifecycle with no change to its command grammar, provider calls, response bytes, policy decisions, accepted-event order, client-facing errors, or limits. Its successful terminal events gain the same bounded metadata.

## Security and error behavior

- The server validates the typed operation and selector before policy lookup or provider execution.
- Repository view requires exactly `repository:read`.
- Unknown repositories, ungranted repositories, missing capability, and provider-kind mismatch all return the same sanitized permission-denied response and make no SDK call.
- SDK and provider HTTP errors are mapped through stable domain errors to sanitized gRPC statuses. Raw response bodies, redirect targets, SDK messages, repository fields, and credentials never reach client diagnostics or audit.
- Caller cancellation and the shorter of the server request deadline and issue-#8 transport deadline stop the SDK request. There is no automatic retry or fallback client.
- The final typed response and rendered output each remain bounded to 8 MiB. Transport response accounting remains independently bounded by issue #8.
- The Gitea token remains only in its broker-side transport and cannot enter request messages, command arguments, child environments, responses, logs, or audit events.

## Compatibility constraints

- Existing GitHub and Git command behavior remains unchanged.
- Gitea-only, GitHub-only, and mixed runtimes remain valid. `GiteaService` is registered only when a complete Gitea executor exists; `GitHubService` registration remains conditional and independent.
- Repository selectors remain case-insensitive for configured Gitea slug matching, while upstream requests use configured canonical casing.
- The secure transport, endpoint, CA, timeout, redirect, and response-budget contracts from issue #8 are reused unchanged.
- Protocol additions are additive within `repowolf.v1`.
- No production artifact contains the upstream `tea` executable.

## Verification strategy

These tests pass the Patchmill Testing Value Gate because they prove parser, authorization, normalization, error, audit, response-limit, and credential-boundary behavior.

### Unit and service tests

- Parser tables cover both command aliases, long and short output/repository flags, all three formats, duplicate/conflicting selectors, malformed slugs, missing operands, argument bounds, and representative rejected commands and flags.
- Parser fuzzing proves arbitrary argv cannot panic, bypass the allowlist, or create a request without one valid repository selector.
- Renderer tests use a full typed fixture and prove exact simple, table, and typed JSON output, fixed ordering, control-character handling, UTF-8 behavior, response-branch validation, exact 8 MiB success, over-limit rejection, and no partial writes.
- Adapter tests use a fake SDK seam to prove one canonical `GetRepo` call, complete field mapping, topic order, cancellation, nil/malformed response rejection, safe error mapping, and no retry.
- Server tests prove `repository:read` authorization, anti-enumeration, kind enforcement, service registration, request metadata, final Protobuf limits, and no adapter call after validation, policy, or accepted-audit failure.
- Shared lifecycle regression tests run GitHub and Gitea cases and prove accepted/terminal ordering, canonical operation names, trusted provider/repository metadata, bounded byte counts, cancellation/failure outcomes, and absence of request/provider content.
- Application tests prove provider-ID dispatch for multiple Gitea records and no cross-instance client use.

### Real Gitea integration

One digest-pinned current Gitea `1.27.2` container is seeded with a repository whose fields exercise strings, booleans, counters, topics, and timestamps. The test starts a TLS RepoWolf broker configured with that instance and runs the packaged `tea` personality through the complete client-to-provider path.

It asserts exact simple and JSON output, the canonical upstream repository, accepted and completed audit events, policy denial for an ungranted selector, cancellation/credential non-disclosure, and absence of the provider token from the sandbox environment, client output, broker audit, and process arguments. Fake-adapter tests do not substitute for this test.

Existing GitHub, Git, race, fuzz, integration, generated-code, Nix/package, OCI, and release checks remain regression gates. Static alias/package declarations are verified through built-artifact invocation rather than tests that merely restate configuration text. `buf lint`, breaking checks, generated-file freshness, `go vet`, `go test -race`, and `git diff --check` are required.

## Non-goals

This issue does not add:

- issue, pull-request, comment, label, milestone, user, or organization messages or commands;
- repository list, search, create, edit, delete, fork, migrate, clone, or open commands;
- `tea` login state, remote inference, current-directory inference, prompts, editors, stdin bodies, or arbitrary API access;
- output field selection, pagination, mutation behavior, retries, provider health checks, diagnostics, metrics, reload, or broad version certification;
- the pinned upstream `tea` oracle or any production `tea` subprocess; or
- a provider registry, generic forge DTO, or provider-neutral user CLI.

## Acceptance criteria

1. The existing client binary has a restricted `tea` personality and built artifacts expose it under the name `tea` without shipping upstream `tea`.
2. Only the documented repository-detail grammar, aliases, selectors, and output values are accepted; all other input fails closed without inference or interactivity.
3. The protocol contains only the minimal Gitea service, repository-view request/result, repository record, and response metadata required by this slice.
4. A granted request resolves one configured Gitea repository, calls only that provider instance's SDK client with canonical owner/name, and returns all approved repository fields.
5. Simple, table, and JSON rendering are deterministic, typed where applicable, non-TTY-dependent, and bounded to 8 MiB.
6. Unknown, unauthorized, wrong-kind, malformed, provider-failed, cancelled, and over-limit requests make no unintended provider call and return sanitized errors.
7. The shared provider lifecycle is used by both GitHub and Gitea without introducing a provider registry or changing existing GitHub behavior.
8. Accepted and terminal audit events have canonical, bounded, non-secret metadata and preserve fail-closed accepted-audit behavior and event order.
9. Multiple Gitea provider instances cannot cross-route SDK clients, authorities, or credentials.
10. Fake parser/renderer/adapter/service tests and one real digest-pinned Gitea 1.27.2 integration test exercise the complete behavior.
11. Existing GitHub, Git, security, race, fuzz, generated-code, integration, Nix, OCI, and release checks remain green.
12. No issue or pull-request protocol message or command is added.

## Open questions

None.
