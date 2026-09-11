# Gitea Git Clone and Fetch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent clone and fetch an exactly granted Gitea repository through RepoWolf without giving the agent or broker-side SSH process any provider credential.

**Architecture:** Extend the existing Git SSH shim and additive v1 selector with the parsed SSH user, then add a narrow provider-aware Git resolver that returns trusted provider configuration. The shared Git service permits GitHub and Gitea for upload-pack, keeps receive-pack GitHub-only, and continues to generate the complete upstream SSH command from trusted policy. Real-Git/fake-SSH tests cover the security boundary continuously, while one digest-pinned Gitea 1.27.2 container is the interoperability gate.

**Tech Stack:** Go 1.25.10, Protocol Buffers, gRPC-Go, Buf, OpenSSH, Git, Docker, Gitea 1.27.2, Nix flakes, and GitHub Actions.

**Spec:** `docs/specs/2026-09-08-issue-6-gitea-roadmap-2-11-gitea-git-clone-and-fetch-design.md`

## Global Constraints

- Keep the protocol in `repowolf.v1`; add `RepositorySelector.ssh_user` at field number `5`, and never renumber or reinterpret existing fields.
- Generate Protobuf Go files only with `scripts/generate.sh`; never edit `gen/repowolf/v1/*.pb.go` by hand.
- Keep the accepted SSH argv shapes, option order, command allowlist, UTF-8/NUL checks, 64-argument limit, 64 KiB argv-byte limit, one optional leading slash, exact quoting, and `.git` suffix unchanged.
- Accept SSH users only with `[a-z_][a-z0-9_-]*$?`, where a literal `$` may appear only at the end; compare users case-sensitively.
- Keep DNS host parsing and lowercase canonicalization; reject IP literals, embedded ports, extra `@`, options outside the existing allowlist, and shell syntax.
- Accept exactly two repository path components. Preserve the existing GitHub grammar and additionally accept Gitea components matching `[A-Za-z0-9][A-Za-z0-9._-]{0,99}`, except complete `.` and `..` components.
- Treat client authority and slug values only as policy selectors. Provider kind, provider ID, canonical owner/name, SSH executable, host, user, port, and remote command must come from trusted startup configuration.
- Git host matching is ASCII case-insensitive; SSH user is exact; nonzero client port is exact; zero retains unspecified-port behavior.
- GitHub owner/name matching stays case-sensitive. Gitea owner/name matching is ASCII case-insensitive and upstream argv uses configured casing.
- Zero, unauthorized, missing-capability, wrong-authority, wrong-kind, and ambiguous matches all return `policy.ErrDenied` and start no SSH process.
- A selector with omitted `ssh_user` means legacy `git` only for a GitHub repository configured with user `git`; it must never select Gitea or a non-`git` provider user.
- Permit `git-upload-pack` for trusted GitHub and Gitea records with `git:read`. Keep `git-receive-pack` restricted to GitHub and its existing `git:read` plus `git:write` checks.
- Preserve existing stream limits, timeouts, cancellation, process reaping, terminal categories, fixed client diagnostic, accepted-before-start audit order, and safe terminal audit behavior.
- SSH receives only the copied `runner.TokenFreeEnvironment`; it receives no principal token, Gitea token, GitHub token, `GH_*`, `GITHUB_*`, or `REPOWOLF_*` value.
- Do not add Gitea push, a Gitea API production path, SDK, `tea`, HTTPS Git, nested organizations, provider registry, SSH key provisioning product behavior, or broad version certification.
- Prefer focused modules. Keep generic API resolution separate from provider-aware Git resolution instead of expanding `policy.Resolve` semantics.
- Testing Value Gate: all planned automated tests exercise parsing, protocol compatibility, authorization, denial-before-process, process argv, stream behavior, audit behavior, or credential isolation and can fail on meaningful regressions. Do not add tests that restate workflow YAML or the image string; validate those with workflow syntax, container startup, and the behavioral Gitea run.
- No `AGENTS.md` exists in this repository or its parent project directory. Use the standard commands required by the approved spec and mirrored by `.github/workflows/ci.yml`.
- Each implementation task ends with focused tests, `gofmt`, `git diff --check`, and a Conventional Commit.

## File Structure

### Production and generated files

- `proto/repowolf/v1/common.proto`: add the untrusted SSH-user selector hint.
- `gen/repowolf/v1/common.pb.go`: generated representation of field 5.
- `internal/client/gitssh/parse.go`: parse and return bounded user/host authority plus the union slug grammar.
- `internal/client/gitssh/probe.go`: reuse the generalized authority parser for `ssh -G` probes.
- `internal/policy/authorize.go`: retain generic API `Selector`/`Resolve` and add `GitSelector`/`ResolveGit` with provider-aware matching.
- `internal/gitservice/upload.go`: validate the expanded selector, resolve trusted provider records, validate provider-specific trusted names, and generate upload-pack commands.
- `internal/gitservice/receive.go`: request GitHub-only command resolution before reading or forwarding receive-pack input.

### Test and fixture files

- `internal/client/gitssh/parse_test.go`: configured-user, union-grammar, and malformed-input parser cases.
- `internal/client/gitssh/git_compat_test.go`: real installed-Git argv and generalized probe compatibility.
- `internal/client/gitssh/fuzz_test.go`: Gitea/user seeds while retaining existing seeds.
- `internal/client/gitssh/relay_test.go`: prove the opening frame carries `ssh_user`.
- `internal/server/github.go` and `internal/server/github_test.go`: reject the Git-only field on non-Git requests.
- `internal/policy/authorize_test.go`: provider-aware matching, ambiguity, capability, casing, and old-client compatibility.
- `internal/gitservice/upload_test.go`: Gitea upload-pack argv, capability, audit, denial, and trusted-input tests.
- `internal/gitservice/receive_test.go`: Gitea receive-pack denial before process/provider input and GitHub regression coverage.
- `integration/testdata/policy.yaml`: grant one distinct Gitea repository to the integration principal.
- `integration/testdata/fake-ssh.sh`: route trusted GitHub and Gitea commands to separate local bare repositories while recording argv, environment, and input.
- `integration/git_test.go`: real Git Gitea clone/fetch and denial/malformed/push checks.
- `integration/leak_test.go`: add Gitea clone/fetch channels and provider-token non-disclosure assertions.
- `internal/testutil/gitea.go`: focused Docker/Gitea lifecycle, operator provisioning, SSH-agent, and host-key fixture support.
- `integration/gitea_test.go`: opt-in pinned-container clone/fetch interoperability test.
- `.github/workflows/ci.yml`: run the pinned Gitea test as a behavioral CI step.

---

### Task 1: Carry configured SSH users through the bounded Git client protocol

**Files:**
- Modify: `proto/repowolf/v1/common.proto`
- Generate: `gen/repowolf/v1/common.pb.go`
- Modify: `internal/client/gitssh/parse.go`
- Modify: `internal/client/gitssh/probe.go`
- Modify: `internal/client/gitssh/parse_test.go`
- Modify: `internal/client/gitssh/git_compat_test.go`
- Modify: `internal/client/gitssh/fuzz_test.go`
- Modify: `internal/client/gitssh/relay_test.go`
- Modify: `internal/server/github.go`
- Modify: `internal/server/github_test.go`

**Interfaces:**
- Consumes: existing `Parse([]string) (Request, error)`, `validateVariantProbe([]string) error`, and additive `RepositorySelector` wire contract.
- Produces: `RepositorySelector.SshUser string` at field 5; `parseAuthority(string) (user string, host string, err error)`; every new client Git open frame includes the parsed user.

- [ ] **Step 1: Add failing selector, parser, probe, relay, and installed-Git tests**

Add `string ssh_user = 5;` to the source `.proto` only after the tests describe the generated accessor. Update expected selectors in every existing accepted parser/installed-Git case to include `SshUser: "git"`, then add cases equivalent to:

```go
{
    name: "Gitea user and names",
    args: []string{"-p", "2222", "forge_user$@Gitea.Example", "git-upload-pack 'Team_Name/repo.with_dot.git'"},
    operation: UploadPack,
    selector: &repowolfv1.RepositorySelector{
        Host: "gitea.example", SshUser: "forge_user$", SshPort: 2222,
        Owner: "Team_Name", Name: "repo.with_dot",
    },
}
```

Keep all current rejection cases and add invalid users (`Git`, `9git`, `git.name`, `git$$`, empty, extra `@`), dot owner/name components, 101-byte components, nested paths, and injection text. Accept probe forms such as `-G forge_user$@gitea.example`, but keep probes syntax-only with no command. In `TestRunUsesSharedTLSAndBearerTransport` and `TestRelayPreservesUploadAndReceiveBytesInBoundedFrames`, require:

```go
repository := open.GetOpen().GetRepository()
if repository.GetSshUser() != "git" {
    return status.Error(codes.InvalidArgument, "missing SSH user")
}
```

Add installed-Git `scp` and `ssh://` upload cases for `forge_user@gitea.example` and a Gitea slug containing `_`/`.`. Add fuzz seeds for those valid cases and the new invalid user/dot-component cases without deleting current seeds. In `internal/server/github_test.go`, clone a valid GitHub request, set `request.Context.Repository.SshUser = "git"`, and require the request to fail as invalid argument so this Git-only field cannot affect API selection.

- [ ] **Step 2: Run the client tests and confirm the compatibility assertions fail**

Run:

```bash
go test ./internal/client/gitssh -run 'TestParse|TestRunHandlesOnlyExactGitSSHVariantProbe|TestInstalledGitGeneratedSSHArgvCompatibility|TestRelayPreservesUploadAndReceiveBytesInBoundedFrames|TestRunUsesSharedTLSAndBearerTransport' -count=1
```

Expected: FAIL because `RepositorySelector` lacks `SshUser`, `parseAuthority` only accepts `git@`, and the current slug validator rejects Gitea-valid owner underscores/dots.

- [ ] **Step 3: Generate the additive protocol field and implement the bounded parser**

Change the source protocol to:

```proto
message RepositorySelector {
  string host = 1;
  string owner = 2;
  string name = 3;
  uint32 ssh_port = 4;
  string ssh_user = 5;
}
```

Run `scripts/generate.sh`. In `parse.go`, return user and host separately, populate `SshUser`, and implement the configured-user grammar without regexp ambiguity:

```go
func parseAuthority(authority string) (string, string, error) {
    if strings.Count(authority, "@") != 1 {
        return "", "", fmt.Errorf("invalid SSH authority")
    }
    user, host, _ := strings.Cut(authority, "@")
    if !validSSHUser(user) {
        return "", "", fmt.Errorf("unsupported SSH user")
    }
    if !validHost(host) {
        return "", "", fmt.Errorf("invalid SSH host")
    }
    return user, strings.ToLower(host), nil
}
```

`validSSHUser` must require the first ASCII character to be lowercase letter or `_`, permit only lowercase letters, digits, `_`, and `-` thereafter, and permit one literal trailing `$`. Replace `validSlug` with a union validator: use the unchanged GitHub owner/name rules when they match; otherwise require both components to pass the 1..100-byte Gitea component grammar and reject exact `.`/`..`. Do not lowercase owner/name. Update `validateVariantProbe` for the new return arity. In `internal/server/github.go`, extend the existing Git-only-field check from `repository.SshPort != 0` to `repository.SshPort != 0 || repository.SshUser != ""`.

- [ ] **Step 4: Verify focused client behavior, fuzz smoke, and protocol freshness**

Run:

```bash
gofmt -w internal/client/gitssh/parse.go internal/client/gitssh/probe.go internal/client/gitssh/parse_test.go internal/client/gitssh/git_compat_test.go internal/client/gitssh/fuzz_test.go internal/client/gitssh/relay_test.go internal/server/github.go internal/server/github_test.go
go test ./internal/client/gitssh ./internal/server -count=1
go test ./internal/client/gitssh -run '^$' -fuzz FuzzParseSSHArgs -fuzztime=10s
go tool buf lint
scripts/check-generated.sh
git diff --check
```

Expected: PASS; existing GitHub cases remain accepted/rejected as before, new requests carry the SSH user, and generated files are fresh.

- [ ] **Step 5: Commit the client protocol slice**

```bash
git add proto/repowolf/v1/common.proto gen/repowolf/v1/common.pb.go internal/client/gitssh/parse.go internal/client/gitssh/probe.go internal/client/gitssh/parse_test.go internal/client/gitssh/git_compat_test.go internal/client/gitssh/fuzz_test.go internal/client/gitssh/relay_test.go internal/server/github.go internal/server/github_test.go
git commit -m "feat(git): carry configured SSH users"
```

---

### Task 2: Resolve Git authority across trusted provider kinds

**Files:**
- Modify: `internal/policy/authorize.go`
- Modify: `internal/policy/authorize_test.go`

**Interfaces:**
- Consumes: immutable `Snapshot`, configured `Provider.Kind/GitHost/SSHUser/SSHPort`, configured repository casing, principal grants, and capabilities.
- Produces:

```go
type GitSelector struct {
    SSHUser string
    Host    string
    SSHPort uint16
    Owner   string
    Name    string
}

func (snapshot *Snapshot) ResolveGit(principal string, selector GitSelector, capability config.Capability) (ResolvedRepository, error)
```

- Preserves: generic `Selector` and `Resolve` semantics for GitHub API paths.

- [ ] **Step 1: Write failing provider-aware policy tests**

Extend the fixture with a Gitea provider configured as `GitHost: "Gitea.Example"`, `SSHUser: "forge_user"`, `SSHPort: 2222` and repository `Team_Name/Repo.One`. Grant `git:read` to the test principal. Add table-driven tests that require:

```go
resolved, err := snapshot.ResolveGit("infra-agent", GitSelector{
    SSHUser: "forge_user", Host: "gitea.example", SSHPort: 2222,
    Owner: "team_name", Name: "repo.one",
}, config.GitRead)
// resolved is the configured Gitea record with Team_Name/Repo.One casing.
```

Cover exact user, case-insensitive host, zero/exact port, GitHub case-sensitive slug, Gitea ASCII case-insensitive slug, missing capability, unknown/ungranted repository, and ambiguity across two granted repositories with indistinguishable Git coordinates. Add old-client cases: empty user resolves only a GitHub provider configured as `git`; it denies Gitea and GitHub providers configured with another user. Keep existing `Resolve` tests unchanged and add an explicit assertion that its API matching remains case-sensitive.

- [ ] **Step 2: Run policy tests and verify the new contract fails**

Run:

```bash
go test ./internal/policy -run 'TestResolveGit|TestResolveExactGrantedRepository|TestResolveMultiRepositoryPrincipal' -count=1
```

Expected: FAIL because `GitSelector` and `ResolveGit` do not exist.

- [ ] **Step 3: Implement a narrow provider-aware resolver**

Keep `Resolve` and `matches` for generic API selectors. Add `ResolveGit` using the same grant-first, exactly-one-match, then capability-check algorithm. Match with logic equivalent to:

```go
func matchesGit(selector GitSelector, repository config.Repository, provider config.Provider) bool {
    userMatches := selector.SSHUser == provider.SSHUser
    if selector.SSHUser == "" {
        userMatches = provider.Kind == config.ProviderGitHub && provider.SSHUser == "git"
    }
    slugMatches := selector.Owner == repository.Owner && selector.Name == repository.Name
    if provider.Kind == config.ProviderGitea {
        slugMatches = strings.EqualFold(selector.Owner, repository.Owner) &&
            strings.EqualFold(selector.Name, repository.Name)
    }
    return userMatches && strings.EqualFold(selector.Host, provider.GitHost) &&
        (selector.SSHPort == 0 || selector.SSHPort == provider.SSHPort) && slugMatches
}
```

All inputs are validated ASCII by client/config boundaries, so `strings.EqualFold` has the required ASCII result here. If desired for defense in depth, implement an unexported ASCII fold helper instead. Return `ErrDenied` for no match, second match, or missing capability, and return a copied `ResolvedRepository` exactly as `Resolve` does.

- [ ] **Step 4: Verify policy behavior and race safety**

Run:

```bash
gofmt -w internal/policy/authorize.go internal/policy/authorize_test.go
go test ./internal/policy -count=1
go test -race ./internal/policy -count=1
git diff --check
```

Expected: PASS, including unchanged generic resolution tests and every uniform-denial case.

- [ ] **Step 5: Commit the policy slice**

```bash
git add internal/policy/authorize.go internal/policy/authorize_test.go
git commit -m "feat(policy): resolve provider-aware Git authority"
```

---

### Task 3: Permit Gitea upload-pack and deny Gitea receive-pack before process start

**Files:**
- Modify: `internal/gitservice/upload.go`
- Modify: `internal/gitservice/receive.go`
- Modify: `internal/gitservice/upload_test.go`
- Modify: `internal/gitservice/receive_test.go`

**Interfaces:**
- Consumes: `policy.ResolveGit`, `RepositorySelector.SshUser`, trusted `ResolvedRepository`, existing runner/audit/stream lifecycle.
- Produces:

```go
func (service *Service) command(
    ctx context.Context,
    open *repowolfv1.GitOpen,
    capability config.Capability,
    remoteService string,
    allowedKinds ...config.ProviderKind,
) (runner.Command, policy.ResolvedRepository, error)
```

- Upload call: `command(ctx, open, config.GitRead, "git-upload-pack", config.ProviderGitHub, config.ProviderGitea)`.
- Receive calls: `command(ctx, open, capability, "git-receive-pack", config.ProviderGitHub)`.

- [ ] **Step 1: Write failing Gitea service tests**

Create service fixtures for GitHub and Gitea. The Gitea record should configure `Gitea.Example`, user `forge_user`, port `2222`, and canonical `Team_Name/Repo.One`. Test a lowercase untrusted selector and require exact trusted argv:

```go
want := []string{
    "-T", "-p", "2222", "--", "forge_user@Gitea.Example",
    "git-upload-pack 'Team_Name/Repo.One.git'",
}
```

Require `git:read`, provider `gitea` in accepted/completed audits, copied token-free environment, and no use of client casing/authority in argv. Table-test missing user/host/owner/name, wrong user/host/port, unknown, unauthorized, missing capability, and ambiguity; all policy failures must return permission-denied terminals and leave a counting `ProcessRunner` at zero starts. Add a receive-pack test whose Gitea selector and input are valid but whose runner remains at zero starts and whose input reader remains unread.

Also add validation cases proving trusted Gitea `.`/`..` and invalid grammar fail invalid-request, and retain current trusted GitHub owner/name behavior.

- [ ] **Step 2: Run focused Git service tests and confirm they fail**

Run:

```bash
go test ./internal/gitservice -run 'TestUpload.*Gitea|TestUploadCommand|TestReceivePack.*Gitea' -count=1
```

Expected: FAIL because command resolution forces `ProviderGitHub` and does not validate or match `ssh_user`.

- [ ] **Step 3: Implement operation-specific provider gates and trusted slug validation**

Require nonempty `Host`, `Owner`, and `Name`. New clients always provide nonempty `SshUser`; allow an empty value to proceed to `ResolveGit` only for the specified legacy GitHub-`git` compatibility behavior. Continue rejecting ports above 65535. Resolve with:

```go
repository, err := service.options.Policy.ResolveGit(principal, policy.GitSelector{
    SSHUser: selector.SshUser,
    Host: selector.Host,
    SSHPort: uint16(selector.SshPort),
    Owner: selector.Owner,
    Name: selector.Name,
}, capability)
```

After resolution, reject a provider kind not in `allowedKinds`. Add an unexported `validTrustedRepository(kind, owner, name string) bool`: GitHub uses the existing owner/name regex behavior; Gitea requires each component to match `[A-Za-z0-9][A-Za-z0-9._-]{0,99}` and rejects exact `.`/`..`. Keep the remote-service allowlist and generate `runner.Command` only from `repository.Provider` and `repository.Repository`.

Pass both kinds only from upload-pack. Pass only `ProviderGitHub` from both receive-pack capability checks, so Gitea is denied before advertisement parsing, process start, stdin read, or provider bytes.

- [ ] **Step 4: Run Git service regression, lifecycle, and race tests**

Run:

```bash
gofmt -w internal/gitservice/upload.go internal/gitservice/receive.go internal/gitservice/upload_test.go internal/gitservice/receive_test.go
go test ./internal/gitservice -count=1
go test -race ./internal/gitservice -count=1
git diff --check
```

Expected: PASS; existing limits, timeout, cancellation, process-reaping, terminal-delivery, audit-failure, and GitHub receive-pack suites remain green.

- [ ] **Step 5: Commit the shared-service slice**

```bash
git add internal/gitservice/upload.go internal/gitservice/receive.go internal/gitservice/upload_test.go internal/gitservice/receive_test.go
git commit -m "feat(git): route Gitea upload-pack"
```

---

### Task 4: Exercise the mixed-provider path with real Git and fake SSH

**Files:**
- Modify: `integration/testdata/policy.yaml`
- Modify: `integration/testdata/fake-ssh.sh`
- Modify: `integration/git_test.go`
- Modify: `integration/leak_test.go`

**Interfaces:**
- Consumes: real `git`, built `repowolf-git-ssh`, TLS test broker, mixed provider policy, fake SSH recorder, and token-free environment.
- Produces: a reusable mixed fixture with GitHub `git@github.com:22/alpha/repo` and Gitea `forge_user@gitea.example.invalid:2222/Team_Name/Repo.One` backed by separate bare repositories.

- [ ] **Step 1: Add the failing mixed-provider real-Git test and fixture contract**

Grant the integration principal `git:read` (not `git:write`) on a renamed `gitea-read` repository with canonical `Team_Name/Repo.One`; retain the existing GitHub grants. Extend `gitFixture` with a second bare remote and separate Gitea upload/receive capture paths. Extend fake SSH environment inputs with `FAKE_SSH_GITHUB_REPOSITORY` and `FAKE_SSH_GITEA_REPOSITORY`, and route only these trusted commands:

```text
git-upload-pack 'alpha/repo.git'
git-receive-pack 'alpha/repo.git'
git-upload-pack 'Team_Name/Repo.One.git'
```

Do not add a Gitea receive-pack branch.

Add `TestRealGitGiteaCloneFetchAndFailClosedDenials` that:

1. clones `ssh://forge_user@gitea.example.invalid:2222/team_name/repo.one.git`;
2. creates and pushes a new commit into the Gitea bare fixture from operator-side seed code;
3. runs `git fetch origin` and proves the new object/ref exists in the checkout;
4. attempts an ungranted Gitea slug and proves fake-SSH argv count does not change;
5. attempts `git push` and proves argv count/input do not change;
6. invokes the built shim directly with malformed/injected argv and proves audit and fake-SSH logs do not change;
7. checks accepted/completed upload-pack audit events identify provider `gitea` and repository `gitea-read`.

Use an invocation-block counter (`BEGIN` lines) rather than only file existence so no-process assertions remain meaningful after successful clone/fetch.

- [ ] **Step 2: Run the integration test and verify the unimplemented path fails**

Run:

```bash
go test ./integration -run 'TestRealGitGiteaCloneFetchAndFailClosedDenials' -count=1 -v
```

Expected: FAIL before the fixture/service changes because Gitea is ungranted and the fake SSH fixture cannot route its trusted upload-pack command.

- [ ] **Step 3: Implement the mixed fixture, denial assertions, and leak matrix**

Seed separate GitHub and Gitea bare repositories. Ensure `gitFixture.git` supplies only RepoWolf client controls and never the provider credential. Preserve existing GitHub fake commands exactly.

Update `TestMarkersRemainInTheirIntendedChannels` to perform the Gitea clone/fetch path and include its client stdout/stderr, checkout, audit, SSH argv/environment, and upload input in the channel map. Keep the Gitea provider credential allowed in no channel:

```go
giteaCredential: {},
```

Require `REPOWOLF_TOKEN_GITEA=unset` in every recorded SSH environment block, and keep principal/GitHub/ambient credential assertions. For audit output, reject token markers, pack/content payloads, raw argv, environment, and provider stderr while permitting only operation/provider/repository/ref metadata defined by the existing audit schema.

- [ ] **Step 4: Verify real Git, leak boundaries, and all integration regressions**

Run:

```bash
gofmt -w integration/git_test.go integration/leak_test.go
sh -n integration/testdata/fake-ssh.sh
go test ./integration -run 'TestRealGit|TestMarkersRemainInTheirIntendedChannels|TestGitFixtureIgnoresHostConfigurationInjection' -count=1 -v
go test -race ./integration -count=1
git diff --check
```

Expected: PASS; GitHub clone/push behavior stays intact, Gitea clone/fetch works, Gitea push and malformed/denied requests start no process, and no credential marker escapes.

- [ ] **Step 5: Commit the offline end-to-end slice**

```bash
git add integration/testdata/policy.yaml integration/testdata/fake-ssh.sh integration/git_test.go integration/leak_test.go
git commit -m "test(git): cover Gitea clone and fetch"
```

---

### Task 5: Add the pinned Gitea interoperability gate and run all release checks

**Files:**
- Create: `internal/testutil/gitea.go`
- Create: `integration/gitea_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: Docker, real Git/OpenSSH/ssh-agent, `testutil.BuildBinaries`, `testutil.StartServer`, and image `docker.gitea.com/gitea:1.27.2@sha256:d20286ca2b2e170fdf628e7231b8a31a3220ade39ff462b55041d43d1fc757dd`.
- Produces: opt-in `TestPinnedGiteaCloneFetch` selected by `REPOWOLF_GITEA_INTEGRATION=1`; deterministic container cleanup; CI interoperability gate.
- Produces these focused fixture interfaces:

```go
const PinnedGiteaImage = "docker.gitea.com/gitea:1.27.2@sha256:d20286ca2b2e170fdf628e7231b8a31a3220ade39ff462b55041d43d1fc757dd"

type Gitea struct {
    SSHHost   string
    SSHPort   int
    SSHUser   string
    Owner     string
    Repository string
}

type SSHAccess struct {
    Home       string
    AgentSocket string
}

func StartGitea(t testing.TB) *Gitea
func (fixture *Gitea) Seed(t testing.TB, filename, contents string) string
func (fixture *Gitea) Update(t testing.TB, filename, contents string) string
func (fixture *Gitea) StartSSHAccess(t testing.TB) SSHAccess
```

The fixture retains container IDs, keys, and operator API credentials privately; exported fields contain no secrets.

- [ ] **Step 1: Write the failing opt-in pinned-container test**

Create `integration/gitea_test.go`. Skip unless `REPOWOLF_GITEA_INTEGRATION == "1"`; once enabled, fail rather than skip when Docker, Git, SSH, ssh-agent, ssh-add, ssh-keygen, or ssh-keyscan is unavailable. The test must call `testutil.StartGitea(t)` and then:

```go
seedCommit := gitea.Seed(t, "seed.txt", "seeded through operator setup\n")
clone := fixture.git(t, root, "clone",
    "ssh://"+gitea.SSHUser+"@"+gitea.SSHHost+":"+strconv.Itoa(gitea.SSHPort)+"/team_name/repo.one.git",
    checkout,
)
if clone.err != nil || strings.TrimSpace(fixture.gitOK(t, checkout, "rev-parse", "HEAD").stdout) != seedCommit {
    t.Fatalf("pinned Gitea clone: %v; stdout=%q stderr=%q", clone.err, clone.stdout, clone.stderr)
}
updatedCommit := gitea.Update(t, "fetched.txt", "fetched through operator setup\n")
fixture.gitOK(t, checkout, "fetch", "origin")
if got := strings.TrimSpace(fixture.gitOK(t, checkout, "rev-parse", "origin/main").stdout); got != updatedCommit {
    t.Fatalf("fetched origin/main = %q, want %q", got, updatedCommit)
}
```

Require RepoWolf audit pairs for `git.upload-pack` with provider `gitea` and configured repository ID, and scan client output, broker stderr, audit, checkout-visible files, and the captured broker-side SSH environment for principal/provider token markers. Require the Gitea token and principal token to be absent from the agent command environment as well.

- [ ] **Step 2: Run the test and verify the fixture API is missing**

Run:

```bash
REPOWOLF_GITEA_INTEGRATION=1 go test ./integration -run '^TestPinnedGiteaCloneFetch$' -count=1 -v
```

Expected: FAIL to compile because the focused Gitea test fixture does not exist.

- [ ] **Step 3: Implement deterministic operator-side Gitea and SSH setup**

In `internal/testutil/gitea.go`, keep container lifecycle separate from general server setup. Implement the exact `PinnedGiteaImage`, `Gitea`, `SSHAccess`, `StartGitea`, `Seed`, `Update`, and `StartSSHAccess` interfaces above. The helper exposes only safe host/port/slug values and owns operator credentials and lifecycle privately. It must:

1. start the exact digest-pinned image with a temporary `/data`, SQLite, install lock, HTTP bound to loopback, and container SSH port 22 published to an ephemeral loopback host port;
2. wait with a bounded deadline for HTTP readiness and always register `docker rm -f` cleanup;
3. create account `Team_Name` with `gitea admin user create` inside the container and set `Gitea.SSHHost = "localhost"`, `Gitea.SSHUser = "git"`, `Gitea.Owner = "Team_Name"`, and `Gitea.Repository = "Repo.One"`;
4. generate the Ed25519 keypair and an operator-only API token with `gitea admin user generate-access-token`, then use that token only inside test fixture code to add the public key and create `Team_Name/Repo.One`;
5. have `Seed`/`Update` use operator Git over the private SSH access to create commits on `main` and return their object IDs, never adding RepoWolf production API code;
6. generate a temporary Ed25519 key, start a private `ssh-agent`, add the key, scan `[localhost]:<port>` into a mode-0600 temporary `known_hosts`, and start the RepoWolf broker with that `SSH_AUTH_SOCK` and `HOME`;
7. pass a distinct Gitea provider token marker only to the broker startup environment, where credential loading requires it but Git SSH never uses it;
8. capture the broker-side SSH process environment through the agent/socket boundary or a test-only pinned executable wrapper that records environment and then `exec`s the real absolute SSH path without changing the service-generated argv.

Do not log the API token, provider token, private key, SSH agent environment, generated broker config, or raw SSH stderr. Use `t.Cleanup` for agent and container shutdown and bounded polling rather than sleeps. If a wrapper records the environment, pin its absolute path in `tools.ssh`, use fixed wrapper behavior with no request-derived arguments of its own, and assert it forwards the six trusted service arguments unchanged.

- [ ] **Step 4: Add the CI behavioral gate without static workflow tests**

After the normal race test step in `.github/workflows/ci.yml`, add:

```yaml
      - name: Test pinned Gitea Git interoperability
        env:
          REPOWOLF_GITEA_INTEGRATION: "1"
        run: go test ./integration -run '^TestPinnedGiteaCloneFetch$' -count=1 -v
```

Do not add a test that greps YAML or merely checks the image string. Validate through YAML parsing already performed by GitHub, direct review, the pinned container startup, and clone/fetch behavior. Keep the broad two-version certification matrix deferred.

- [ ] **Step 5: Verify the pinned demonstration and all repository gates**

Run in this exact order:

```bash
gofmt -w internal/testutil/gitea.go integration/gitea_test.go
go test ./internal/client/gitssh ./internal/policy ./internal/gitservice -count=1
go test ./integration -run 'TestRealGitGiteaCloneFetchAndFailClosedDenials|TestMarkersRemainInTheirIntendedChannels' -count=1 -v
REPOWOLF_GITEA_INTEGRATION=1 go test ./integration -run '^TestPinnedGiteaCloneFetch$' -count=1 -v
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
git diff --check
```

Expected: every command exits 0. The pinned test proves clone and fetch through real Gitea; the standard race suite keeps the opt-in container test skipped while all non-container tests run; Nix/OCI checks prove unchanged packaging and integration wiring.

- [ ] **Step 6: Review scope and commit the interoperability gate**

Before committing, run:

```bash
git diff --stat
git diff --check
git status --short
```

Confirm there is no Gitea receive-pack enablement, Gitea SDK/production API dependency, `tea` binary, HTTPS Git path, provider registry, credential in config/log/output, or broad compatibility claim. Then commit:

```bash
git add internal/testutil/gitea.go integration/gitea_test.go .github/workflows/ci.yml
git commit -m "test(gitea): add pinned clone and fetch gate"
```
