# Gitea-First Provider Roadmap Design

**Status:** Approved

## Summary

RepoWolf will evolve from a GitHub broker into a repository-scoped forge access broker. Gitea will be the second supported provider.

The first Gitea release will support core collaboration work. Agents will use a restricted `tea`-compatible client and the existing Git transport.

Delivery will use a Gitea-first vertical slice. Shared modules will change only when the second provider proves that a seam is real.

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
- runtime assembly constructs the GitHub adapter directly.
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

## Architecture direction

### Runtime composition

`internal/app` will use a provider registry or factory seam. The runtime will construct only the adapters required by the validated configuration.

The registry will support GitHub and Gitea. It will not expose arbitrary provider plugins in the first release.

### Provider configuration

The configuration model will add a `gitea` provider kind. A Gitea provider record will pin its API host, Git host, SSH user, and SSH port.

Repository records will continue to refer to a configured provider by ID. Client input will not select executable paths, credentials, or provider hosts.

### Gitea protocol

Gitea operations will use provider-specific typed Protobuf messages. The interface will contain only approved collaboration operations.

The protocol will not carry raw `tea` arguments, raw JSON, arbitrary paths, or generic key-value request fields.

### Gitea client

The client artifact will add a restricted `tea`-compatible entry point. The parser will accept only documented command forms.

The parser will convert each command into a typed Gitea request. Output rendering will use normalized typed responses.

### Gitea adapter

The first adapter will invoke a pinned server-side `tea` executable through the existing process runner. The adapter will use bounded input, output, time, and environment controls.

A narrow executor interface will separate the Gitea service from the adapter implementation. A native HTTP adapter can use this seam in a later release.

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

## Request flow

A Gitea request will use this flow:

1. The sandbox invokes the restricted `tea` client.
2. The client parses an approved command form.
3. The client sends a typed request over TLS gRPC.
4. The server authenticates the bearer token.
5. The policy module resolves the principal, repository, and capability.
6. The Gitea adapter invokes the pinned server-side `tea` executable.
7. The adapter validates and normalizes the bounded provider response.
8. The server returns a typed response with request metadata.
9. The broker writes safe audit events for the operation lifecycle.

Raw provider credentials never enter the request, response, command arguments, or audit record.

## Roadmap

### Now: prove the Gitea contract

- Select the supported Gitea and `tea` versions.
- Define the restricted `tea` command grammar.
- Define Gitea authentication and repository-selection rules.
- Create provider contract fixtures for validation, normalization, errors, and secret-leak detection.
- Define protocol compatibility rules before adding Gitea messages.

**Exit condition:** The operation matrix and compatibility contract are stable.

### Milestone 1: read-only vertical slice

- Add Gitea configuration and adapter registration.
- Add repository, issue, and pull-request read operations.
- Add Git fetch for configured Gitea repositories.
- Add restricted `tea` parsing and response rendering.
- Add provider-aware audit records.
- Add end-to-end TLS tests with a fake Gitea adapter.

**Exit condition:** An agent can inspect and clone a permitted Gitea repository without provider credentials.

### Milestone 2: controlled collaboration writes

- Add issue create, edit, comment, close, and reopen operations.
- Add pull-request create, edit, comment, close, and reopen operations.
- Add controlled Git push with the existing Git policies.
- Validate inert input, response bounds, cancellation, and credential isolation.
- Publish Docker and native Gitea deployment examples.

**Exit condition:** The documented core collaboration operation matrix is complete.

### Milestone 3: multi-provider operations

- Add `repowolf config doctor` diagnostics.
- Diagnose credentials, tools, TLS, connectivity, and policy before cutover.
- Add provider-tagged metrics and safe operator diagnostics.
- Add audit sink health and loss visibility.
- Document combined GitHub and Gitea deployment, migration, and rollback.
- Publish a compatibility matrix for RepoWolf, Gitea, and `tea` versions.

**Exit condition:** Operators can deploy and diagnose both providers with consistent workflows.

### Later: platform leverage

- Consolidate duplication that both adapters prove.
- Publish a provider contract-test harness.
- Add transactional configuration, token, and certificate reload.
- Evaluate GitLab as the third provider.
- Reassess a provider-neutral CLI after maintenance data exists for both shims.

## Error handling

Unknown and unauthorized repositories will remain indistinguishable to clients. Provider errors will use sanitized status messages.

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
- Supported Gitea and `tea` versions pass the compatibility test suite.

## Main risks and mitigations

### Premature abstraction

The second provider can tempt the design toward a broad generic forge model. The roadmap limits shared interfaces to proven seams.

### Provider behavior differences

Gitea and GitHub differ in fields, state transitions, and CLI behavior. Provider-specific typed messages and adapters keep these differences local.

### Write-capability expansion

Issue, pull-request, and Git writes increase the effect of a compromised agent. Exact repositories, explicit capabilities, limits, and audits remain mandatory.

### Client version drift

A `tea` update can change command output or behavior. Pinned executables, compatibility tests, and a published version matrix limit this risk.

### Git regression risk

General provider resolution changes a security-sensitive module. Existing Git protocol and leak tests remain required for each milestone.

## Recommended priority

1. Prove the Gitea contract and supported versions.
2. Deliver the read-only vertical slice.
3. Add controlled collaboration writes.
4. Add multi-provider diagnostics and operations features.
5. Consolidate proven seams and evaluate GitLab.
