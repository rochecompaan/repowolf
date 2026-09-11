# Secure Gitea API transport design

**Date:** 2026-09-10

**Status:** Approved for implementation planning

## Context

Issue 1 added validated Gitea provider records, startup-loaded provider credentials, and one private runtime instance per provider ID. A Gitea instance currently retains configuration and a token but has no API client.

RepoWolf now needs the smallest API foundation that later Gitea operations can use safely. This issue creates and verifies the SDK and HTTP transport boundary only. It does not expose a Gitea operation to a sandbox.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`. This design applies decomposition decisions ID-01, ID-02, ID-03, ID-06, ID-07, and ID-08 without introducing an exception to that roadmap.

## Outcome

At startup, RepoWolf constructs one pinned Gitea SDK client for every validated Gitea provider record. Each client can reach only that record's HTTPS API authority with that record's token, configured trust roots, a hard deadline, strict redirect rejection, and an 8 MiB decoded-response limit.

A controlled TLS fake-server demonstration calls a Gitea SDK endpoint and proves those properties without adding gRPC messages or a restricted `tea` command.

## Approaches considered

### Dedicated `internal/providerhttp` transport plus a narrow Gitea client constructor

This is the selected approach. `internal/providerhttp` owns the reusable outbound HTTP security controls, while `internal/provider/gitea` supplies Gitea-specific endpoint and SDK construction. `internal/app` remains the closed composition root.

This keeps credentials and network policy below future operations without creating a provider registry or generic forge API.

### Configure only the SDK's built-in HTTP client

This is smaller, but it couples security behavior to SDK internals and makes exact-authority authentication and decoded-body limits difficult to prove independently. It is rejected.

### Add a broad provider-client abstraction now

A provider-neutral API could hide GitHub and Gitea behind one interface. No shared API operation exists yet, so the abstraction would be speculative and contrary to the roadmap's vertical-slice rule. It is rejected.

## Existing behavior and affected components

- `internal/config` validates host-only provider endpoints and Gitea `tokenEnv`, but does not accept `caFile`.
- `internal/credentials` binds one startup-loaded token to each trusted provider ID.
- `internal/app/providers.go` creates GitHub adapters and leaves Gitea records as inert runtime state.
- `internal/clientconfig` has broker-client TLS helpers, but its endpoint and trust semantics are client-facing and must not become the provider transport boundary.
- `go.mod` declares Go 1.25.10 and has no Gitea SDK dependency.
- Nix development, package, check, and OCI builds currently follow the unqualified nixpkgs Go toolchain. CI reads the Go version from `go.mod`; release builds run through the Nix development environment and GoReleaser.

The implementation affects `go.mod`, `go.sum`, Nix inputs and package definitions, CI/release verification, provider configuration, runtime composition, and new `internal/providerhttp` and `internal/provider/gitea` packages. It does not change Protobuf files, server registration, capabilities, audit events, or client binaries.

## Dependency and toolchain contract

The repository moves to Go 1.26 across all build paths:

- `go.mod` is the source of truth for the Go 1.26 language/toolchain requirement.
- The flake and devenv select the Go 1.26 package explicitly rather than inheriting whichever `pkgs.go` is current.
- Nix server and client packages use the matching Go 1.26 module builder and refresh their fixed dependency hashes.
- CI continues to install from `go.mod` and verifies the resolved major/minor version.
- OCI builds inherit the Nix-built server, and release smoke and GoReleaser builds run with the same Go 1.26 toolchain.

The production dependency is pinned to exactly `code.gitea.io/sdk/gitea v0.25.1` in `go.mod` and `go.sum`. No `tea` executable or production fallback is added.

## Configuration contract

A Gitea provider accepts one optional field:

```yaml
providers:
  gitea-lab:
    kind: gitea
    apiHost: gitea.example.com
    gitHost: gitea.example.com
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITEA_LAB
    caFile: /run/repowolf/gitea-ca.pem
```

`caFile` names a PEM bundle that augments the system trust roots for that provider. Omission uses system roots only. An explicitly empty or null value is invalid. A configured file is loaded during runtime construction, is bounded to 1 MiB, must resolve to a regular readable file, and must contain at least one valid CA certificate.

`caFile` is rejected on GitHub records in this issue because GitHub remains a subprocess adapter and would not consume the field. This prevents an accepted but ineffective security setting.

Existing endpoint rules remain unchanged: `apiHost` is a host-only value, HTTPS and port 443 are implied, and user info, ports, paths, queries, fragments, and plain HTTP are not configurable. There is no skip-verification or server-name override.

Configuration, token, and CA changes continue to require a broker restart.

## Transport boundary

`internal/providerhttp` constructs a bounded `*http.Client` from immutable options. It owns all provider-token injection and outbound HTTP security behavior. It must be independently testable with an injected base `http.RoundTripper`.

### Exact-authority authentication

The expected authority is fixed when the client is constructed. Before any network round trip, the transport verifies that the request:

1. uses `https`;
2. has no URL user information;
3. has the exact configured normalized authority, including the effective port; and
4. contains no pre-existing `Authorization` header.

DNS host comparison is ASCII case-insensitive; IP literals compare by canonical address; the effective HTTPS port is 443. Any different host or port is rejected before the base transport is called.

For an accepted request, the wrapper clones the request and headers, sets exactly `Authorization: token <provider-token>`, and forwards the clone. The original request is not mutated. Tokens are never placed in URLs, query parameters, logs, or errors.

The Gitea SDK is constructed without SDK-managed authentication. `providerhttp` is the sole owner of the authorization header, ensuring that SDK behavior cannot attach a token before the authority check.

### TLS and trust roots

The transport starts with the system certificate pool and appends certificates from the provider's optional `caFile`. It requires TLS 1.3 or newer and uses the configured API host for normal hostname verification.

Invalid roots, certificate failures, hostname mismatches, and unavailable system roots fail closed. There is no insecure mode. Loading roots and creating clients performs no network request or identity probe.

### Redirects

The HTTP client rejects every redirect in `CheckRedirect`, including same-authority redirects and redirects produced by status codes 301, 302, 303, 307, or 308. Redirect rejection is returned as a stable transport error; callers never receive a redirect response as a successful SDK result.

This intentionally disallows endpoint discovery and prevents credentials or request bodies from being replayed to an unapproved location.

### Deadlines

Every Gitea HTTP client has a hard two-minute whole-operation timeout covering connection establishment, TLS negotiation, request upload, response headers, and response-body reads. A shorter caller context wins. Cancellation closes in-flight response bodies and propagates through the SDK.

The timeout is a hard Gitea transport cap, not a new configuration field. Future server operations will also retain the existing request-context deadline, so effective execution is bounded by the shorter deadline.

### Response budget

Each HTTP response body is limited to 8 MiB of bytes visible to the SDK after HTTP content decoding. The exact 8 MiB boundary succeeds; observing one additional byte returns a stable response-limit error. The limit applies equally to successful and error-status responses.

The wrapper preserves streaming reads and does not buffer an entire response. Closing the wrapped body always closes the provider body. A limit or cancellation error must leave the connection and body in a safe closed state and must not include body content or credentials in the error.

## Gitea SDK client construction

`internal/provider/gitea` owns a narrow constructor that:

- derives the production SDK base URL as `https://<apiHost>/`;
- receives the provider's token only to configure `providerhttp`;
- gives the SDK the bounded HTTP client;
- does not enable SDK-managed authentication; and
- returns the concrete SDK client needed by later Gitea adapters.

A package-private constructor accepts an explicit HTTPS base URL and transport options for controlled tests. The exported production path accepts only the validated host-only configuration, so this test seam does not add custom-port or subpath endpoint support.

`internal/app` extends its existing sorted closed-kind switch. Each Gitea `providerInstance` stores its own constructed SDK client rather than inert raw-token state. GitHub construction and trusted provider-ID dispatch remain unchanged.

If any Gitea client's configuration, CA bundle, transport, or SDK construction fails, runtime construction stops before listener binding and readiness. Startup does not contact Gitea, so provider availability and token validity remain lazy concerns.

No global default SDK client, mutable transport, cross-provider token map, registry, or fallback client is introduced. Shared immutable system-root material may be copied, but authority, token, redirect policy, deadline, and response accounting remain bound to the individual provider client.

## Data flow

### Startup

```text
strict YAML
  -> validated Gitea provider record, tokenEnv, and optional caFile
  -> credential snapshot resolves token by trusted provider ID
  -> providerhttp loads system plus provider trust roots
  -> provider/gitea constructs bounded HTTP client and pinned SDK client
  -> app stores one client in that provider instance
  -> all provider instances complete
  -> broker becomes ready
```

### Future operation

```text
trusted repository resolution
  -> provider ID selects its runtime instance
  -> typed Gitea adapter method calls that instance's SDK client
  -> providerhttp checks scheme and exact authority
  -> providerhttp injects only that instance's token
  -> TLS request with redirect, deadline, and response bounds
  -> SDK decodes a bounded response
```

This issue tests the second flow directly through an SDK call but does not expose it through RepoWolf's gRPC server.

## Error and security behavior

Construction errors may identify the provider ID, invalid field, and CA file path. They never include token values, token digests, authorization headers, or response bodies.

Runtime transport errors distinguish safe operator-relevant classes such as authority rejection, redirect rejection, timeout/cancellation, TLS failure, and response-limit exhaustion through stable sentinel errors suitable for later sanitized status mapping. They do not expose an untrusted redirect location or provider response content.

A request rejected for scheme, authority, user information, or existing authorization never reaches the base transport. There is no fallback to system default clients or another provider instance.

## Compatibility constraints

- Existing GitHub commands, environments, routing, output, status mapping, and audit order remain unchanged.
- Existing Git and SSH behavior remains unchanged.
- The configuration API remains `repowolf.dev/v1alpha1`; `caFile` is an additive Gitea-only field.
- Existing Gitea provider configurations without `caFile` remain valid and use system trust roots.
- No Gitea service is registered and no protocol surface changes.
- No support claim expands beyond the roadmap's Gitea compatibility policy.

## Verification strategy

Automated tests are warranted because authority checks, credential handling, TLS, redirects, timeouts, and response limits are reusable security behavior.

### `internal/providerhttp` tests

Table and fake-round-tripper tests prove:

- only normalized exact-authority HTTPS requests reach the base transport;
- wrong scheme, host, effective port, user information, and pre-existing authorization fail before dispatch;
- accepted requests receive exactly one token header without mutating the caller's request;
- errors and request diagnostics contain no token;
- all redirect status classes are rejected, including same-authority redirects;
- system roots are used by default and a private CA is added rather than replacing them;
- empty, malformed, oversized, missing, unreadable, and non-regular CA inputs fail closed;
- TLS 1.3 and hostname verification are enforced;
- a stalled server hits a short injected test deadline and caller cancellation wins;
- exactly 8 MiB of decoded body succeeds and 8 MiB plus one byte fails;
- success and error responses share the budget and body closure propagates.

### SDK and runtime tests

A controlled TLS server presents a certificate from a test CA and implements the SDK's version endpoint. A client built through `internal/provider/gitea`'s package-private test seam calls that endpoint; the production constructor remains host-only. The server records the request authority and exact token header.

Additional cases make that endpoint redirect, stall, exceed the response budget, use an untrusted certificate, and target a mismatched authority. Tests prove strict failures and absence of token disclosure.

Client-construction tests create two clients with distinct authorities, tokens, and CAs. Each reaches only its matching fake endpoint and sends only its matching token. Runtime tests prove that two Gitea records retain distinct clients, invalid construction prevents readiness, a Gitea-only runtime still requires no `gh` executable, and mixed-runtime GitHub behavior remains unchanged.

### Build verification

Direct verification, rather than tests that restate version files, proves the mechanical toolchain update:

- resolved Go versions in local, Nix, CI, OCI, and release paths are 1.26;
- `go mod verify` succeeds and the SDK is pinned at the specified version;
- Go formatting, vet, race tests, generated-file checks, and existing integration tests pass;
- Nix flake checks, fixed dependency hashes, OCI smoke tests, and release archive smoke tests pass;
- `git diff --check` passes.

One current pinned Gitea image remains the roadmap completion check where applicable. This transport issue's useful behavior is provider-independent and is proven by the controlled TLS SDK server; no production Gitea operation or broad certification matrix is added here.

## Non-goals

This issue does not add:

- Gitea Protobuf messages, gRPC methods, server registration, capabilities, or audit operations.
- A restricted `tea` personality, parser, command, binary, oracle, or production fallback.
- Repository, issue, pull-request, identity, or startup-probe operations.
- Gitea Git clone, fetch, or push changes.
- Subpath, plain-HTTP, custom-port, skip-verify, or hostname-override endpoints.
- Token-scope discovery, provider health checks, diagnostics, reload, or certificate rotation.
- A generic provider registry, generic forge client, retry policy, or automatic request retry.
- Broad Gitea version certification.

## Acceptance criteria

1. Every Go, Nix, CI, OCI, and release build path uses Go 1.26.
2. `code.gitea.io/sdk/gitea v0.25.1` is pinned and verified; no production `tea` dependency exists.
3. Strict configuration accepts optional non-empty `caFile` only for Gitea records and loads it before readiness.
4. Every Gitea provider record owns one SDK client with its own authority, token, trust roots, and bounds.
5. Authentication is injected only after exact HTTPS authority validation and never reaches a mismatched request.
6. Optional CA certificates augment system roots; standard hostname verification and TLS 1.3 remain mandatory.
7. Every redirect is rejected without replay, fallback, or credential disclosure.
8. Gitea requests are bounded by a hard two-minute timeout and shorter caller cancellation.
9. Every decoded response body is limited to 8 MiB, with exact-boundary success and over-boundary failure.
10. A controlled TLS fake server is called through the pinned SDK and proves authentication, trust, redirects, deadlines, and response budgets.
11. Multiple Gitea provider clients cannot cross-route credentials or authorities, and any construction failure prevents readiness.
12. Existing GitHub, Git, configuration, integration, Nix, OCI, and release checks remain green.
13. No gRPC method or restricted `tea` command is added.

## Open questions

None.
