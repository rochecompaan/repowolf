# Gitea provider runtime and credential isolation design

**Date:** 2026-09-02

**Status:** Draft for review

## Context

The Gitea roadmap requires one isolated runtime instance for each provider record. The current runtime constructs one GitHub adapter and one shared child environment.

The current configuration accepts only GitHub providers. Provider records do not name their credential environment variables.

Principal authentication tokens come from `Principal.TokenEnvs`. The runtime loads them through `auth.Load` and rejects duplicate principal token values.

The provider child environment removes configured principal tokens and `REPOWOLF_` variables. It preserves ambient `GH_` and `GITHUB_` variables.

The runtime always resolves both `gh` and `ssh`. The server always registers the GitHub service when a policy snapshot exists.

These assumptions prevent safe Gitea-only deployments and multiple isolated GitHub records. They also let unrelated ambient GitHub variables reach provider children.

## Outcome

RepoWolf constructs isolated runtime state for each provider record without changing GitHub request behavior.

This issue provides the configuration and credential foundation for Gitea Git access. It does not add a Gitea API client or operation.

## Applicable roadmap decisions

This specification applies these roadmap decisions:

- **DR-06:** Reject case-folded Gitea repository slug collisions.
- **DR-10:** Construct one immutable runtime instance for each provider record through a closed kind switch.
- **DR-11:** Bind one startup-loaded credential to each provider record and reject credential reuse.
- **DR-12:** Render one GitHub environment for each record and one token-free SSH environment.
- **DR-13:** Keep one narrow legacy-token migration rule for a single GitHub provider.
- **DR-29:** Measure GitHub parity through observable status, output, commands, environments, policy, status mapping, and audit order.

The authoritative cross-cutting contract remains `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`.

## Existing behavior and affected modules

### `internal/config`

`config.Provider` contains the provider kind, API host, Git host, SSH user, and SSH port. `ProviderGitHub` is the only supported kind.

The strict YAML decoder rejects unknown fields. Repository owner validation uses the existing GitHub grammar for every provider.

Principal token environment names must match `REPOWOLF_TOKEN_[A-Z0-9_]+`. Their names must be unique across principals.

### `internal/auth`

`auth.Load` reads principal tokens from the process environment. It validates RepoWolf token format and indexes token digests by principal.

The loader rejects missing, empty, malformed, and duplicate principal tokens. It does not know about provider credentials.

### `internal/runner`

`runner.ProviderEnvironment` removes explicit principal token names and variables with the `REPOWOLF_` prefix.

`runner.ResolveTools` always resolves `gh` and `ssh`. It cannot represent a Gitea-only runtime without `gh`.

### `internal/app`

`app.NewRuntime` constructs one GitHub adapter. That adapter receives one shared provider environment.

The runtime has no provider-instance index. It exposes one GitHub adapter and one provider environment.

### `internal/server`

`server.Options` requires the policy snapshot and GitHub executor together. A policy snapshot therefore causes GitHub service registration.

### `internal/policy`

The policy snapshot keeps the trusted provider record for every repository. `ResolvedRepository.Repository.Provider` contains the trusted provider ID.

The client cannot select this provider ID. Runtime dispatch can use it without trusting client input.

## Approaches considered

### Private provider-instance map with a closed kind switch

This is the selected approach.

`internal/app` owns one private map keyed by provider ID. It constructs each record in sorted order through a closed switch on provider kind.

A small credential module loads every principal and provider token once. It returns an immutable snapshot with narrow lookup operations.

This approach keeps composition local to startup. It supports multiple records without creating a provider plugin system.

### One shared GitHub adapter with request-time environment selection

This approach keeps the current adapter count. Each request selects and attaches a credential before process creation.

The adapter then holds mutable or request-specific credential state. This shape makes cross-record leaks easier and weakens startup validation.

This approach is rejected.

### Generic provider registry

A registry maps provider kinds to constructor interfaces. It supports later providers without edits to startup code.

Only GitHub has a complete adapter in this issue. A registry creates an unproven interface and unnecessary extension points.

This approach is rejected by DR-10.

## Configuration contract

### Provider kinds and fields

The configuration adds `ProviderGitea` with the value `gitea`.

Each provider record contains these fields:

| Field | GitHub | Gitea | Rule |
|---|---|---|---|
| `kind` | Required | Required | Accept only `github` or `gitea`. |
| `apiHost` | Required | Required | Accept one host-only value. |
| `gitHost` | Required | Required | Accept one host-only value. |
| `sshUser` | Required | Required | Use the existing validated SSH-user grammar. |
| `sshPort` | Optional | Optional | Default to 22 and reject zero. |
| `tokenEnv` | Required for new records | Required | When present, require a non-empty YAML string that matches `REPOWOLF_TOKEN_[A-Z0-9_]+`. |

Issue 4 adds `caFile` with trust-root loading. Issue 1 does not accept or process `caFile`.

The configuration API version remains `repowolf.dev/v1alpha1`. The new kind and field are additive changes to the alpha configuration.

An explicit provider record has this form:

```yaml
providers:
  github-main:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    sshPort: 22
    tokenEnv: REPOWOLF_TOKEN_GITHUB_MAIN
  gitea-lab:
    kind: gitea
    apiHost: gitea.example.com
    gitHost: gitea.example.com
    sshUser: git
    sshPort: 2222
    tokenEnv: REPOWOLF_TOKEN_GITEA_LAB
```

The configuration stores credential names only. It never stores credential values.

### Credential-name validation

Every explicit provider `tokenEnv` must match the existing principal token-name grammar.

An explicit empty string or YAML null is invalid. Neither value activates legacy migration.

A token environment name can occur only once across all principals and provider records. Configuration validation rejects a duplicate before environment lookup.

A Gitea provider without `tokenEnv` is invalid. More than one GitHub provider without `tokenEnv` is invalid.

Only syntactic omission on one GitHub provider can activate the legacy migration below.

### Provider-specific repository names

GitHub repository validation remains unchanged.

A Gitea owner or repository name uses this grammar:

```text
[A-Za-z0-9][A-Za-z0-9._-]{0,99}
```

The complete names `.` and `..` are invalid. A Gitea repository name cannot end with `.git`.

Validation selects the owner and repository grammar from the referenced provider kind. It keeps the configured casing for upstream use.

### Gitea slug collisions

The restricted Gitea selector is `<owner>/<name>`. It does not include a provider ID or host.

Configuration validation derives `lower(owner)/lower(name)` for every Gitea repository. It rejects duplicate derived values across all Gitea provider records.

This rule rejects same-record duplicates, cross-instance duplicates, and case-only duplicates. It does not compare GitHub repositories with Gitea repositories.

The error identifies the conflicting repository IDs. It does not include credentials or other provider state.

## Credential snapshot

A new `internal/credentials` module owns named credential-source lookup and token interpretation. No other startup module performs named token lookups.

The module has one small interface:

- `Load` accepts the validated configuration and one environment lookup function.
- `AuthIndex` returns the immutable principal authentication index.
- `ProviderToken` returns the credential bound to one trusted provider ID.
- `EnvironmentNames` returns a sorted copy of every loaded credential name.
- `UsesLegacyProviderToken` reports legacy provenance without returning a credential.

The snapshot does not expose a token map or token enumeration operation. It never returns a mutable internal slice.

### Loading rules

The loader resolves token sources in deterministic provider and principal order. It performs exactly one named lookup for each distinct credential source.

Principal tokens keep the existing RepoWolf token-format validation. Provider tokens require a present, non-empty value.

Provider token values are otherwise opaque. The loader does not trim them or probe a provider identity.

The loader computes a SHA-256 digest for every raw token value. A digest can occur only once across principals and provider records.

Duplicate detection includes tokens within one principal, across principals, across providers, and between a principal and provider.

The snapshot binds each provider credential to the configured provider ID. One provider record can serve multiple repositories.

### Legacy GitHub migration

The legacy rule applies only when exactly one GitHub provider syntactically omits `tokenEnv`.

| `GH_TOKEN` | `GITHUB_TOKEN` | Result |
|---|---|---|
| Present and non-empty | Absent | Use `GH_TOKEN`. |
| Absent | Present and non-empty | Use `GITHUB_TOKEN`. |
| Absent | Absent | Stop startup. |
| Present | Present | Stop startup, even when one value is empty. |
| Present but empty | Absent | Stop startup. |
| Absent | Present but empty | Stop startup. |

Explicit provider `tokenEnv` values disable legacy selection for that provider. Ambient legacy variables are still removed from child environments.

The snapshot records the selected legacy source. Issue 1 does not add the future doctor warning.

### Credential errors

Credential errors can identify provider IDs, principal IDs, and environment variable names. They never include token values or token digests.

A missing, empty, malformed, or duplicate credential stops startup before listener binding. RepoWolf has no degraded credential mode.

## Child environment contract

Environment rendering remains in `internal/runner`. The module provides a token-free base and a GitHub-specific derivative.

### Token-free base

The renderer starts from an `os.Environ()` snapshot and preserves the order and bytes of retained entries.

For a removed entry, the renderer uses only its name. It does not interpret or copy the credential value into its result.

It removes these entries:

- Every configured principal token name.
- Every explicit or effective provider token name.
- Every variable whose name starts with `REPOWOLF_`.
- Every variable whose name starts with `GH_`.
- Every variable whose name starts with `GITHUB_`.

Environment names are case-sensitive. The renderer parses each name at the first `=` character.

The SSH child receives a copied token-free base. It receives no provider-specific additions.

Safe inherited variables remain available. These variables include `SSH_AUTH_SOCK` and `GIT_PROTOCOL`.

### GitHub environment

Each GitHub adapter receives its own environment copy. The renderer removes inherited `NO_COLOR` entries from the token-free base.

It then appends these exact entries in this order:

```text
GH_TOKEN=<credential for this provider ID>
GH_PROMPT_DISABLED=1
GH_NO_UPDATE_NOTIFIER=1
NO_COLOR=1
```

The environment contains one `GH_TOKEN` and one `NO_COLOR`. It contains no token for another provider or principal.

A Gitea token remains inside the broker process. Issue 1 creates no Gitea child process.

## Runtime composition

`internal/app` remains the composition seam. It does not add a registry or public provider framework.

Startup uses this order:

1. Decode and validate the configuration.
2. Load the credential snapshot.
3. Load the server TLS configuration.
4. Resolve the tools required by configured provider kinds.
5. Build the immutable policy snapshot.
6. Render the token-free SSH environment.
7. Construct private provider instances in sorted provider-ID order.
8. Construct the Git service with the token-free environment.
9. Construct the gRPC server with only complete provider services.
10. Mark the server ready.

Any error stops construction. The listener never binds to a partially constructed runtime.

### Tool resolution

SSH remains required for every valid configuration. The broker resolves it at startup.

The broker resolves `gh` only when at least one GitHub provider exists. A Gitea-only configuration does not require `gh`.

The broker never resolves `git` or `tea`. Git remains sandbox-side tooling, and Gitea API access remains in-process.

An unused absolute `tools.gh` override can remain in a Gitea-only configuration. The runtime does not inspect its filesystem target.

### Provider instances

The private instance map is keyed by configured provider ID. Each entry keeps its provider kind, configuration, credential provenance, and concrete adapter state.

The closed switch has these Issue 1 branches:

- A GitHub record constructs one `providergithub.Adapter` with its own rendered environment.
- A Gitea record retains isolated configuration and credential state without an API adapter.
- An unknown kind stops configuration validation before the switch.

A shared `runner.Runner` can manage child process lifetime. Provider adapters do not share environment slices or credential values.

The `Runtime` type does not expose provider credentials. It can expose the selected adapters and safe environment for tests.

### GitHub dispatch

The GitHub service receives one executor that owns the GitHub adapter map. This executor still satisfies the existing `server.GitHubExecutor` interface.

After policy resolution, the executor reads the trusted provider ID from `ResolvedRepository.Repository.Provider`. It selects the matching GitHub adapter.

A missing adapter is an internal runtime error. Dispatch fails with a sanitized unavailable result and never falls back to another record.

The server registers the GitHub service only when the executor contains at least one complete GitHub adapter.

A Gitea-only runtime registers no GitHub service. Issue 1 registers no Gitea API service.

## Data flow

### Startup data flow

```text
strict YAML
  -> validated provider and repository records
  -> credential snapshot
       -> principal authentication index
       -> provider credential bindings
       -> credential-name scrub list
  -> token-free SSH environment
  -> provider instances by trusted provider ID
  -> optional GitHub executor
  -> Git service and gRPC server
  -> readiness
```

### GitHub request flow

The client request format does not change.

```text
restricted gh request
  -> GitHub service validation
  -> policy resolves a trusted repository and provider record
  -> GitHub executor selects the adapter by trusted provider ID
  -> adapter starts gh with that record's environment
  -> existing normalization and status mapping
  -> existing terminal audit order
```

### SSH child flow

The Git request format does not change in Issue 1.

```text
existing Git request
  -> existing Git policy path
  -> SSH child starts with the token-free environment
```

Issue 2 adds Gitea Git selector parsing and routing.

## Security behavior

The credential module performs one named lookup for each credential source. It retains values only in trusted process memory and required adapter state.

No provider credential enters configuration values, client requests, responses, command arguments, audit events, or SSH child environments.

A GitHub credential enters only the matching GitHub child environment. A Gitea credential enters no child environment.

Provider selection comes from the policy snapshot. Client input cannot select a provider ID, credential source, executable, or environment.

All lookup and construction errors fail closed before readiness. Runtime dispatch never substitutes a different provider instance.

## Error behavior

Configuration errors identify the invalid field and record. Collision errors identify both conflicting repository IDs.

Credential errors identify names and owning records without secret material. Duplicate-value errors do not show values or digests.

Tool errors occur only for tools required by configured kinds. A missing required tool stops startup.

Adapter-construction errors identify the provider ID and kind. They do not include the provider credential or rendered environment.

A missing runtime adapter after readiness returns a sanitized unavailable result. It does not become an authorization distinction.

## GitHub compatibility

Existing GitHub commands, output bytes, status mapping, policy results, and audit order remain unchanged.

The child environment has one approved security change. Ambient GitHub variables are replaced by the exact per-record environment contract.

A single legacy GitHub provider remains usable through exactly one legacy token variable. New and updated examples use explicit `tokenEnv`.

GitHub-only, Gitea-only, and mixed configurations are valid. Each configuration requires at least one provider, repository, and principal.

Historical specifications and plans remain unchanged. Active configuration documentation and runnable examples move to explicit provider credentials.

## Documentation and example updates

Implementation updates these active surfaces when they contain a provider record:

- `docs/configuration.md`
- `README.md`
- `scripts/repowolf-dogfood.sh`
- `examples/docker/config/repowolf.yaml`
- `examples/docker/config/repowolf-host.yaml`
- Integration policy fixtures and service environments

The examples use distinct names for principal and provider credentials. They do not teach the legacy migration as the preferred configuration.

Static documentation does not need a new automated text test. Direct review and existing example validation provide sufficient evidence.

## Test strategy

### Configuration tests

Tests cover both provider kinds, explicit `tokenEnv`, strict unknown fields, conditional legacy omission, and provider-specific repository grammar.

Separate cases reject `tokenEnv: ""` and `tokenEnv: null`. These values never select ambient legacy credentials.

Tests cover Gitea collisions within one instance, across instances, and by case only. GitHub repository behavior remains unchanged.

Tests cover duplicate credential names across providers, principals, and both categories.

### Credential tests

Tests use a counting environment lookup. They prove one named lookup for each distinct source and deterministic error order.

Tests cover missing and empty provider tokens. Principal tokens keep their existing format cases.

Tests cover duplicate values in every category combination. Secret markers must not occur in returned errors.

Table tests cover every legacy `GH_TOKEN` and `GITHUB_TOKEN` combination.

### Environment tests

Tests cover prefix removal, exact-name removal, order preservation, entries with multiple `=` characters, and copied results.

Tests prove the exact GitHub suffix and unique `NO_COLOR`. They prove that SSH receives no principal or provider token.

Tests construct multiple GitHub environments and prove that no environment contains another record's token.

### Runtime and server tests

Runtime tests cover GitHub-only explicit, GitHub-only legacy, Gitea-only, mixed, and multiple same-kind provider records.

A routing test constructs two GitHub adapters with distinct provider IDs and credentials. Requests to each repository invoke only its matching adapter and environment.

A missing-adapter test returns the sanitized unavailable result. It proves that no adapter runs and no fallback occurs.

Tests prove that `gh` resolution is conditional and SSH resolution is unconditional.

Tests prove that service registration follows complete provider instances. No Gitea API service appears in Issue 1.

Startup tests prove that credential and adapter errors occur before listener binding and readiness.

### GitHub parity tests

Observable parity tests compare these values:

- Client exit status.
- Standard output and standard error bytes.
- Provider command path and arguments.
- The approved provider environment.
- Policy result.
- gRPC status mapping.
- Accepted and terminal audit order.

The environment comparison uses the new exact DR-12 contract. All other GitHub behavior remains byte-compatible.

### End-to-end demonstration

The demonstration starts a broker with one explicit GitHub record and one Gitea record.

A restricted GitHub read reaches a fake `gh` provider and returns the existing output. The fake records the exact GitHub environment.

An existing GitHub Git read reaches a fake SSH provider. The fake records a token-free environment with `SSH_AUTH_SOCK` and `GIT_PROTOCOL` retained.

A second startup attempt reuses one token value across a principal and provider. The broker rejects it before readiness without printing the value.

A live Gitea container is not applicable to this issue because Issue 1 performs no Gitea operation.

## Non-goals

Issue 1 does not add:

- A Gitea SDK dependency or Gitea API client.
- A Gitea gRPC service, request, response, or restricted `tea` command.
- Gitea Git parser changes, clone, fetch, or push.
- Provider HTTP transport or `caFile` handling.
- Token-scope discovery or identity probes.
- The future doctor warning for legacy credentials.
- Reload, credential rotation, or degraded startup.
- A provider registry, plugin interface, or dynamic loading.
- Changes to capability names or Git push policy.

## Acceptance criteria

Issue 1 is complete when all applicable criteria are true:

1. Explicit GitHub and Gitea provider records decode into the validated configuration model.
2. Only syntactic omission activates legacy migration. Explicit empty and null `tokenEnv` values are invalid.
3. New provider records use `tokenEnv`, and only one GitHub record can use the legacy migration.
4. Credential names and values are unique across principals and provider records.
5. The runtime performs one named lookup for each credential source and never includes secret material in an error.
6. Each GitHub provider record has one immutable adapter with its own exact child environment.
7. Trusted provider-ID dispatch selects only the matching GitHub adapter. Missing adapters fail without invocation or fallback.
8. SSH receives the token-free base and no provider credential.
9. A Gitea-only runtime does not require `gh` and registers no GitHub service.
10. A mixed runtime preserves current GitHub request behavior and creates isolated Gitea runtime state.
11. Gitea repository slugs are unique after case folding across all Gitea provider records.
12. Existing GitHub, Git, configuration, policy, server, integration, and leak tests pass.
13. The end-to-end demonstration proves GitHub parity, SSH isolation, and duplicate-value rejection.
14. Active documentation and runnable examples use explicit provider `tokenEnv` values.

## Open questions

None.
