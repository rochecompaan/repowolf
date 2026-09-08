# Gitea Git clone and fetch design

**Date:** 2026-09-08

**Status:** Ready for implementation planning

## Context

Issue 1 added the `gitea` provider kind, provider-instance runtime state, provider-specific repository-name validation, startup-loaded provider credentials, and a token-free SSH child environment. Gitea records still have no usable operation.

The existing Git transport supports real Git in the sandbox through `repowolf-git-ssh`, a typed bidirectional gRPC stream, the shared Git service, exact-repository policy, and a broker-side pinned SSH executable. Its client parser and server command builder assume GitHub in two important ways:

- the parser accepts only the SSH user `git` and the GitHub owner grammar;
- the Git service forces policy resolution to `ProviderGitHub`.

The shared selector carries host, optional SSH port, owner, and repository name, but not the SSH user. The broker therefore cannot prove that an untrusted authority used the configured user before regenerating the trusted upstream command.

This design is the second issue in the approved decomposition. The authoritative cross-cutting contracts remain:

- `docs/specs/2026-08-27-gitea-provider-roadmap-design.md`
- `docs/specs/2026-09-02-gitea-roadmap-issue-decomposition-design.md`
- `docs/specs/2026-09-02-gitea-provider-runtime-credential-isolation-design.md`

## Outcome

An agent can clone and fetch one permitted Gitea repository through RepoWolf. Neither the agent nor the broker-side SSH process receives the Gitea provider token.

GitHub Git clone, fetch, and push retain their existing accepted grammar, generated SSH command, stream behavior, policy enforcement, diagnostics, and audit behavior.

## Applicable roadmap decisions

This specification applies the issue-decomposition decisions:

- **ID-01:** Use one serial implementation stream.
- **ID-02:** Use thin foundations followed by vertical slices.
- **ID-03:** Complete the Git path before the Gitea API feature path.
- **ID-06:** Write specifications and plans just before implementation.
- **ID-07:** Use one current Gitea image for issue completion.
- **ID-08:** Defer broad operations and certification work until actual use creates a need.

It also follows the roadmap’s cross-cutting rules: exact-repository authorization, provider credentials outside sandboxes, shared Git limits and lifecycle, provider-specific validation, sanitized errors, safe audit records, and unchanged GitHub behavior.

## Goals

- Accept Git-generated SSH invocations for configured Gitea users, hosts, ports, owners, and repository names.
- Carry the untrusted SSH user to the broker so policy can match every authority component.
- Resolve Git repositories across configured provider kinds without allowing the client to choose a provider ID or credential.
- Route authorized Gitea `git-upload-pack` through the existing Git service, runner, limits, audit, and token-free SSH environment.
- Preserve the existing GitHub Git grammar and behavior.
- Prove clone, fetch, denial, malformed-input handling, real-Gitea interoperability, and credential non-disclosure.

## Non-goals

This issue does not add:

- Gitea `git-receive-pack` or any Gitea push path;
- Gitea API access, an SDK dependency, a `tea` client, or new Gitea RPCs;
- HTTPS Git transport;
- nested Gitea organizations beyond the configured two-component `<owner>/<repository>` selector;
- SSH keys, SSH agent forwarding, known-host provisioning, or provider token scope discovery;
- provider selection by client-supplied provider ID or provider kind;
- a provider registry, plugin interface, or separate Gitea Git service;
- broad compatibility certification or deployment documentation.

## Approaches considered

### Extend the shared Git path and resolve a trusted provider kind

This is the selected approach. The SSH shim parses a provider-neutral, bounded Git invocation. Policy resolves one exact granted repository and returns its trusted provider record. Upload-pack permits GitHub and Gitea; receive-pack remains GitHub-only.

This preserves one security-sensitive Git implementation and reuses its stream limits, process lifecycle, policy, status mapping, and audit behavior. The change is small and reversible.

### Add a separate Gitea Git service

A second service could keep the current GitHub assumptions untouched, but it would duplicate framing, limits, process handling, audit, and push-policy seams before provider behavior requires divergence. This is rejected by the roadmap’s deep shared Git-module direction.

### Put a provider kind or provider ID in the client request

The client could infer or accept provider identity and route directly. That makes untrusted input participate in provider selection and still cannot distinguish identical SSH coordinates safely. This is rejected. Provider kind is an output of trusted policy resolution, not client authority.

## Interface changes

### Repository selector

Add an additive `ssh_user` field to `repowolf.v1.RepositorySelector` using the next unused field number. Existing clients omit it.

The Git SSH shim sets `ssh_user` from the parsed SSH authority. Non-Git provider requests continue to leave it empty, and their server validation continues to reject Git-only selector fields.

For Git requests, the service requires non-empty host, SSH user, owner, and name. Port behavior remains unchanged:

- an explicit `-p` value must be decimal `1..65535` and is matched exactly;
- an omitted `-p` remains zero in the request and does not override the configured port;
- broker-side SSH always uses the configured port.

The protocol carries no provider kind, provider ID, token, executable, SSH option, or remote command.

### Git SSH parser

The parser continues to accept only the exact Git-generated shapes already supported:

```text
[-o SendEnv=GIT_PROTOCOL] [-p PORT] USER@HOST "git-upload-pack '[/]OWNER/REPOSITORY.git'"
[-o SendEnv=GIT_PROTOCOL] [-p PORT] USER@HOST "git-receive-pack '[/]OWNER/REPOSITORY.git'"
```

The existing option order, argument count and byte bounds, UTF-8 and NUL checks, single optional leading slash, quoting, `.git` suffix, and operation allowlist do not change.

The authority parser generalizes from literal `git@` to the configured SSH-user grammar already accepted by configuration:

```text
[a-z_][a-z0-9_-]*$?
```

The trailing `$` is literal and permitted only as the final character. User matching is case-sensitive. Hosts retain the existing DNS-host grammar and are canonicalized to lower case in the request. IP literals, embedded ports, extra `@` characters, and shell syntax remain rejected.

The command parser accepts the union needed by the two configured provider grammars:

- GitHub-valid owners and repository names remain accepted exactly as today;
- Gitea owner and repository components may match `[A-Za-z0-9][A-Za-z0-9._-]{0,99}`;
- complete `.` and `..` components are rejected;
- exactly two path components are required.

The client’s broader syntactic acceptance is not authorization. The broker applies the resolved provider’s stricter configured grammar and exact repository identity before process start. Existing valid GitHub inputs produce the same request fields plus `ssh_user: "git"`; rejected option, command, path, and injection forms remain rejected with the same fixed client diagnostic.

The `-G` SSH variant probe uses the same generalized authority parser. It remains a syntax-only, credential-free probe and accepts no remote command.

## Trusted repository resolution

Git requests use a Git-specific policy selector containing SSH user, host, optional port, owner, and repository name. Resolution examines only repositories granted to the authenticated principal and requiring the requested capability.

Matching rules are provider-aware:

- `ssh_user` must equal the configured `Provider.SSHUser` exactly;
- host comparison is ASCII case-insensitive because the client canonicalizes DNS names, while the configured spelling remains authoritative for upstream argv;
- a nonzero requested port must equal `Provider.SSHPort`; zero retains the existing unspecified-port behavior;
- GitHub owner and name matching remains exact and case-sensitive;
- Gitea owner and name matching is ASCII case-insensitive, and the configured casing is used upstream, consistent with the approved Gitea selector contract.

Resolution returns exactly one trusted `policy.ResolvedRepository`. Zero matches, a missing capability, or multiple matches all return the same denial. In particular, if a principal is granted cross-provider repositories with indistinguishable SSH coordinates, RepoWolf denies the ambiguous request rather than choosing by map order or falling back.

The generic API-policy behavior does not change. Provider-aware Git matching should remain a narrow policy entry point or selector mode rather than changing GitHub API repository selection accidentally.

## Git service behavior

### Upload-pack

The existing upload-pack flow remains intact:

1. Receive and validate the initial `GitOpen` frame within the initial-stream timeout.
2. Authenticate the principal and resolve one exact `git:read` repository using the Git selector.
3. Permit the resolved provider kind only when it is `github` or `gitea`.
4. Revalidate the trusted owner and repository name with the resolved provider’s grammar.
5. Generate the complete SSH command from trusted configuration:

   ```text
   <pinned ssh> -T -p <configured port> -- <configured user>@<configured host> "git-upload-pack '<configured owner>/<configured name>.git'"
   ```

6. Write the accepted audit event before process start.
7. Start the pinned SSH executable with the copied token-free environment.
8. Relay the existing bounded bidirectional stream with unchanged timeouts, cancellation, cleanup, and terminal audit behavior.

The client-supplied authority and slug select policy only. They are never copied into upstream argv.

### Receive-pack

Gitea receive-pack remains unavailable in this issue. The receive-pack path resolves only a GitHub repository with both existing `git:read` and `git:write` requirements. A Gitea target is denied before SSH process start and before any Git update bytes reach a provider.

Existing GitHub receive-pack and push-policy behavior is unchanged. Issue 3 will deliberately add Gitea receive-pack.

## Security behavior

- The agent receives only the RepoWolf client, RepoWolf bearer token, endpoint, and public CA; it receives no Gitea token or broker SSH identity.
- Issue 1’s Gitea token remains startup-loaded in broker memory but is not used for Git SSH authentication.
- The SSH child receives `runner.TokenFreeEnvironment` only. It receives no principal token, Gitea token, GitHub token, `GH_*`, `GITHUB_*`, or `REPOWOLF_*` variable.
- Broker-side SSH authentication continues to come from operator-controlled SSH configuration or agent state outside the sandbox. This issue does not render credentials into argv or environment.
- Policy, not client input, supplies provider kind, canonical owner/name, upstream host, SSH user, port, executable, and remote command.
- Unknown, unauthorized, wrong-kind, ambiguous, and missing-capability requests are indistinguishable to the client and start no SSH process.
- Malformed parser and protocol inputs fail closed. They cannot become flags, shell fragments, extra path components, or remote commands.
- Audit events contain provider kind and configured repository ID after resolution, but never tokens, environments, argv, pack data, or raw SSH stderr.

## Error behavior

Client parser and probe errors keep the fixed `repowolf git transport failed` diagnostic and current exit behavior.

The server retains existing Git terminal categories. Malformed frames and invalid selectors map to invalid request; policy failures map to permission denied; process, timeout, size, cancellation, and unavailable outcomes retain their existing categories.

A denied Gitea push is a permission denial, not an unsupported raw-command escape hatch. Provider stderr remains unavailable to the client and audit sink.

No error includes a provider ID, configured repository list, credential name or value, generated SSH command, process environment, or raw provider detail.

## Compatibility constraints

- Existing GitHub scp-style and `ssh://` clone, fetch, and push forms continue to work.
- Existing GitHub default-port wildcard behavior and explicit-port matching remain unchanged.
- Existing valid GitHub parser cases, fuzz seeds, stream framing, gRPC methods, terminal categories, generated argv, client diagnostics, policy outcomes, and audit ordering remain regression gates.
- The Protobuf change is additive within `v1`; existing field numbers and meanings do not change, and generated code changes only through `scripts/generate.sh`.
- For protocol compatibility, an existing client that omits `ssh_user` retains the legacy meaning `git` for GitHub resolution only. An omitted user never selects a configured non-`git` user or a Gitea repository. New clients always send the parsed user.
- No Gitea token is passed to SSH even when a mixed runtime also serves GitHub.

## Affected components

- `proto/repowolf/v1/common.proto` and generated Go: add the SSH-user hint.
- `internal/client/gitssh`: generalize authority and slug parsing, probes, compatibility tests, relay assertions, and fuzz seeds.
- `internal/policy`: add SSH-user and provider-aware Git repository resolution while preserving API resolution behavior.
- `internal/gitservice`: permit Gitea only for upload-pack, validate trusted names by provider kind, generate existing trusted SSH commands, and keep receive-pack GitHub-only.
- `integration` and `internal/testutil`: add mixed GitHub/Gitea fixtures, fake-SSH assertions, real Git clone/fetch coverage, denials, leak checks, and one pinned Gitea-container path.
- CI/Nix test wiring only as needed to run the pinned Gitea integration test. No production Gitea API or `tea` dependency is added.

## Verification strategy

All proposed automated tests pass the Patchmill Testing Value Gate: they exercise reusable parsing, authorization, protocol, security-boundary, and provider-interoperability behavior and can fail on meaningful regressions.

### Parser and protocol tests

- Accept Git-generated scp and `ssh://` upload-pack forms with `git` and at least one non-default configured user, default and explicit ports, GitHub names, and Gitea names containing dots or underscores.
- Preserve every existing accepted GitHub parser and installed-Git compatibility case.
- Reject invalid users, hosts, ports, option order, extra options, nested paths, dot components, malformed quotes, missing `.git`, unsupported commands, injection text, NUL, invalid UTF-8, excessive argv, and excessive bytes.
- Extend parser fuzz seeds with Gitea-valid names and configured users while retaining all current seeds.
- Prove the opening frame carries `ssh_user` and that non-Git request validation rejects it where applicable.
- Run `buf lint`, generated-code freshness, and existing protocol tests.

### Policy and Git-service tests

- Resolve GitHub and Gitea repositories by trusted provider kind from the full SSH selector.
- Prove exact SSH-user and explicit-port matching, case-insensitive host matching, GitHub case-sensitive slugs, Gitea case-insensitive slugs with configured casing upstream, and denial on ambiguity.
- Prove Gitea upload-pack requires `git:read`, generates argv only from trusted Gitea configuration, and records provider `gitea` in accepted and terminal audit events.
- Prove unknown repository, unauthorized repository, wrong user/host/port, missing capability, and ambiguous coordinates produce the same denial and start no process.
- Prove Gitea receive-pack is denied before process start and before provider input, while existing GitHub receive-pack tests remain unchanged.
- Retain stream limits, timeout, cancellation, terminal-delivery, audit-failure, and process-reaping tests.

### End-to-end integration

Extend the real Git integration fixture with a granted Gitea repository and a distinct SSH user/host/port. Using the real Git executable and `repowolf-git-ssh`:

1. clone the seeded repository through RepoWolf;
2. add a commit to the upstream fixture outside the checkout;
3. run `git fetch` and prove the new object/ref is available;
4. attempt an unauthorized or ungranted Gitea repository and prove no SSH process starts;
5. attempt Gitea push and prove no receive-pack process starts;
6. exercise malformed SSH arguments directly and prove no gRPC/provider activity;
7. scan client stdout/stderr, audit, fake-SSH argv/stdin/stdout/stderr/environment, and checkout-visible channels for principal and provider credential markers.

The SSH environment assertion must show the Gitea provider token as unset. The client/sandbox environment and artifact boundary must also show that token absent.

### Pinned Gitea demonstration

Run one current digest-pinned Gitea image, create one repository and seed content through operator-side fixture setup, and provision broker-side SSH authentication plus verified host identity. Do not use a Gitea API from RepoWolf production code.

Clone and fetch that repository using the released client shape and the real broker Git service. Inspect the sandbox/client environment and the broker-side SSH process environment to prove the provider token is absent. Assert `git.upload-pack` accepted/completed audit events identify provider `gitea` and the configured repository, without secret or pack-data leakage.

The pinned-image test is the issue-completion interoperability gate. A broad Gitea version matrix remains deferred under ID-08.

### Regression commands

The implementation plan must include focused package tests, the real Git integration tests, the pinned Gitea test, and the repository’s standard gates:

```text
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
git diff --check
```

Use the existing Nix/CI verification relevant to changed integration wiring. Static fixture or workflow text does not need tests that merely restate configuration; validate it through syntax checks, container startup, and the behavioral demonstration.

## Acceptance criteria

1. A permitted Gitea repository can be cloned and fetched through real Git, the RepoWolf SSH shim, TLS gRPC, policy, and the existing broker Git service.
2. The parser accepts configured SSH users and the approved Gitea owner/name grammar without weakening option, command, quoting, path, or input bounds.
3. The broker matches SSH user, host, optional port, owner, and repository name, then obtains provider kind and canonical upstream values only from trusted policy.
4. Gitea `git-upload-pack` requires `git:read` and uses the configured SSH executable, user, host, port, owner, and repository name.
5. Unknown, unauthorized, wrong-authority, missing-capability, and ambiguous requests are indistinguishable and start no SSH process.
6. Gitea receive-pack remains denied before provider input; no Gitea push support is introduced.
7. The agent and broker-side SSH process receive no Gitea provider token, principal token, or unrelated provider credential.
8. Git stream limits, timeouts, cancellation, process cleanup, terminal categories, and safe audit ordering remain unchanged.
9. Existing GitHub clone, fetch, push, parser, policy, argv, integration, leak, and audit tests pass unchanged or with only the additive SSH-user assertion.
10. One current digest-pinned Gitea image passes the clone-and-fetch demonstration.
11. No Gitea API, SDK, `tea`, provider registry, HTTPS Git, or deployment/certification expansion is added.

## Open questions

None.
