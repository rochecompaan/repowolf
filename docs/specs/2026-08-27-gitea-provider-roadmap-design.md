# Gitea-First Provider Roadmap Design

**Status:** Revised draft for review

## Summary

RepoWolf will evolve from a GitHub broker into a repository-scoped forge access broker. Gitea will be the second supported provider.

The first Gitea release will support core collaboration work. Agents will use a restricted `tea`-compatible client and the existing Git transport. The server will use a native Gitea SDK adapter; the pinned `tea` 0.15.1 executable will serve as a test-only certification oracle.

Delivery will use a Gitea-first vertical slice. Shared modules will change only when the second provider proves that a seam is real.

This revision records the full contract set approved during the roadmap review: versions, command grammar, output, endpoints, runtime composition, packaging, protocol compatibility, diagnostics, and milestone gates. The decision record appendix lists each approved decision.

## Current state

RepoWolf has a strong security-focused MVP with these features:

- exact repository and capability grants.
- typed GitHub operations over TLS gRPC.
- restricted `gh` and Git SSH clients.
- bounded provider processes and Git streams.
- Git ref, delete, and update-count policies.
- safe JSONL audit records.
- race, fuzz, integration, leak, OCI, and multi-architecture CI tests.

The current implementation has these limits:

- GitHub is the only provider kind.
- runtime assembly constructs the GitHub adapter directly and unconditionally.
- parts of the Git path assume GitHub.
- configuration and credentials load only at startup.
- the client has no Gitea-compatible command path.
- operator diagnostics depend mainly on health status and JSONL audit output.

The original MVP design already defers Gitea operations, a restricted `tea` client, and Gitea service authentication.

## Product goal

RepoWolf will give agents scoped Gitea access without exposing Gitea credentials or SSH identities. The user experience will remain familiar to Gitea users.

One RepoWolf broker will serve configured GitHub and Gitea repositories. Both providers will use the same authentication, policy, limits, lifecycle, and audit controls.

## Guiding principles

1. Preserve exact-repository authorization and fail-closed behavior.
2. Keep provider credentials outside agent sandboxes.
3. Use typed operations instead of raw command or API forwarding.
4. Preserve provider-native client workflows where practical.
5. Keep provider-specific behavior inside provider adapters.
6. Extract shared logic only after GitHub and Gitea prove the seam.
7. Keep existing GitHub behavior stable during Gitea delivery.
8. Claim support only for versions that pass certification.
9. Treat compatibility as a documented subset; RepoWolf owns normalized output, errors, and limits.

## Compatibility and support policy

RepoWolf will maintain a bounded compatibility matrix:

- Supported Gitea minor lines: `1.26.x` and `1.27.x`.
- Initially certified versions: Gitea `1.26.4` and `1.27.2`.
- Later patch releases become supported only after the certification matrix passes against them in CI.
- The supported window is a rolling two minor lines. Adding a new minor line drops the oldest.
- Removing a supported minor line is a support change and will be documented as such in release notes.

The pinned `tea` executable is exactly `0.15.1`. It is not part of the production serving path. It is the certification oracle in the test harness (see Testing and validation).

"`tea`-compatible" means a documented subset of `tea 0.15.1` commands and flags with matching meanings. RepoWolf owns normalized output, errors, and limits. RepoWolf does not provide full drop-in `tea` compatibility. Every divergence is documented in the restricted command contract below.

`repowolf version` will report the certified matrix (Gitea versions and the pinned `tea` oracle) from a single source-of-truth constant shared by docs, CI certification, and the version output.

## First Gitea release

The first release will support these operations:

- repository view.
- issue list, view, create, edit, comment, close, and reopen.
- pull-request list, view, create, edit, comment, close, and reopen.
- Git fetch.
- controlled Git push.
- restricted `tea` command parsing and output rendering.

The release will use the existing capability groups where their meaning matches Gitea:

- `repository:read`.
- `issues:read` and `issues:write`.
- `pull_requests:read` and `pull_requests:write`.
- `git:read` and `git:write`.

Capability groups remain provider-agnostic. The first release adds no per-kind capability names.

## Non-goals

The first Gitea release will not add:

- Actions or workflow operations.
- merge operations.
- release management.
- repository, organization, or user administration.
- arbitrary Gitea API requests.
- repository or organization wildcards.
- a provider-neutral user CLI.
- sandbox creation or lifecycle management.
- a third provider.

The restricted command contract additionally excludes:

- `csv`, `tsv`, and `yaml` output formats.
- interactive prompts, `$EDITOR` flows, and stdin as a request body source.
- multi-index mutations (one index per invocation).
- repository enumeration (bare `tea repos` list form).
- subpath Gitea endpoints such as `https://example.com/gitea`.
- the `tea` binary in any production artifact or serving path.

## Approaches considered

### Gitea-first vertical slice

This approach adds one complete Gitea request path. It extracts shared seams only when both provider implementations need them.

This approach gives users early value and limits GitHub regression risk. It is the selected approach.

### Provider platform first

This approach creates provider-neutral types and registries before Gitea work. It offers a clean theoretical model but delays user value.

The approach also risks shallow modules based on one existing adapter. It is not selected.

### Mostly separate provider stacks

This approach duplicates the GitHub stack with few shared changes. It can deliver the first Gitea path quickly.

Long-term policy, audit, validation, and behavior drift make this approach unsuitable. It is not selected.

### Server-side `tea` subprocess adapter

This approach invokes a pinned server-side `tea` executable for Gitea operations, mirroring the GitHub adapter's use of `gh`. It inherits vendor CLI behavior but pays for it in production: per-operation process overhead, rendered login configuration, and normalization of text output. Key gaps exist: `tea 0.15.1` list JSON is all-strings, and `tea repos <owner>/<name>` ignores `--output` entirely, so repository view has no machine-readable form.

This approach is not selected for the serving path. The pinned `tea` binary instead becomes the certification oracle: the same binary that defines compatibility also verifies the native adapter in tests.

## Architecture direction

### Runtime composition

`internal/app` will construct provider adapters through a closed switch on provider kind. The runtime will hold one optional concrete adapter slot per kind and will build a kind only when the validated configuration contains it. An empty slot fails closed at dispatch. There will be no registry, registration API, plugins, or dynamic loading; adding a third provider means editing the switch.

GitHub-only, Gitea-only, and mixed configurations are all first-class deployments:

- Configuration requires at least one provider of any kind; each kind is optional.
- Tool resolution is kind-conditional: the `gh` executable is required only when a GitHub provider exists. Git and SSH tooling remain unconditional. The `tea` executable is never resolved at runtime.
- Startup remains fail-closed: if any configured adapter fails construction, the broker does not become ready. There is no degraded startup mode.
- GitHub-only behavior remains byte-identical to today.

### Provider configuration

The configuration model will add a `gitea` provider kind. A Gitea provider record keeps the existing shape: it is keyed by ID and pins its API host, Git host, SSH user, and SSH port.

Endpoint rules:

- API and Git hosts are host-only values; HTTPS is implied. Subpath endpoints are excluded from the first release.
- Plain-HTTP endpoints are rejected.
- An optional per-provider `caFile` adds a PEM bundle to the system roots for private certificate authorities. There are no skip-verify or hostname-override options. TLS failures fail closed.

Credential rules:

- Each provider record references its token through a `tokenEnv` field. The value must match `REPOWOLF_TOKEN_[A-Z0-9_]+`. Configuration never contains secrets.
- Tokens load once at startup. Reload is a separate deferred design (see Deferred design topics).
- One token per provider record, shared by its repositories. Provider tokens are distinct from principal tokens: principal tokens authenticate agents to the broker; the provider token authenticates the broker to Gitea.
- The operator documentation will state the minimal Gitea token scopes. The required set will be verified in Milestone 1 against both certified Gitea versions.
- The Gitea token is read in-process and never enters a child-process environment.

The client has no login commands; `tea login`, `logout`, and `whoami` are rejected. The server performs no startup identity probe, so broker startup never depends on Gitea availability. Token problems surface lazily and through `repowolf config doctor`.

Repository records will continue to refer to a configured provider by ID. Client input will not select executable paths, credentials, or provider hosts.

### Gitea protocol

Gitea operations will use provider-specific typed Protobuf messages in a new `proto/repowolf/v1/gitea.proto`, in the existing `repowolf.v1` package. The interface will contain only approved collaboration operations. The protocol will not carry raw `tea` arguments, raw JSON, arbitrary paths, or generic key-value request fields.

The service mirrors the GitHub shape: `GiteaService` with a single `Execute` method, a `oneof operation` request, and a `oneof result` response. Each grammar command has its own branch, including explicit close and reopen operations.

Protocol compatibility rules, defined before any Gitea message is added:

- Within `v1`, changes are additive-only: new messages, new fields with fresh tag numbers, new `oneof` branches, new enum values. Fields are never renumbered, renamed, retyped, or deleted; deletions use `reserved`.
- `optional` fields carry three-state presence semantics (unset, set-empty, set-value).
- Enum zero values are `UNSPECIFIED`. For list-state filters, unset means the documented default (`open`). For required-value fields, unset is a validation error and fails closed.
- Clients map unknown enum values to a sanitized error and never crash on them.
- CI enforces `buf lint`, `buf breaking` against the main baseline, and a generated-code freshness check.
- Generated code changes only through `scripts/generate.sh`.

Version-skew policy: within `v1`, the server must be at least as new as the client for new features; rolling upgrades are safe. A client calling an operation the server does not know receives an unimplemented or invalid-argument status, mapped to the sanitized unsupported-operation client error. There is no handshake RPC in `v1`.

### Gitea client

The client artifact is the existing multi-call `repowolf-client` binary with an added `tea` personality, selected by executable name like the current `gh` personality. Sandboxes install the same binary under the name `tea`.

The parser will accept only documented command forms from the restricted command contract below. The parser will convert each command into a typed Gitea request. Output rendering will use normalized typed responses.

### Gitea adapter

The production Gitea adapter is SDK-native. It calls the pinned `code.gitea.io/sdk/gitea` module directly over HTTPS, in-process, through the existing TLS and dialing modules. There is no `tea` subprocess in the serving path.

A narrow executor interface will separate the Gitea service from the adapter implementation. The executor seam is built in Milestone 1 with the SDK-native executor as the production path. The pinned `tea` 0.15.1 executable implements the same seam inside the test harness, where it acts as the certification oracle. There is no production-switchable `tea` fallback: the SDK and `tea` share the same API behavior, since `tea` itself wraps that SDK.

Production adapter bounds: a two-minute operation timeout, matching the GitHub operation timeout, and an 8 MiB bound per provider API response. The previously considered subprocess bounds (8 MiB captured output, scrubbed environment) apply only to the oracle harness.

### Git transport

The Git module will resolve repositories across configured provider kinds. It will not assume that every Git repository uses GitHub.

GitHub and Gitea will use the same deep Git module. The module will retain byte limits, protocol validation, push policies, process cleanup, and audit behavior.

### Shared modules

These modules remain shared:

- authentication and principal context.
- immutable policy snapshots.
- repository and capability resolution.
- process execution and cleanup.
- Git protocol handling.
- request limits and concurrency control.
- audit records.
- TLS and client dialing.
- sanitized status mapping.

Provider operation parsing, validation, command planning, and normalization will remain provider-specific. Proven duplication can move behind a shared interface after Gitea parity.

The provider process environment becomes per-kind. The GitHub subprocess environment contains only what `gh` needs. The Gitea token never enters any child-process environment.

## Restricted `tea` command contract

This section is the complete, exact grammar contract. Anything not listed here is rejected.

### Command surface

Read operations:

- `tea repos <owner>/<name>` (alias `repo`): repository detail view. Bare `tea repos` is rejected; repository enumeration is out of scope.
- `tea issues` (aliases `issue`, `i`): same as `issues list`.
- `tea issues <index>`: issue detail view; `--comments` includes comments.
- `tea issues list` (alias `ls`): issue list.
- `tea pulls` (aliases `pull`, `pr`), `tea pulls <index>`, `tea pulls list` (alias `ls`): the same pattern for pull requests.

Mutations:

- `tea issues create` (alias `c`), `tea issues edit <index>` (alias `e`), `tea issues close <index>`, `tea issues reopen <index>` (alias `open`).
- The same four forms for `tea pulls`.
- `tea comments add <index> [<body>]` (alias `a`); body positional or `--description`.
- `tea comment <index> [<body>]` (aliases `comments`, `c`): identical to `comments add`, matching `tea 0.15.1`.

Every other `tea 0.15.1` command is rejected with an unsupported-command error that names the restricted set. This includes `pulls checkout`, `merge`, `approve`, `reject`, `review`, `resolve`, `clean`, and `reply`; `comments list`, `edit`, and `delete`; `repos list`, `search`, `create`, `fork`, `migrate`, `delete`, and `edit`; and all of `labels`, `milestones`, `releases`, `times`, `notifications`, `organizations`, `admin`, `api`, `wiki`, `branches`, `attachments`, `webhooks`, `sshkeys`, `login`, `logout`, `whoami`, `open`, and `clone`.

Documented deviations from `tea 0.15.1`:

- D1: mutations accept a single index per invocation. `tea` allows `<idx> [<idx>...]`. One mutation is one policy decision and one audit record.
- D2: bare `tea repos` is rejected; only the detail form exists.
- D3: `--login` and `--remote` are removed globally. RepoWolf owns credentials and endpoints.
- D4: `--repo` accepts only an `<owner>/<name>` slug that resolves to a configured repository record. Filesystem paths are rejected.
- D5: no interactivity. Missing required input is a usage error with a non-zero exit. There are no prompts and no `$EDITOR` fallback.
- D6: aliases are kept exactly as `tea 0.15.1` documents them, with matching meanings.

### Per-command flags

Global flags from the next section apply to every command. Any flag not listed as kept is rejected with an unsupported-flag error.

- `issues list` keeps `--state` (all\|open\|closed, default `open`), `--keyword -k`, `--author -A`, `--assignee -a`, `--mentions -M`, `--from -F`, `--until -u`, `--owner/--org`, `--page -p` (default 1), `--limit --lm` (default 30), and `--fields`. Dropped: `--kind -K`; issue lists never return pull requests.
- `pulls list` keeps `--state`, `--page`, `--limit`, and `--fields`.
- `issues create` keeps `--title -t` (required), `--description -d`, `--assignees -a`, and `--labels -L`. Dropped: `--milestone`, `--deadline`, and `--referenced-version`; milestone and deadline management is not scoped for this release.
- `pulls create` keeps `--head`, `--base -b`, `--draft`, `--allow-maintainer-edits/--edits` (default true), `--title -t`, `--description -d`, `--assignees -a`, and `--labels -L`. Dropped: `--agit` and `--topic`, plus the create drops above.
- `issues edit` keeps `--title`, `--description`, `--set-assignees`, `--add-assignees -a`, `--remove-assignees`, `--add-labels -L`, and `--remove-labels`. Dropped: `--milestone`, `--deadline`, and `--referenced-version`.
- `pulls edit` keeps the `issues edit` set plus `--add-reviewers -r`, `--remove-reviewers`, `--draft`, and `--ready`. The same drops apply.
- `close` and `reopen` take global flags only.
- `comments add` takes the positional body or `--description -d`. Dropped: stdin as a body source.

The `pulls list --fields` allowlist excludes `diff`, `patch`, and `ci` (unbounded size or unscoped commit-status surface). The allowed set is: `index`, `state`, `author`, `author-id`, `url`, `title`, `body`, `mergeable`, `base`, `base-commit`, `head`, `created`, `updated`, `deadline`, `assignees`, `milestone`, `labels`, `comments`. The issues allowlist keeps the full `tea` set of seventeen fields; note `kind` is now always `issue`.

Default field sets match `tea 0.15.1`: issues list shows `index`, `title`, `state`, `author`, `milestone`, `labels`, `owner`, `repo`; pulls list shows `index`, `title`, `state`, `author`, `milestone`, `updated`, `labels`.

### Global flags and repository selection

- `--repo -r <owner>/<name>` is required on every command. There is no current-directory or remote inference. The slug resolves by case-insensitive exact owner and name match against configured repository records; upstream calls use the record's canonical casing. Unknown and unauthorized repositories return the identical client-facing error; the audit record keeps the true reason.
- `--output -o table|simple|json`. Other values, including `csv`, `tsv`, `yaml`, and `yml`, are a hard error naming the accepted set.
- `--fields` is valid only on the two list commands.
- `--page -p` and `--limit --lm` are valid only on list commands.

### Output contract

- List operations default to `table`. Detail and mutation operations default to `simple`. `--output json` provides stable machine-readable output. Output never changes based on TTY detection.
- `--fields` replaces the default field set, preserves the given order, and applies identically to all three formats. An unknown field is a hard error listing the operation's allowlist. Output contains exactly the selected fields.
- List `table` is headers plus rows; headers are the selected field names. List `simple` matches `tea 0.15.1` exactly: space-joined values, one line per item, no headers.
- List `json` is a top-level array of flat objects with `tea` field names as keys. Values are typed: numbers, arrays, and RFC 3339 timestamps. This is a documented divergence from `tea`, whose JSON values are all strings.
- Detail `simple` is RepoWolf-defined: labeled `key: value` lines in a documented fixed order per operation, with `body` last and verbatim. `--comments` appends a documented `comments` section. `tea` has no field-based detail format to inherit.
- Detail and mutation `json` is a single object under the same key and typing rules. Mutations return canonical keys: `index`, `title`, `state`, `url` for issues and pull requests; `id`, `url`, `body` for comments.
- Stability: within a RepoWolf minor line, JSON keys are additive-only. Removal, rename, or type change is a breaking change documented in release notes. There is no version envelope in `v1`.

### Edit and mutation semantics

- E1: assignee and label precedence matches `tea 0.15.1`: `--set-assignees` beats `--add-assignees`, which beats `--remove-assignees`; `--add-labels` beats `--remove-labels`. There is no `--set-labels`.
- E2: an edit with zero mutation flags is a usage error listing the available mutation flags.
- E3: string mutation flags are three-state. Unset means no change. `--description ""` clears the body. `--title ""` is a client-side validation error.
- E4: on `pulls edit`, `--draft` and `--ready` conflict with each other and with `--title` in the same invocation; the conflict is a hard error.
- E5: `close` and `reopen` are idempotent ensure-state operations. Closing an already-closed issue succeeds and reports the final state. The audit record captures the request and whether a transition occurred.
- E6: entity-kind validation rejects a pull-request index on `issues` commands and an issue index on `pulls` commands, with a corrective error. Gitea pull requests share the issue index space.
- E7: mutation responses carry canonical keys only; there is no changed-fields diff in `v1`.

### Pagination and limits

- `--page` and `--limit` default to 1 and 30. `--limit` is capped at 50, the Gitea default API maximum on both certified lines; higher values are a hard error naming the cap.
- There is no pagination envelope. If the returned count equals `--limit`, another page may exist; an empty page is the end. A page beyond the end returns an empty list with a success exit.
- Input bounds, validated at client parse time and again server-side: `--title` at most 255 characters; `--description` and comment bodies at most 64 KiB; CSV flags at most 25 entries of at most 255 characters each. Violations are validation errors before any provider call.
- Existing shared limits are unchanged: concurrency (8 global, 4 per principal), the 1 MiB maximum message size, and stream caps. A list selection with heavy fields such as `body` at a high `--limit` can exceed the message cap; the server then returns a sanitized limits error advising a smaller `--limit` or fewer fields. The cap is not raised in this roadmap.
- Production adapter bounds: two-minute operation timeout and an 8 MiB bound per provider API response. Oracle-harness subprocess bounds: 8 MiB captured output and a scrubbed environment.

### Typed responses

The protocol defines `GiteaRepo`, `GiteaIssue`, `GiteaPull`, and `GiteaComment` messages. List responses are a repeated field with no envelope.

Field types: `index` and `author-id` are `int64`; `state` and `kind` are enums rendered as their string values; `created` and `updated` are timestamps, RFC 3339 in JSON; `deadline` is an optional timestamp; `assignees` and `labels` are repeated strings; `milestone` is an optional string holding the milestone title; `mergeable` is an optional boolean, omitted when Gitea reports unknown; `base`, `head`, and `base-commit` are strings, with `head` rendered as `user:branch` for cross-repository pull requests.

The `comments` field has exactly two shapes: an `int64` count in list context, and an array of `GiteaComment` in a detail response with `--comments`.

Presence rules: unset optionals are omitted from JSON and skipped in `simple` output; empty repeated fields render as `[]`; canonical keys are always printed.

Labels and milestones are plain strings in this release. Richer label and milestone objects are deferred; adding them later is additive under the output stability rule.

The `GiteaRepo` field set is RepoWolf-defined, since `tea 0.15.1` repository detail has no machine-readable form: `full_name`, `description`, `default_branch`, `url`, `ssh_url`, `clone_url`, the booleans `private`, `archived`, `fork`, `mirror`, `empty`, the counters `stars`, `forks`, `open_issues`, `size` (KiB), `topics`, `created`, and `updated`.

## Request flow

A Gitea request will use this flow:

1. The sandbox invokes the restricted `tea` client.
2. The client parses an approved command form.
3. The client sends a typed request over TLS gRPC.
4. The server authenticates the bearer token.
5. The policy module resolves the principal, repository, and capability.
6. The Gitea adapter executes the operation through the SDK-native executor.
7. The adapter validates and normalizes the bounded provider response.
8. The server returns a typed response with request metadata.
9. The broker writes safe audit events for the operation lifecycle.

Raw provider credentials never enter the request, response, command arguments, or audit record.

## Roadmap

### Now: prove the Gitea contract

- Select the supported Gitea and `tea` versions. (Done: see Compatibility and support policy.)
- Define the restricted `tea` command grammar. (Done: see the command contract.)
- Define Gitea authentication and repository-selection rules. (Done: see Provider configuration.)
- Define protocol compatibility rules before adding Gitea messages. (Done: see Gitea protocol.)
- Create provider contract fixtures for validation, normalization, errors, and secret-leak detection.

**Exit condition:** The operation matrix and compatibility contract are stable.

### Milestone 1: read-only vertical slice

- Add Gitea configuration and adapter registration.
- Add the executor seam with the SDK-native executor as the production path.
- Add repository, issue, and pull-request read operations.
- Add Git fetch for configured Gitea repositories.
- Add restricted `tea` parsing and response rendering.
- Add provider-aware audit records.
- Add end-to-end TLS tests with a fake Gitea adapter.
- Verify and document the minimal Gitea token scope set.

**Exit condition:** An agent can inspect and clone a permitted Gitea repository without provider credentials. Repository view, issue and pull-request list and view, and Git fetch pass against containerized Gitea `1.26.4` and `1.27.2` through the SDK-native adapter in CI. Milestone 1 cannot exit on fake-adapter tests alone.

### Milestone 2: controlled collaboration writes

- Add issue create, edit, comment, close, and reopen operations.
- Add pull-request create, edit, comment, close, and reopen operations.
- Add controlled Git push with the existing Git policies.
- Validate inert input, response bounds, cancellation, and credential isolation against the real adapter.
- Publish Docker and native Gitea deployment examples.

**Exit condition:** The documented core collaboration operation matrix is complete. All write operations and push policies pass against both certified container versions. The Docker example smoke test is green in CI.

### Milestone 3: multi-provider operations

- Add `repowolf config doctor` diagnostics.
- Diagnose credentials, tools, TLS, connectivity, and policy before cutover.
- Add provider-tagged metrics and safe operator diagnostics.
- Add audit sink health and loss visibility.
- Document combined GitHub and Gitea deployment, migration, and rollback.
- Publish the compatibility matrix for RepoWolf, Gitea, and `tea` versions, generated from the certification run.

**Exit condition:** Operators can deploy and diagnose both providers with consistent workflows. The full certification matrix, diffing the SDK-native adapter against the `tea` 0.15.1 oracle, is green on both certified versions.

### Later: platform leverage

- Consolidate duplication that both adapters prove.
- Publish a provider contract-test harness.
- Evaluate GitLab as the third provider.
- Reassess a provider-neutral CLI after maintenance data exists for both shims.

Transactional configuration, token, and certificate reload is deliberately not in this roadmap; see Deferred design topics.

## Packaging and deployment

- The client remains one multi-call `repowolf-client` binary. It gains a `tea` personality selected by executable name. There is no separate client artifact.
- The go-sdk module is a pinned Go dependency with the usual Nix hash pinning.
- No `tea` binary appears in any production artifact, package, or image. The pinned `tea` 0.15.1 exists only as a hash-pinned test-harness input.
- The published image continues to contain `gh` so mixed deployments work. A Gitea-only slim image is possible but not published in this release.
- `repowolf version` reports the certified compatibility matrix from the single source-of-truth constant.
- The Docker example gains a Gitea service with a CI-exercised smoke test. The native example is documentation plus a configuration file validated in CI.

## Diagnostics, metrics, and audit health

`repowolf config doctor` is a one-shot, read-only operator command in the server binary. It checks, in fail-closed order: configuration; TLS including expiry; principal token environments; per provider, the token environment, API host DNS, TCP, and TLS with any configured `caFile`, an authenticated identity probe, and Git host SSH reachability; tool resolution; policy construction; and audit output writability. It defaults to text output with a `--json` option. Exit codes are 0 for pass, 1 for warnings only, and 2 for any failure. Connectivity checks run by default with a ten-second timeout per probe; `--offline` restricts to local checks. Doctor never prints secrets.

Metrics use a Prometheus text endpoint on a separate listener, disabled by default and bound to localhost unless configured. Labels are limited to provider, operation, and outcome; repository, owner, principal, and request identifiers never appear in metrics. Series cover request counts, request duration, in-flight requests, and the audit-health series below.

Audit health:

- Gitea operations reuse the existing audit event schema unchanged; operation names are the protocol operation names.
- A failed write of a pre-operation audit event denies the operation; the security record cannot be guaranteed, so the operation does not run.
- A failed write of a terminal audit event increments the audit-write-failure counter, emits a stderr fallback line, and marks the `audit` health name not serving until a write succeeds again.
- Health exposes the overall status and the named `audit` status. Provider reachability is deliberately absent from health; it flaps and couples broker health to Gitea availability. Reachability is doctor's job.
- Loss visibility comes from audit event counters, the write-failure counter, and a last-success timestamp gauge. The `v1` sink remains local JSONL.

## Error handling

Unknown and unauthorized repositories will remain indistinguishable to clients. Provider errors will use sanitized status messages.

Unsupported commands, flags, output formats, and entity-kind mismatches fail closed with corrective errors that name the accepted set.

Exceeding the documented limits produces sanitized limits errors that state the bound.

Detailed diagnostics will remain available only to trusted operators. Unsupported provider or client behavior will fail closed.

Audit errors will remain visible. The broker will not silently discard required security records.

GitHub and Gitea operations will use the same limits for time, size, concurrency, cancellation, and shutdown.

## Testing and validation

Automated tests will cover production behavior and security-sensitive interfaces.

The Gitea work will add:

- parser tests for approved and rejected `tea` forms.
- fuzz tests for the client parser and request validators.
- policy and capability matrix tests.
- adapter command and response contract tests.
- normalization and output-bound tests.
- anti-enumeration and sanitized-error tests.
- audit lifecycle and secret-leak tests.
- cancellation, race, timeout, and process-cleanup tests.
- real Git clone, allowed-push, and denied-push tests.
- containerized tests against supported Gitea versions.
- end-to-end repository, issue, and pull-request tests.
- protocol lint, breaking-change, and generated-code freshness checks in CI.
- `repowolf config doctor --json` checks in CI, including example configuration validation.

The certification oracle is a differential test harness, not production code. Its determinism requirements are:

1. Seeded containerized Gitea fixtures with pinned image digests; every run starts from identical state.
2. Read-path comparisons run both executors against the same seeded instance and compare normalized typed responses with canonical ordering.
3. Every field is classified stable or volatile in advance. Stable fields are compared exactly. Volatile fields, such as `mergeable` and wall-clock values, are checked for shape only. A field that flakes under a single executor moves to shape-only.
4. Write comparisons run on paired identical fixtures, so allocated indices match, and compare post-state through the deterministic read path. Creation timestamps are shape-checked only.

Existing GitHub, Git, integration, Nix, OCI, and release tests will remain regression gates.

## Release success criteria

The first production Gitea release must meet these conditions:

- One broker instance serves configured GitHub and Gitea repositories.
- Agent sandboxes receive no provider credential or SSH identity.
- The restricted `tea` client supports the documented operation matrix.
- Git policy behavior is consistent across GitHub and Gitea.
- Existing GitHub behavior remains unchanged.
- Operators can validate and diagnose configuration before cutover.
- Audit records cover accepted, denied, completed, cancelled, and failed operations.
- The certification matrix is green on the certified Gitea versions, and `repowolf version` reports those versions.
- No production artifact contains a `tea` binary.
- Documentation claims support only for versions with a green certification run.

## Main risks and mitigations

### Premature abstraction

The second provider can tempt the design toward a broad generic forge model. The roadmap limits shared interfaces to proven seams: the closed provider switch and the executor interface, each justified by two real implementations.

### Provider behavior differences

Gitea and GitHub differ in fields, state transitions, and CLI behavior. Provider-specific typed messages and adapters keep these differences local. The oracle diff harness catches behavior changes across Gitea versions.

### go-sdk dependency risk

The native adapter adds a pinned third-party module. Version pinning, hash verification, supply-chain review, and oracle diffing against the pinned `tea` binary limit this risk.

### Write-capability expansion

Issue, pull-request, and Git writes increase the effect of a compromised agent. Exact repositories, explicit capabilities, limits, and audits remain mandatory.

### Client version drift

A `tea` update can change command behavior. The compatibility claim is pinned to `tea 0.15.1` semantics, the grammar is a documented subset, and the pinned binary verifies behavior in certification.

### Git regression risk

General provider resolution changes a security-sensitive module. Existing Git protocol and leak tests remain required for each milestone.

## Deferred design topics

These topics are deliberately excluded from this roadmap. Each needs its own design.

- **Transactional configuration, token, and certificate reload.** The runtime is an immutable startup snapshot; reload needs its own contract set covering configuration versioning, in-flight request semantics, credential and TLS rotation, and audit continuity. Until then, a configuration, token, or certificate change means a broker restart. Restarts are safe by design: a 30-second shutdown grace, clean cancellation of in-flight operations, and reconnecting clients. Multi-broker deployments use rolling restarts.
- **Interactivity.** The no-interactivity rule (D5) is likely to be revisited when humans and agents collaborate in the same sandbox. A future design could add a client-side-only guided flow that collects all parameters before building the typed request, with no server change. This release keeps the hard rule: no prompts, no editors, no TTY-conditional behavior.
- **Other documented exclusions:** subpath endpoints, stdin request bodies, milestone and deadline flags, agit flow, `csv`/`tsv`/`yaml` output, `--kind` on issue lists, the `diff`/`patch`/`ci` pull fields, richer label and milestone objects, multi-index mutations, `comments list/edit/delete`, and repository enumeration. Each was excluded with a reason and can be added later as an explicit support change.

## Decision record

This revision folds in twenty-three approved decisions:

1. Use a bounded compatibility matrix.
2. Support Gitea `1.26.x` and `1.27.x`.
3. Initially certify Gitea `1.26.4` and `1.27.2`.
4. Test and certify later patches before claiming support.
5. Maintain a rolling window of two Gitea minor lines.
6. Document removal of a supported minor line as a support change.
7. Pin `tea` to exactly `0.15.1`.
8. "`tea`-compatible" means a documented subset of `tea 0.15.1` commands and flags with matching meanings.
9. RepoWolf owns normalized output, errors, and limits; no full drop-in `tea` compatibility.
10. Output contract: lists default `table`, details and mutations default `simple`, `--output json` is the stable machine format, `--fields` uses per-operation allowlists, `csv`/`tsv`/`yaml` are rejected, and output never depends on TTY detection.
11. Command surface per the contract, with deviations D1–D6.
12. Per-command keep and drop flag lists, recorded explicitly.
13. Global flags and output shapes: required `--repo`, restricted `--output`, `--fields` replace semantics, typed JSON, RepoWolf-owned detail `simple`, additive-only JSON stability.
14. Edit and mutation semantics E1–E7.
15. Pagination and limits P1–P4 and L1–L4, including the accepted 1 MiB message-cap consequence.
16. Typed-field catalog T1–T6.
17. SDK-native production adapter; pinned `tea` 0.15.1 as the test-only certification oracle; no production `tea` fallback.
18. Endpoint, TLS, credential, login, and repository-selection rules N1–N5.
19. Runtime composition R1–R5 with the closed provider switch.
20. Packaging K1–K4 and real-adapter milestone gates G1–G3.
21. Protocol compatibility rules PR1–PR5.
22. Diagnostics, metrics, and audit-health contracts DG1–DG3.
23. Transactional reload split into a separate future design; restart is the `v1` mechanism.

## Recommended priority

1. Create the provider contract fixtures and finish the proof-of-contract phase.
2. Deliver the read-only vertical slice with the SDK-native adapter.
3. Add controlled collaboration writes.
4. Add multi-provider diagnostics, metrics, and the published certification matrix.
5. Consolidate proven seams and evaluate GitLab.
