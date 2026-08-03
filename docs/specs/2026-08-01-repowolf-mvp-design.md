# RepoWolf MVP Design

**Date:** 2026-08-01

**Status:** Approved
**Repository:** `rochecompaan/repowolf`

## Summary

RepoWolf is a repository-scoped access broker for Git and forge tooling. It lets an untrusted agent use familiar `gh` and Git workflows without receiving the service operator's forge tokens, SSH keys, forge configuration, or SSH agent.

The MVP is a sandbox-agnostic network service. Restricted client shims run inside an independently managed sandbox and communicate with `repowolf serve` over gRPC, HTTP/2, and mandatory server-side TLS. Opaque bearer tokens authenticate sandbox/project principals. Strict server-side policy grants each principal explicit capabilities over exact repositories.

The governing boundary is:

> The sandbox protects the host from the agent. RepoWolf protects the forge from the sandbox.

RepoWolf does not create, inspect, register, or attest sandboxes. There is no sandbox driver protocol and no `repowolf run` command in the MVP.

## Goals

The MVP must:

- support repository-pinned GitHub operations through a restricted `gh` compatibility shim;
- support repository-pinned Git fetch and push over SSH while real Git remains inside the sandbox;
- keep provider credentials and provider CLI implementations outside the agent sandbox;
- enforce principal × exact repository × capability policy on the service;
- provide the same client/service interface for local sandboxes and Kubernetes agents;
- use a typed, versioned network protocol rather than raw command forwarding;
- preserve the current jailed GitHub broker's useful operation surface for clubhouse;
- fail closed on malformed configuration, authentication, policy, protocol, and provider input;
- bound request sizes, streams, subprocesses, time, and concurrency;
- produce durable-collector-friendly audit events without sensitive bodies or credentials;
- ship without requiring Nix while retaining first-class Nix packaging.

## Non-goals

The MVP does not provide:

- sandbox creation or lifecycle management;
- a local `repowolf run` helper;
- Unix-socket transport;
- HTTPS Git credential transport;
- arbitrary Git execution on the service;
- arbitrary provider CLI argv forwarding;
- arbitrary provider API, GraphQL, URL, header, or endpoint access;
- aliases, CLI extensions, templates, editors, browsers, or shell execution;
- pull-request merge;
- repository or organization administration;
- release administration;
- workflow dispatch or other action writes;
- organization or repository wildcards;
- repository-local trusted policy;
- mTLS, workload identity, or GitHub App token issuance;
- ACME, an online certificate authority, trust-store installation, or automatic certificate rotation;
- configuration, token, certificate, or executable hot reload;
- macOS or Windows support.

Anonymous direct network access from an agent sandbox is outside RepoWolf's authority. The sandbox platform may restrict it separately.

## Architecture

```text
Agent sandbox                         RepoWolf service
┌──────────────────────┐              ┌──────────────────────────┐
│ restricted gh        │── gRPC/TLS ─▶│ TLS and authentication   │
│ real Git             │              │ policy engine            │
│  └─ repowolf-git-ssh │◀─ streaming ─│ GitHub adapter → gh      │
└──────────────────────┘              │ Git SSH adapter → ssh    │
                                      │ subprocess runner        │
                                      │ audit writer             │
                                      └──────────────────────────┘
```

### Components

The service artifact contains:

- `repowolf serve`;
- strict configuration parsing and validation;
- bearer-token authentication;
- policy evaluation;
- versioned gRPC services;
- GitHub and Git SSH provider adapters;
- bounded provider process execution;
- safe structured audit output;
- `repowolf config validate`;
- `repowolf token generate`;
- `repowolf cert init`.

The credential-free client artifact is a multicall binary installed as:

- `gh` for GitHub-compatible commands;
- `repowolf-git-ssh` for Git SSH transport.

The client artifact contains no provider credentials, provider executable paths, service policy, or provider process runner.

## Trust model

Trusted inputs are:

- the RepoWolf service binary and host operating system;
- strict YAML policy supplied by the operator;
- configured environment variables and mounted service Secrets;
- server TLS private material;
- installed `gh` and `ssh` implementations selected at service startup;
- provider credentials available to those service-side tools;
- the service operator's logging and process supervisor.

Untrusted inputs are:

- every client and sandbox process;
- all client command arguments and request fields;
- Git remotes and checkout content;
- repository-local files and manifests;
- gRPC metadata other than a successfully authenticated bearer token;
- Git protocol bytes and provider output.

A bearer token can be copied by the sandbox that receives it. Its security property is therefore bounded authority, not non-exportability. Tokens must be scoped to one sandbox/project role and rotated or revoked when compromised.

Processes with operating-system privilege to inspect the `repowolf serve` environment are inside the trusted service boundary. Local supervisors and Kubernetes deployments must isolate the service environment from untrusted workloads.

RepoWolf does not guarantee sandbox correctness. If a sandbox exposes host provider credentials, authenticated host CLIs, SSH keys, or `SSH_AUTH_SOCK`, the agent may bypass RepoWolf.

## Configuration and policy

`repowolf serve` loads one strict YAML file at startup and creates an immutable policy snapshot. Configuration changes require restart.

Example:

```yaml
apiVersion: repowolf.dev/v1alpha1

listen: "0.0.0.0:8443"

tls:
  certificate: /run/repowolf/tls/tls.crt
  privateKey: /run/repowolf/tls/tls.key

tools:
  gh: null
  ssh: null

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git

repositories:
  clubhouse-infra:
    provider: github-public
    owner: alphaexplorationco
    name: clubhouse_infra
    git:
      denyRefs:
        - refs/heads/main
      denyDeletes: true
      maxRefUpdates: 16

principals:
  clubhouse-infra-agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_CLUBHOUSE_INFRA
    grants:
      - repository: clubhouse-infra
        capabilities:
          - repository:read
          - git:read
          - git:write
```

The example grant is intentionally partial; production principals receive only the capabilities they require.

A `null` tool path means resolve the executable once from the service startup `PATH`. An absolute path pins a deterministic executable for Nix and container deployments. The resolved canonical path remains fixed for the service lifetime. Request data never influences executable selection.

Provider entries pin the provider kind, API host, Git SSH host, and SSH user. Repositories refer to a provider entry by ID; clients cannot supply or override provider hosts, SSH users, or executable choices.

The parser rejects:

- duplicate or unknown YAML fields;
- unsupported schema versions;
- missing required fields;
- malformed listen addresses, hosts, owners, repository names, or Git refs;
- unknown providers, capabilities, or policy values;
- undefined repository references;
- ambiguous principal or repository identifiers;
- duplicate token environment variable names;
- invalid, zero, or excessive limits.

Only exact repository entries are supported. There are no owner, organization, host, or repository wildcards.

### Capabilities

The MVP capability vocabulary is:

- `repository:read`;
- `issues:read`;
- `issues:write`;
- `pull_requests:read`;
- `pull_requests:write`;
- `actions:read`;
- `statuses:read`;
- `git:read`;
- `git:write`.

Pull-request write does not imply merge. Action read does not imply workflow dispatch. Provider-specific operations may be absent where the installed provider CLI cannot implement them safely.

## Client authentication

MVP clients authenticate with opaque bearer tokens over mandatory TLS.

`repowolf token generate` emits a token with a version prefix and at least 256 bits of cryptographically secure randomness. It prints the token once and does not edit configuration.

The policy contains only environment variable names. At startup the service:

1. reads every configured token environment variable;
2. rejects missing, empty, malformed, or duplicate token values, including duplicate values assigned to the same principal;
3. computes an in-memory digest for lookup;
4. maps the digest to exactly one principal;
5. excludes token values from logs, audit events, diagnostics, errors, and provider arguments.

Provider subprocess environments are derived from the service startup environment by removing every configured RepoWolf client-token variable and RepoWolf's own internal control variables. All remaining provider-authentication variables are preserved byte-for-byte; RepoWolf does not inspect, retrieve, normalize, preflight, or mutate them. This prevents RepoWolf client credentials from being inherited by `gh` or `ssh`.

A service may temporarily configure multiple token environment variables for one principal during rotation, but each variable must contain a distinct token value. The operator restarts RepoWolf after adding or removing token sources.

Clients send their token in gRPC authorization metadata. Tokens are never accepted in URLs, request payload fields, or command-line arguments.

## TLS

TLS provides service identity, confidentiality, and integrity. Bearer tokens provide client identity and authorization. MVP does not use client certificates.

`repowolf serve` loads PEM-encoded server certificate and private-key files at startup. The files may be created by RepoWolf, cert-manager, or another operator-controlled issuer.

`repowolf cert init` is a local bootstrap command. It generates:

- an Ed25519 local CA key and certificate;
- an Ed25519 server key and certificate;
- configured DNS and IP subject alternative names;
- atomic files with restrictive private-key permissions.

It refuses to overwrite existing material. It does not install trust roots or run an online CA.

In Kubernetes, cert-manager or another platform component creates and rotates the mounted server certificate. MVP reloads updated files only after a service restart or rolling replacement.

Clients receive the public trusted CA through their sandbox platform and configure it using `REPOWOLF_CA_FILE` or the platform trust store.

## Network protocol

The protocol uses versioned Protobuf services over gRPC, HTTP/2, and TLS.

Provider operations use unary RPCs with provider-specific typed messages. Git upload-pack and receive-pack use bidirectional streaming RPCs.

Every RPC enforces:

- TLS before authentication metadata is processed;
- bearer-token authentication;
- principal and exact-repository authorization;
- operation-specific schema and semantic validation;
- deadlines and cancellation;
- global and principal concurrency limits;
- operation-specific body and result limits;
- stream byte and duration limits;
- backpressure;
- an effective hard message ceiling of 1 MiB in both directions.

Client and server implementations use Git stream chunks smaller than the hard ceiling. The ceiling bounds allocation authority and is not a preferred chunk size.

Unknown services, methods, message variants, and protocol versions fail closed.

## Operation model

Protocol operations are provider-specific while policy capabilities are provider-neutral.

For example, `github.issue.create` has a GitHub-specific request and result schema, requires `issues:write`, and is converted into a validated GitHub invocation by the GitHub adapter. A future forge provider adds its own typed messages and adapter while reusing the capability where the authorization meaning matches.

This preserves provider behavior without creating separate policy vocabularies or a lowest-common-denominator provider API.

### Supported operation surface

The MVP preserves the current GitHub broker's useful clubhouse surface through typed GitHub operations:

- exact repository metadata read;
- issue list, view, create, edit, comment, close, and reopen;
- pull-request list, view, create, edit, comment, close, reopen, readiness, and checks where supported;
- action/run list and view where supported;
- commit/check/status read where supported;
- Git upload-pack for fetch;
- Git receive-pack for push.

The server does not support merge, arbitrary API requests, GraphQL, workflow writes, repository administration, release administration, or command extensions.

## `gh` request flow

A client shim parses supported native CLI syntax for compatibility. It may obtain the requested repository from an explicit native flag or from local Git metadata. That repository is untrusted request data, not authority.

The shim sends a GitHub-specific typed RPC. The service:

1. authenticates the bearer token;
2. resolves the principal;
3. validates every typed field;
4. resolves an exact configured repository;
5. checks the required grant and capability;
6. constructs the complete provider command;
7. runs the pinned service-side tool;
8. validates and converts provider output into the typed response;
9. returns a compatible result or sanitized error;
10. emits an audit event.

Unsupported command shapes fail before provider execution. Client-side parsing improves compatibility and error messages but is not a security boundary; the service repeats all security-relevant validation.

## Git request flow

Real Git runs inside the sandbox and owns `.git`, the working tree, local objects, refs, commits, and configuration.

`GIT_SSH_COMMAND=repowolf-git-ssh` redirects only authenticated Git transport.

For fetch:

```text
git fetch
  → repowolf-git-ssh
  → authenticated GitUploadPack gRPC stream
  → policy check
  → pinned service-side ssh
  → exact git-upload-pack command
```

For push:

```text
git push
  → repowolf-git-ssh
  → authenticated GitReceivePack gRPC stream
  → policy check
  → pinned service-side ssh
  → exact git-receive-pack command
```

The service generates the SSH host and remote service command from configured repository identity. The client cannot supply SSH options, shell fragments, arbitrary hosts, or arbitrary remote commands.

### Receive-pack enforcement

The service relays the remote receive-pack advertisement to Git. Before forwarding any client update bytes to the SSH process, it buffers and validates the receive-pack command prefix.

Validation includes:

- pkt-line syntax;
- supported capabilities;
- exact object ID and ref syntax;
- maximum update count;
- denied refs;
- delete policy;
- operation and stream limits.

`refs/heads/main` is denied by default. A denied push forwards zero client update bytes to the remote receive-pack process.

Once the prefix is accepted, RepoWolf forwards the validated prefix and remaining bounded stream in order. GitHub branch protections remain authoritative for ancestry-sensitive rules that RepoWolf cannot determine without repository state.

The credential-bearing SSH process never opens or inspects the agent checkout.

## Provider execution

At startup RepoWolf resolves required `gh` and `ssh` executables once from `PATH`, unless strict YAML provides absolute overrides. Startup fails before readiness if a configured provider lacks a required executable.

Every provider request uses:

- the pinned executable path;
- server-generated argv;
- provider authentication variables unchanged;
- a private temporary working directory;
- a private process group or platform equivalent;
- bounded stdin, stdout, and stderr capture;
- operation deadlines and cancellation;
- bounded graceful termination followed by forced cleanup;
- complete child reaping.

Provider processes never receive the agent checkout. Request fields are inert data and cannot become flags, environment names, executable paths, shell code, URLs, headers, or arbitrary endpoints. Free-form titles, bodies, comments, and other content remain single inert values at service-controlled argument boundaries. The adapter uses bounded stdin or private temporary files when the provider CLI supports them; otherwise it supplies content only as the value of a fixed, generated flag.

Raw provider stderr is never returned to clients or written to audit output. It is represented by a stable sanitized failure category and safe metadata such as exit status and provider name.

## Errors

Clients receive stable sanitized categories:

- unauthenticated;
- permission denied;
- unsupported operation;
- invalid request;
- repository unavailable;
- provider failure;
- deadline exceeded;
- request or stream limit exceeded;
- service unavailable.

Unauthorized repository requests return the same permission-denied category whether or not the repository exists in policy. This prevents policy enumeration.

Errors do not contain tokens, policy details, provider argv, provider environment, raw stderr, request bodies, or Git pack data.

Client shims convert gRPC status into concise native-style stderr and nonzero exit status. Git transport failures use SSH-helper-compatible failure behavior.

## Service lifecycle and health

`repowolf serve` validates configuration, token sources, TLS material, repository grants, limits, and tool availability before accepting traffic.

It exposes the standard gRPC health service. Health RPCs remain TLS-protected, require no bearer token, and return only serving state. Readiness succeeds only after the immutable runtime snapshot is complete.

`repowolf config validate` performs strict structural, reference, and policy validation without requiring runtime Secrets. `repowolf serve` additionally validates token sources, TLS material, and tool resolution.

On shutdown RepoWolf:

1. stops accepting new RPCs;
2. gives active requests a bounded grace period;
3. cancels remaining provider operations and streams;
4. terminates and reaps provider process groups;
5. flushes audit output;
6. exits.

Configuration, token, certificate, and executable updates require restart in the MVP.

## Audit

The default audit sink is structured JSON Lines on stdout. Local deployments may collect it through journald; Kubernetes deployments may collect it through the cluster logging stack. Redirection to an operator-managed durable file is permitted. Rotation and retention are external responsibilities.

Safe audit fields include:

- timestamp;
- request ID;
- authenticated principal;
- provider;
- exact repository;
- typed operation;
- accepted, denied, completed, cancelled, or failed outcome;
- stable reason code;
- duration;
- bounded input and output byte counts;
- validated Git ref metadata and update counts when applicable.

Audit output never includes:

- bearer tokens or token digests;
- TLS private material;
- issue, comment, or pull-request bodies;
- Git pack bytes;
- process environments;
- generated argv;
- raw provider stdout or stderr;
- provider credentials.

## Packaging

The MVP publishes:

- Linux amd64 service and client binaries;
- Linux arm64 service and client binaries;
- an OCI service image containing RepoWolf, `gh`, OpenSSH, and CA certificates;
- separate Nix flake service and client packages.

The service and client remain separate build artifacts. A sandbox must receive only the client artifact.

macOS and Windows support are deferred because provider process groups, signals, Git SSH integration, and release verification require separate platform work.

## Deployment

### Local and devenv

A host supervisor such as Home Manager or a systemd user service manages `repowolf serve`.

The existing Bubblewrap-based devenv jail remains responsible for filesystem and process isolation. Its RepoWolf integration is limited to:

- including the credential-free client closure;
- putting restricted `gh` first in `PATH`;
- setting `GIT_SSH_COMMAND=repowolf-git-ssh`;
- providing `REPOWOLF_ENDPOINT`;
- providing `REPOWOLF_TOKEN`;
- providing `REPOWOLF_CA_FILE` or an equivalent trust configuration;
- keeping host provider credentials and real provider tools outside the jail.

The Unix-socket mount and embedded broker lifecycle disappear only after feature parity is proven.

### Kubernetes

A Kubernetes deployment uses:

- a ConfigMap for strict policy YAML;
- Secrets exposed as service token environment variables;
- Secrets or mounted provider configuration for service-side `gh` and SSH authentication;
- a Secret containing the TLS server certificate and key;
- a ConfigMap or Secret containing the public trusted CA for agent pods;
- a Service exposing the gRPC port;
- the RepoWolf OCI image.

Agent pods contain the client artifact and receive only their role token, endpoint, and trusted CA.

RepoWolf is stateless after startup except for private temporary provider directories and emitted audit output. Replicas can share policy and Secrets. Each Git stream remains bound to one HTTP/2 connection and replica.

## Testing and verification

### Unit and property tests

Automated behavioral tests cover:

- strict YAML parsing and startup failure;
- environment token loading, duplicate detection, authentication, rotation, and non-disclosure;
- principal × repository × capability policy matrices;
- GitHub client parsing;
- GitHub-specific Protobuf validation;
- generated provider commands and inert request data;
- provider output validation;
- error sanitization and anti-enumeration behavior;
- safe audit schemas and secret/body leak prevention;
- TLS trust and authentication failures;
- gRPC limits, deadlines, cancellation, and backpressure;
- Git pkt-line and receive-pack parsing;
- denied refs and zero update-byte forwarding;
- provider timeout, process-group termination, and reaping;
- concurrent behavior under `go test -race`.

Fuzz tests target CLI parsers, request validators, pkt-line parsing, receive-pack state machines, and stream framing because malformed input is security-sensitive.

### Integration tests

Integration verification must:

- start a real TLS-enabled gRPC server and invoke the `gh` and `repowolf-git-ssh` client modes;
- use fake `gh` and `ssh` executables to prove generated argv and avoid real credentials;
- exercise fetch and push against an offline fake Git host;
- prove exact-main denial before forwarding client update bytes;
- run the client inside a real Bubblewrap jail;
- verify no service executable, host tool, provider configuration, SSH agent, or provider credential enters the client closure;
- scan client errors and audit output for token, body, pack, environment, argv, and stderr leakage;
- build and smoke-test the OCI image;
- test Linux amd64 and arm64 release binaries.

Exact-main denial is never tested against the real GitHub main branch.

## Migration from the embedded broker

Migration is incremental and rollback-safe:

1. Implement and verify RepoWolf independently while retaining the existing broker.
2. Deploy a local RepoWolf service through Home Manager.
3. Add the RepoWolf client, endpoint, trusted CA, and role token to the clubhouse jail.
4. Run current-broker and RepoWolf parity suites.
5. Perform authenticated repository-read and fetch smoke tests.
6. Perform approved non-main write smoke tests without force.
7. Run the existing leak scans and lifecycle checks.
8. Obtain adversarial review.
9. Switch clubhouse to RepoWolf while retaining a documented rollback.
10. Remove the embedded broker only in a separate later change after stable operation.

## Acceptance criteria

The MVP is complete when:

- exact-repository GitHub, Git fetch, and Git push operations work through one TLS gRPC service;
- clubhouse retains its current approved GitHub operation surface;
- unauthorized repositories and capabilities fail closed;
- exact-main push denial forwards zero client update bytes;
- client artifacts contain no provider credentials, provider tools, or usable service-side paths;
- provider processes never inspect the agent checkout;
- request, stream, process, time, and concurrency limits are enforced;
- all accepted, denied, completed, cancelled, and failed operations produce safe audit metadata;
- leak scans find no token, provider credential, body, pack, environment, argv, or raw-stderr exposure;
- Linux amd64/arm64, OCI, Nix, Bubblewrap, and offline Git integration verification passes;
- the Go race suite passes;
- the current embedded broker remains available until migration parity and review are complete.

## Deferred evolution

The architecture intentionally leaves room for:

- Gitea typed operations, a restricted `tea` client, and Gitea service-account authentication;
- mTLS or workload identity;
- GitHub App and external secret credential providers;
- native provider API adapters behind the existing typed operations;
- certificate and configuration hot reload;
- macOS and Windows process lifecycle, Git SSH, CI, and release support;
- additional typed forge operations;
- centralized audit sinks and metrics;
- stronger distributed rate limiting;
- an optional local session helper if future evidence justifies it.

None of these are required for the MVP.
