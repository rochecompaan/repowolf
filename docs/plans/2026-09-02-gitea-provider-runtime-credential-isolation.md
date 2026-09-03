# Gitea Provider Runtime and Credential Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build one isolated runtime instance for each configured provider while keeping every credential bound to its trusted provider ID.

**Architecture:** Configuration records credential names but not values. A new credential snapshot performs all named token lookups once. `internal/app` then builds a private provider-instance map and a GitHub dispatcher keyed by trusted provider ID. The runner creates one token-free SSH environment and one exact environment for each GitHub adapter.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, gRPC, the existing GitHub CLI adapter, OpenSSH, and the existing integration harness.

## Global Constraints

- Keep `apiVersion` at `repowolf.dev/v1alpha1`.
- Accept only `github` and `gitea` provider kinds.
- Accept provider token names only when they match `REPOWOLF_TOKEN_[A-Z0-9_]+`.
- Treat only syntactic GitHub `tokenEnv` omission as legacy migration.
- Reject explicit empty and YAML null `tokenEnv` values.
- Permit legacy migration for at most one GitHub provider.
- Load each named credential source once during startup.
- Reject duplicate credential names before environment lookup.
- Reject duplicate raw credential values across principals and providers.
- Do not place secret values or digests in errors, logs, audit events, arguments, or responses.
- Pass no principal or provider credential to SSH.
- Pass only the matching provider credential to each GitHub adapter.
- Resolve `ssh` for every valid runtime.
- Resolve `gh` only when the configuration contains a GitHub provider.
- Keep provider composition private to `internal/app`; do not add a registry or plugin framework.
- Do not add a Gitea SDK client, service, operation, transport, or `caFile` support.
- Keep all existing GitHub request, response, status, normalization, and audit behavior unchanged.

## File Structure

### New files

- `internal/config/provider_instance_test.go`: focused provider schema, migration, and Gitea collision tests.
- `internal/credentials/snapshot.go`: immutable credential snapshot and narrow accessors.
- `internal/credentials/load.go`: deterministic environment loading and duplicate-value rejection.
- `internal/credentials/load_test.go`: lookup-count, migration, collision, and leak tests.
- `internal/app/github_executor.go`: trusted provider-ID dispatch for GitHub requests.
- `internal/app/github_executor_test.go`: two-adapter routing and missing-adapter tests.
- `internal/app/providers.go`: closed provider-kind switch and private instance construction.
- `internal/app/providers_test.go`: provider-instance construction and isolation tests.
- `internal/app/runtime_provider_test.go`: runtime composition tests for GitHub-only, Gitea-only, mixed, and failure cases.
- `integration/runtime_isolation_test.go`: real-process duplicate-credential rejection before readiness.

### Modified files

- `internal/config/types.go`: add `ProviderGitea` and `Provider.TokenEnv`.
- `internal/config/decode.go`: preserve omitted versus explicit provider token fields.
- `internal/config/validate.go`: validate provider kinds, credential names, Gitea names, and folded slug collisions.
- `internal/config/config_test.go`: keep shared valid fixtures explicit.
- `internal/config/testdata/valid.yaml`: use an explicit provider token name.
- `internal/auth/index.go`: construct authentication indexes from already loaded token values.
- `internal/auth/index_test.go`: keep authentication behavior tests after lookup ownership moves.
- `internal/auth/interceptor_test.go`: use the new authentication constructor.
- `internal/auth/token.go`: expose a boolean principal-token validator to the credential loader.
- `internal/server/server_test.go`: use the new authentication constructor.
- `internal/runner/environment.go`: render token-free and GitHub-specific child environments.
- `internal/runner/environment_test.go`: prove exact removal, suffix, copying, and cross-provider isolation.
- `internal/runner/tools.go`: make `gh` resolution conditional.
- `internal/runner/tools_test.go`: prove Gitea-only startup ignores `gh`.
- `internal/app/runtime.go`: assemble credentials, tools, provider instances, services, and readiness in fail-closed order.
- `internal/app/runtime_test.go`: keep the existing startup and listener-order checks.
- `internal/server/github_test.go`: prove absent GitHub dependencies register no GitHub service.
- `integration/testdata/policy.yaml`: run the integration broker with explicit GitHub and Gitea provider records.
- `integration/testdata/fake-provider.sh`: record the complete sensitive environment contract.
- `integration/testdata/fake-ssh.sh`: record token absence and safe inherited variables.
- `integration/forge_test.go`: supply isolated provider tokens and assert the GitHub environment.
- `integration/git_test.go`: supply isolated provider tokens and assert the SSH environment.
- `integration/bubblewrap_test.go`: preserve the sandbox boundary under the new environment contract.
- `integration/leak_test.go`: keep each secret marker in its intended channel.
- `integration/audit_assertions_test.go`: reject every new provider and ambient marker from audits.
- `internal/testutil/server.go`: expose a bounded real-process startup-failure helper for integration tests.
- `docs/configuration.md`: document provider token names and legacy migration.
- `README.md`: update the runnable Docker credential name.
- `scripts/repowolf-dogfood.sh`: render and load an explicit GitHub provider token name.
- `examples/docker/.env.example`: name the explicit GitHub provider source.
- `examples/docker/compose.yaml`: pass the explicit source only to the broker.
- `examples/docker/config/repowolf.yaml`: add `tokenEnv`.
- `examples/docker/config/repowolf-host.yaml`: add `tokenEnv`.
- `examples/docker/README.md`: document Compose and native-host credential setup.
- `examples/docker/bootstrap.sh`: print the new credential instruction.
- `examples/docker/test-install-host-principal.sh`: preserve the renamed service environment fixture.
- `scripts/ci/docker-example/bootstrap-disposable-state.sh`: create the renamed disposable credential.
- `scripts/ci/docker-example/assert-sandbox-boundary.sh`: prove the provider source is absent from the sandbox.
- `scripts/ci/docker-example/assert-github-policy.sh`: use the new source name in diagnostics.

---

### Task 1: Extend and validate the provider configuration model

**Files:**
- Modify: `internal/config/types.go:41-51`
- Modify: `internal/config/decode.go:13-30,159-176`
- Modify: `internal/config/validate.go:14-147`
- Modify: `internal/config/config_test.go:12-44,313-361`
- Modify: `internal/config/testdata/valid.yaml:9-15`
- Create: `internal/config/provider_instance_test.go`

**Interfaces:**
- Consumes: strict YAML decoding through `config.Decode(io.Reader)`.
- Produces: `config.ProviderGitea` and `config.Provider.TokenEnv string`.
- Produces: validated provider records for `credentials.Load` in Task 2.
- Produces: ASCII-only Gitea owner and repository names for policy snapshots.

- [ ] **Step 1: Write failing decode and migration tests**

Create `internal/config/provider_instance_test.go`. Use table tests with these exact YAML states:

```go
func TestDecodeDistinguishesProviderTokenEnvironmentStates(t *testing.T) {
	for _, test := range []struct {
		name      string
		tokenLine string
		wantError string
	}{
		{name: "explicit", tokenLine: "    tokenEnv: REPOWOLF_TOKEN_GITHUB\n"},
		{name: "omitted legacy", tokenLine: ""},
		{name: "empty", tokenLine: "    tokenEnv: \"\"\n", wantError: "tokenEnv"},
		{name: "null", tokenLine: "    tokenEnv: null\n", wantError: "tokenEnv"},
		{name: "non string", tokenLine: "    tokenEnv: 7\n", wantError: "tokenEnv"},
	} {
		t.Run(test.name, func(t *testing.T) {
			yaml := providerYAML("github", test.tokenLine)
			cfg, err := Decode(strings.NewReader(yaml))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("Decode() error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.name == "explicit" && cfg.Providers["provider"].TokenEnv != "REPOWOLF_TOKEN_GITHUB" {
				t.Fatalf("explicit tokenEnv = %q", cfg.Providers["provider"].TokenEnv)
			}
			if test.tokenLine == "" && cfg.Providers["provider"].TokenEnv != "" {
				t.Fatalf("legacy tokenEnv = %q", cfg.Providers["provider"].TokenEnv)
			}
		})
	}
}
```

Add table rows which reject these validated configurations:

```go
[]struct {
	name      string
	mutate    func(*Config)
	wantError string
}{
	{
		name: "Gitea omits tokenEnv",
		mutate: func(cfg *Config) {
			provider := cfg.Providers["github"]
			provider.Kind = ProviderGitea
			provider.TokenEnv = ""
			cfg.Providers["github"] = provider
		},
		wantError: "requires tokenEnv",
	},
	{
		name: "two GitHub records omit tokenEnv",
		mutate: func(cfg *Config) {
			provider := cfg.Providers["github"]
			provider.TokenEnv = ""
			cfg.Providers["github"] = provider
			cfg.Providers["github-two"] = provider
		},
		wantError: "legacy",
	},
}
```

- [ ] **Step 2: Run the focused tests and record the expected failure**

Run:

```bash
go test ./internal/config -run 'TestDecodeDistinguishesProviderTokenEnvironmentStates|TestValidateProviderTokenEnvironmentRules' -count=1
```

Expected: FAIL because `ProviderGitea`, `Provider.TokenEnv`, and explicit field-state handling do not exist.

- [ ] **Step 3: Add field-presence decoding and provider-kind validation**

Add the public model fields in `internal/config/types.go`:

```go
const (
	ProviderGitHub ProviderKind = "github"
	ProviderGitea  ProviderKind = "gitea"
)

type Provider struct {
	Kind     ProviderKind `yaml:"kind"`
	APIHost  string       `yaml:"apiHost"`
	GitHost  string       `yaml:"gitHost"`
	SSHUser  string       `yaml:"sshUser"`
	SSHPort  uint16       `yaml:"sshPort"`
	TokenEnv string       `yaml:"tokenEnv"`
}
```

Add one private scalar type in `internal/config/decode.go`:

```go
type rawOptionalString struct {
	present bool
	value   string
}

func (value *rawOptionalString) UnmarshalYAML(node *yaml.Node) error {
	value.present = true
	if node.Tag != "!!str" {
		return fmt.Errorf("tokenEnv must be a string")
	}
	value.value = node.Value
	return nil
}
```

Use `TokenEnv rawOptionalString` in `rawProvider`. Reject a present empty value during normalization. Copy the value into `Provider.TokenEnv` only after this check.

Use a closed switch in `validateProvider`:

```go
switch provider.Kind {
case ProviderGitHub, ProviderGitea:
default:
	return fmt.Errorf("provider %q has unsupported kind %q", id, provider.Kind)
}
if provider.TokenEnv == "" {
	if provider.Kind == ProviderGitea {
		return fmt.Errorf("provider %q requires tokenEnv", id)
	}
} else if !tokenEnvName.MatchString(provider.TokenEnv) {
	return fmt.Errorf("provider %q has invalid token environment name", id)
}
```

Count GitHub records with an empty normalized `TokenEnv`. Reject a count greater than one.

- [ ] **Step 4: Write failing duplicate-name and Gitea repository tests**

Add exact cases for these credential-name collisions:

```go
[]struct {
	name   string
	first  string
	second string
}{
	{name: "provider and provider", first: "REPOWOLF_TOKEN_SHARED", second: "REPOWOLF_TOKEN_SHARED"},
	{name: "provider and principal", first: "REPOWOLF_TOKEN_AGENT", second: "REPOWOLF_TOKEN_AGENT"},
}
```

Add a direct `Config.Validate` case with two identical token names in one principal:

```go
principal := cfg.Principals["agent"]
principal.TokenEnvs = []string{"REPOWOLF_TOKEN_AGENT", "REPOWOLF_TOKEN_AGENT"}
cfg.Principals["agent"] = principal
```

Add Gitea repository cases for:

```go
[]struct {
	owner     string
	repository string
	valid     bool
}{
	{owner: "group.with-dot", repository: "repo_name", valid: true},
	{owner: ".", repository: "repo", valid: false},
	{owner: "..", repository: "repo", valid: false},
	{owner: "group", repository: "..", valid: false},
	{owner: "group", repository: "repo.git", valid: false},
}
```

Add three collision cases. Use same-record, cross-provider, and case-only pairs. Require each error to contain both repository IDs.

- [ ] **Step 5: Run the new tests and record the expected failure**

Run:

```bash
go test ./internal/config -run 'TestValidateRejectsDuplicateCredentialNames|TestValidateGiteaRepositoryNames|TestValidateRejectsFoldedGiteaSlugCollisions' -count=1
```

Expected: FAIL because credential-name and folded-slug validation do not exist.

- [ ] **Step 6: Implement cross-record validation**

Keep the current GitHub grammar unchanged. Add a separate Gitea grammar:

```go
var giteaName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func validGiteaName(value string) bool {
	return giteaName.MatchString(value) && value != "." && value != ".."
}
```

Reject an exact `.git` suffix for Gitea repository names. Select the grammar from `providers[repository.Provider].Kind`.

Move principal-only token-name collision checks into one configuration-wide function. Visit sorted principal IDs and sorted provider IDs. Store the first owner label for each name. Return an error before any credential lookup occurs.

Visit sorted repository IDs and build this key for each Gitea record:

```go
key := strings.ToLower(repository.Owner) + "/" + strings.ToLower(repository.Name)
```

Reject a repeated key and name both repository IDs. Do not compare Gitea keys with GitHub repositories.

Update `validConfig()`, `validYAML()`, and `testdata/valid.yaml` to use `REPOWOLF_TOKEN_GITHUB`.

- [ ] **Step 7: Run and format the configuration package**

Run:

```bash
gofmt -w internal/config/types.go internal/config/decode.go internal/config/validate.go internal/config/config_test.go internal/config/provider_instance_test.go
go test ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit the configuration contract**

```bash
git add internal/config
git commit -m "feat(config): validate provider instances"
```

---

### Task 2: Load one immutable credential snapshot

**Files:**
- Modify: `internal/auth/index.go:1-78`
- Modify: `internal/auth/index_test.go:14-96`
- Modify: `internal/auth/interceptor_test.go:108-118`
- Modify: `internal/auth/token.go:27-38`
- Modify: `internal/server/server_test.go:349-360`
- Create: `internal/credentials/snapshot.go`
- Create: `internal/credentials/load.go`
- Create: `internal/credentials/load_test.go`

**Interfaces:**
- Consumes: validated `config.Config` from Task 1.
- Produces: `auth.NewIndex(map[string][]string) (*auth.Index, error)`.
- Produces: `auth.ValidToken(string) bool`.
- Produces: `credentials.Load(config.Config, credentials.LookupEnv) (*credentials.Snapshot, error)`.
- Produces: immutable authentication, provider token, environment-name, and legacy-provenance accessors.

- [ ] **Step 1: Write failing authentication-constructor tests**

Replace environment ownership tests in `internal/auth/index_test.go` with constructor tests:

```go
func TestNewIndexAuthenticatesDistinctTokens(t *testing.T) {
	first := testToken(1)
	second := testToken(2)
	index, err := NewIndex(map[string][]string{
		"agent-a": {first},
		"agent-b": {second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if principal, ok := index.Authenticate(first); !ok || principal != "agent-a" {
		t.Fatalf("Authenticate(first) = %q, %v", principal, ok)
	}
	if principal, ok := index.Authenticate(second); !ok || principal != "agent-b" {
		t.Fatalf("Authenticate(second) = %q, %v", principal, ok)
	}
}
```

Keep cases for malformed principal tokens and duplicate principal token values. Require errors to omit the supplied secret.

- [ ] **Step 2: Run the authentication tests and record the expected failure**

Run:

```bash
go test ./internal/auth -run 'TestNewIndex' -count=1
```

Expected: FAIL because `NewIndex` does not exist.

- [ ] **Step 3: Move named lookup ownership out of `internal/auth`**

Add the constructor below. Keep `auth.LookupEnv` and `auth.Load` as temporary wrappers so intermediate commits compile. Task 6 removes them after runtime migration.

```go
func NewIndex(principals map[string][]string) (*Index, error)
```

Sort principal IDs. Validate and digest every value. Reject duplicate values. Store only digests and principal IDs in `Index`.

Add this narrow validator in `internal/auth/token.go`:

```go
// ValidToken reports whether token has the canonical RepoWolf principal-token form.
func ValidToken(token string) bool {
	_, ok := tokenDigest(token)
	return ok
}
```

Update interceptor and server test helpers to call `auth.NewIndex` with already loaded token values.

- [ ] **Step 4: Write failing credential snapshot tests**

Define a counting lookup in `internal/credentials/load_test.go`:

```go
type countingLookup struct {
	values map[string]string
	calls  map[string]int
	order  []string
}

func (lookup *countingLookup) get(name string) (string, bool) {
	lookup.calls[name]++
	lookup.order = append(lookup.order, name)
	value, ok := lookup.values[name]
	return value, ok
}
```

Add tests which prove:

```go
func TestLoadReadsEachNamedSourceOnce(t *testing.T)
func TestLoadReturnsCopiedSortedEnvironmentNames(t *testing.T)
func TestLoadBindsProviderTokensByProviderID(t *testing.T)
func TestLoadRejectsMissingAndEmptyProviderTokensWithoutDisclosure(t *testing.T)
func TestLoadRejectsDuplicateValuesAcrossEveryCredentialCategory(t *testing.T)
func TestLoadLegacyGitHubTokenMatrix(t *testing.T)
func TestLoadReportsLegacyProvenanceWithoutReturningAnotherToken(t *testing.T)
```

Use all six legacy states from the specification. The both-present case must fail even when one value is empty. Require `lookup.calls[name] == 1` for every inspected name.

Use deliberately unsorted principal and provider maps in `TestLoadReadsEachNamedSourceOnce`. Require this exact lookup order:

```go
[]string{
	"REPOWOLF_TOKEN_AGENT_A_SECOND",
	"REPOWOLF_TOKEN_AGENT_A_FIRST",
	"REPOWOLF_TOKEN_AGENT_B",
	"REPOWOLF_TOKEN_GITEA",
	"REPOWOLF_TOKEN_GITHUB",
	"GH_TOKEN",
	"GITHUB_TOKEN",
}
```

Assign the first two names to principal `agent-a` in the displayed order. Assign the third name to `agent-b`. Use provider IDs `gitea`, `github-explicit`, and `github-legacy`.

Use one valid RepoWolf principal token as the provider value in the cross-category duplicate test. Require the error to contain source names but not the token or its digest.

- [ ] **Step 5: Run the credential tests and record the expected failure**

Run:

```bash
go test ./internal/credentials -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 6: Define the narrow snapshot API**

Create `internal/credentials/snapshot.go` with these signatures:

```go
package credentials

type LookupEnv func(string) (string, bool)

type Snapshot struct {
	auth        *auth.Index
	providers   map[string]providerCredential
	environments []string
}

type providerCredential struct {
	value       string
	environment string
	legacy      bool
}

func (snapshot *Snapshot) AuthIndex() *auth.Index
func (snapshot *Snapshot) ProviderToken(providerID string) (string, bool)
func (snapshot *Snapshot) EnvironmentNames() []string
func (snapshot *Snapshot) UsesLegacyProviderToken(providerID string) bool
```

`EnvironmentNames` returns `append([]string(nil), snapshot.environments...)`. Do not expose the provider map or token enumeration.

- [ ] **Step 7: Implement deterministic loading and duplicate rejection**

Create `internal/credentials/load.go` with this entry point:

```go
func Load(cfg config.Config, lookup LookupEnv) (*Snapshot, error)
```

Load sorted principal IDs first. Keep each principal's configured token order. Validate principal values with `auth.ValidToken` before constructing the authentication index.

Then load sorted provider IDs. For explicit providers, inspect only `Provider.TokenEnv`. For the one legacy GitHub record, inspect `GH_TOKEN` and `GITHUB_TOKEN` once each.

Track raw-value identity with:

```go
digest := sha256.Sum256([]byte(value))
```

Map each digest to a source label. Reject any repeated digest across all categories. Do not format the value or digest into an error.

Sort the loaded environment names before storing them. Include only the selected legacy source in `EnvironmentNames`. Prefix filtering in Task 3 removes every ambient `GH_` and `GITHUB_` variable.

- [ ] **Step 8: Run credential, authentication, and server tests**

Run:

```bash
gofmt -w internal/auth/index.go internal/auth/index_test.go internal/auth/interceptor_test.go internal/auth/token.go internal/credentials/snapshot.go internal/credentials/load.go internal/credentials/load_test.go internal/server/server_test.go
go test ./internal/auth ./internal/credentials ./internal/server -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit the credential snapshot**

```bash
git add internal/auth internal/credentials internal/server/server_test.go
git commit -m "feat(credentials): load isolated credential snapshot"
```

---

### Task 3: Render token-free and GitHub child environments

**Files:**
- Modify: `internal/runner/environment.go:1-25`
- Modify: `internal/runner/environment_test.go:1-45`

**Interfaces:**
- Consumes: `credentials.Snapshot.EnvironmentNames()` from Task 2.
- Produces: `runner.TokenFreeEnvironment([]string, []string) []string`.
- Produces: `runner.GitHubEnvironment([]string, string) []string`.

- [ ] **Step 1: Replace the environment tests with the DR-12 contract**

Add a token-free test with this input:

```go
base := []string{
	"PATH=/bin",
	"SSH_AUTH_SOCK=/run/agent.sock",
	"GIT_PROTOCOL=version=2",
	"REPOWOLF_TOKEN_AGENT=principal-secret",
	"REPOWOLF_TOKEN_GITHUB=provider-secret",
	"REPOWOLF_INTERNAL=control",
	"GH_TOKEN=ambient-gh",
	"GH_HOST=ambient-host",
	"GITHUB_TOKEN=ambient-github",
	"NO_COLOR=0",
	"SAFE=value=with=equals",
	"MALFORMED",
}
```

Require this exact result:

```go
[]string{
	"PATH=/bin",
	"SSH_AUTH_SOCK=/run/agent.sock",
	"GIT_PROTOCOL=version=2",
	"NO_COLOR=0",
	"SAFE=value=with=equals",
	"MALFORMED",
}
```

Add a GitHub test. Start with the token-free result. Require this exact suffix and order:

```go
[]string{
	"GH_TOKEN=provider-secret",
	"GH_PROMPT_DISABLED=1",
	"GH_NO_UPDATE_NOTIFIER=1",
	"NO_COLOR=1",
}
```

Add copy tests for inputs and outputs. Build two GitHub environments and prove neither contains the other token.

- [ ] **Step 2: Run the environment tests and record the expected failure**

Run:

```bash
go test ./internal/runner -run 'TestTokenFreeEnvironment|TestGitHubEnvironment' -count=1
```

Expected: FAIL because the new renderers do not exist and the current renderer retains `GH_` values.

- [ ] **Step 3: Implement the two renderers**

Use these public signatures:

```go
func TokenFreeEnvironment(base []string, excluded []string) []string
func GitHubEnvironment(tokenFree []string, token string) []string
```

`TokenFreeEnvironment` parses each name with `strings.Cut(entry, "=")`. Remove exact excluded names and every name with these prefixes:

```go
[]string{"REPOWOLF_", "GH_", "GITHUB_"}
```

Clone each retained string. Preserve retained order and bytes.

`GitHubEnvironment` removes inherited `NO_COLOR`, clones all other entries, and appends the four required values. It must not mutate or alias the token-free input.

Keep this temporary wrapper so intermediate commits compile:

```go
func ProviderEnvironment(base []string, excluded []string) []string {
	return TokenFreeEnvironment(base, excluded)
}
```

Task 6 removes the wrapper after runtime migration.

- [ ] **Step 4: Run and format the runner environment tests**

Run:

```bash
gofmt -w internal/runner/environment.go internal/runner/environment_test.go
go test ./internal/runner -run 'Environment' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the child environment contract**

```bash
git add internal/runner/environment.go internal/runner/environment_test.go
git commit -m "feat(runner): isolate child environments"
```

---

### Task 4: Resolve only tools required by configured providers

**Files:**
- Modify: `internal/runner/tools.go:15-35`
- Modify: `internal/runner/tools_test.go:13-81`
- Modify: `internal/app/runtime.go:52-55`

**Interfaces:**
- Consumes: a boolean which states whether at least one GitHub provider exists.
- Produces: `runner.ResolveTools(config.Tools, bool, func(string) (string, error)) (runner.Toolset, error)`.
- Produces: a canonical SSH path for every runtime and an optional canonical `gh` path.

- [ ] **Step 1: Write failing conditional-resolution tests**

Add these focused tests:

```go
func TestResolveToolsAlwaysResolvesSSH(t *testing.T)
func TestResolveToolsSkipsGHForGiteaOnlyRuntime(t *testing.T)
func TestResolveToolsRequiresGHForGitHubRuntime(t *testing.T)
```

For the Gitea-only case, provide an invalid absolute `tools.GH` override. Record lookup names. Require an empty `Toolset.GH`, a canonical `Toolset.SSH`, and no `gh` lookup.

For the GitHub case, require one `gh` resolution and one `ssh` resolution. Keep existing unsafe executable and canonicalization cases.

- [ ] **Step 2: Run the tool tests and record the expected failure**

Run:

```bash
go test ./internal/runner -run 'TestResolveTools' -count=1
```

Expected: FAIL because `ResolveTools` has no provider requirement parameter and always resolves `gh`.

- [ ] **Step 3: Implement conditional `gh` resolution**

Change the signature to:

```go
func ResolveTools(tools config.Tools, githubRequired bool, lookPath func(string) (string, error)) (Toolset, error)
```

Resolve SSH for every call. Resolve `gh` only when `githubRequired` is true. Leave `Toolset.GH` empty otherwise. Keep absolute-path, canonical-file, and executable-bit checks unchanged.

Update the current runtime call with `githubRequired` set to `true`:

```go
tools, err := runner.ResolveTools(cfg.Tools, true, runner.LookPath)
```

Task 6 replaces this compatibility value with the configured provider-kind check.

- [ ] **Step 4: Run and format all runner tests**

Run:

```bash
gofmt -w internal/runner/tools.go internal/runner/tools_test.go internal/app/runtime.go
go test ./internal/runner ./internal/app -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit conditional tool resolution**

```bash
git add internal/runner/tools.go internal/runner/tools_test.go internal/app/runtime.go
git commit -m "feat(runner): resolve provider tools conditionally"
```

---

### Task 5: Build private provider instances and trusted GitHub dispatch

**Files:**
- Create: `internal/app/github_executor.go`
- Create: `internal/app/github_executor_test.go`
- Create: `internal/app/providers.go`
- Create: `internal/app/providers_test.go`

**Interfaces:**
- Consumes: `credentials.Snapshot`, `runner.Toolset`, and the token-free environment.
- Consumes: `policy.ResolvedRepository.Repository.Provider` as the trusted provider ID.
- Produces: one private `providerInstance` for each configured provider ID.
- Produces: one `githubExecutor` which satisfies `server.GitHubExecutor`.

- [ ] **Step 1: Write failing two-adapter dispatch tests**

Create `internal/app/github_executor_test.go` in package `app`. Define a fake which records calls and returns a fixed response.

Use this dispatch setup:

```go
first := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
second := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
executor := &githubExecutor{adapters: map[string]server.GitHubExecutor{
	"github-a": first,
	"github-b": second,
}}
repository := policy.ResolvedRepository{
	Repository: config.Repository{Provider: "github-b"},
}
```

Call `Execute`. Require `first.calls == 0`, `second.calls == 1`, and the second fake to receive the same resolved repository.

Add a missing-adapter test with `Provider: "missing"`. Require `errors.Is(err, rpcstatus.ErrServiceUnavailable)`. Require zero calls to every configured fake.

- [ ] **Step 2: Run the dispatch tests and record the expected failure**

Run:

```bash
go test ./internal/app -run 'TestGitHubExecutor' -count=1
```

Expected: FAIL because `githubExecutor` does not exist.

- [ ] **Step 3: Implement trusted provider-ID dispatch**

Create `internal/app/github_executor.go`:

```go
type githubExecutor struct {
	adapters map[string]server.GitHubExecutor
}

func (executor *githubExecutor) Execute(
	ctx context.Context,
	repository policy.ResolvedRepository,
	request *repowolfv1.GitHubRequest,
) (*repowolfv1.GitHubResponse, error) {
	if executor == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	adapter := executor.adapters[repository.Repository.Provider]
	if adapter == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	return adapter.Execute(ctx, repository, request)
}
```

Do not inspect provider IDs from request fields. Do not select a fallback adapter.

- [ ] **Step 4: Write failing provider-instance construction tests**

Create `internal/app/providers_test.go`. Load a credential snapshot with two GitHub providers and one Gitea provider. Use distinct provider tokens.

Require:

```go
len(instances) == 3
instances["github-a"].github != nil
instances["github-b"].github != nil
instances["gitea-lab"].github == nil
instances["gitea-lab"].token == "gitea-secret"
len(executor.adapters) == 2
```

Use one recording `providergithub.Caller` with this result:

```go
runner.Result{Stdout: []byte(`{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","private":false,"html_url":"https://safe.invalid/repo","default_branch":"main"}`)}
```

Execute a `GitHubRequest_RepositoryView` through each concrete adapter. Inspect the two recorded commands. Require each `runner.Command.Env` to contain only its matching `GH_TOKEN`.

Add an unsupported-kind case which calls the private constructor directly. Require an error and no partial map.

- [ ] **Step 5: Run the provider construction tests and record the expected failure**

Run:

```bash
go test ./internal/app -run 'TestBuildProviderInstances' -count=1
```

Expected: FAIL because private provider-instance construction does not exist.

- [ ] **Step 6: Implement the closed provider-instance builder**

Create `internal/app/providers.go` with these private types and functions:

```go
type providerInstance struct {
	provider config.Provider
	token    string
	legacy   bool
	github   server.GitHubExecutor
}

func buildProviderInstances(
	cfg config.Config,
	snapshot *credentials.Snapshot,
	tools runner.Toolset,
	tokenFreeEnvironment []string,
	caller providergithub.Caller,
) (map[string]providerInstance, error)

func buildGitHubExecutor(instances map[string]providerInstance) *githubExecutor
```

Visit sorted provider IDs. Fetch each token by ID. Return a sanitized construction error if the binding is absent. Store `snapshot.UsesLegacyProviderToken(id)` in each instance.

Use this closed switch:

```go
switch provider.Kind {
case config.ProviderGitHub:
	environment := runner.GitHubEnvironment(tokenFreeEnvironment, token)
	adapter, err := providergithub.New(providergithub.AdapterOptions{
		Path:        tools.GH,
		Environment: environment,
		Timeout:     cfg.Limits.OperationTimeout,
		Caller:      caller,
	})
	if err != nil {
		return nil, fmt.Errorf("create GitHub provider %q: %w", id, err)
	}
	instance.github = adapter
case config.ProviderGitea:
	// Issue 4 constructs the Gitea SDK client.
default:
	return nil, fmt.Errorf("create provider %q: unsupported kind", id)
}
```

Store Gitea configuration and its token in the private instance. Do not create a client, service, or operation.

Return `nil` from `buildGitHubExecutor` when no instance contains a GitHub adapter. Copy adapter references into a new private map.

Add one invalid-tool test to `providers_test.go`:

```go
_, err := buildProviderInstances(cfg, snapshot, runner.Toolset{GH: "relative-gh"}, tokenFree, caller)
if err == nil || !strings.Contains(err.Error(), "GitHub provider") {
	t.Fatalf("buildProviderInstances() error = %v", err)
}
```

Require an explicit GitHub instance to have `legacy == false`. Require a legacy GitHub instance to have `legacy == true`.

- [ ] **Step 7: Run and format the provider-instance tests**

Run:

```bash
gofmt -w internal/app/github_executor.go internal/app/github_executor_test.go internal/app/providers.go internal/app/providers_test.go
go test ./internal/app -run 'TestGitHubExecutor|TestBuildProviderInstances' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit provider composition**

```bash
git add internal/app/github_executor.go internal/app/github_executor_test.go internal/app/providers.go internal/app/providers_test.go
git commit -m "feat(app): build provider instances by identifier"
```

---

### Task 6: Assemble the fail-closed runtime and conditional services

**Files:**
- Modify: `internal/app/runtime.go:1-102`
- Modify: `internal/app/runtime_test.go:18-92`
- Create: `internal/app/runtime_provider_test.go`
- Modify: `internal/auth/index.go:1-78`
- Modify: `internal/runner/environment.go:1-25`
- Modify: `internal/server/github_test.go:35-40`

**Interfaces:**
- Consumes: every interface produced by Tasks 1 through 5.
- Produces: a runtime with a private provider map, token-free SSH environment, and optional GitHub executor.
- Produces: no GitHub gRPC registration for a Gitea-only runtime.

- [ ] **Step 1: Write failing runtime matrix tests**

Create `internal/app/runtime_provider_test.go` in package `app_test`. Add these tests:

```go
func TestNewRuntimeBuildsExplicitGitHubOnlyRuntime(t *testing.T)
func TestNewRuntimeBuildsLegacyGitHubOnlyRuntime(t *testing.T)
func TestNewRuntimeBuildsGiteaOnlyRuntimeWithoutGH(t *testing.T)
func TestNewRuntimeBuildsMixedAndMultipleProviderRuntime(t *testing.T)
func TestNewRuntimeRejectsDuplicatePrincipalAndProviderValue(t *testing.T)
```

For Gitea-only, set `tools.gh` to a missing path. Require `runtime.Tools.GH == ""`, `runtime.GitHub == nil`, and non-nil Git, policy, tokens, TLS, and server values.

For mixed and multiple providers, use two explicit GitHub records and one Gitea record. Require a non-nil GitHub executor and canonical `gh` and `ssh` paths.

For duplicate values, assign one valid RepoWolf token to a principal and provider. Require a nil runtime, an error before readiness, and no token text in the error.

- [ ] **Step 2: Run the runtime matrix and record the expected failure**

Run:

```bash
go test ./internal/app -run 'TestNewRuntimeBuilds|TestNewRuntimeRejects' -count=1
```

Expected: FAIL because `NewRuntime` still loads principal tokens separately, resolves `gh` unconditionally, and builds one adapter.

- [ ] **Step 3: Reorder `NewRuntime` around complete immutable dependencies**

Use this startup order in `internal/app/runtime.go`:

```go
cfg, err := config.LoadFile(configPath)
credentialSnapshot, err := credentials.Load(cfg, os.LookupEnv)
tlsConfig, err := tlsconfig.LoadServer(cfg.TLS.Certificate, cfg.TLS.PrivateKey)
githubRequired := hasProviderKind(cfg.Providers, config.ProviderGitHub)
tools, err := runner.ResolveTools(cfg.Tools, githubRequired, runner.LookPath)
policySnapshot, err := policy.New(cfg)
tokenFreeEnvironment := runner.TokenFreeEnvironment(os.Environ(), credentialSnapshot.EnvironmentNames())
providerRunner := &runner.Runner{}
instances, err := buildProviderInstances(cfg, credentialSnapshot, tools, tokenFreeEnvironment, providerRunner)
githubExecutor := buildGitHubExecutor(instances)
```

Then build audit, Git, and server dependencies. Pass the token-free environment only to `gitservice.New`.

Pass both GitHub policy and executor only when `githubExecutor != nil`:

```go
var githubPolicy *policy.Snapshot
if githubExecutor != nil {
	githubPolicy = policySnapshot
}
```

Keep the Git service registered for every runtime. Mark readiness only after server construction succeeds.

Change `Runtime` fields to:

```go
type Runtime struct {
	Config         config.Config
	Tokens         *auth.Index
	TLSConfig      *tls.Config
	Tools          runner.Toolset
	Policy         *policy.Snapshot
	SSHEnvironment []string
	GitHub         server.GitHubExecutor
	Git            *gitservice.Service
	Server         *server.Server
	providers      map[string]providerInstance
}
```

Remove `tokenEnvironmentNames`. Remove the temporary `auth.Load`, `auth.LookupEnv`, and `runner.ProviderEnvironment` compatibility surfaces. Copy `SSHEnvironment` into the runtime snapshot.

- [ ] **Step 4: Prove service registration follows complete dependencies**

Add this server test:

```go
func TestNewOmitsGitHubServiceWhenDependenciesAbsent(t *testing.T) {
	service := testServer(t, Options{})
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GitHubService"]; ok {
		t.Fatal("GitHub service was registered")
	}
}
```

Keep the existing test which registers GitHub when both policy and executor exist. Keep `validateOptions` pairing rules unchanged.

- [ ] **Step 5: Run app, server, policy, and Git service tests**

Run:

```bash
gofmt -w internal/app/runtime.go internal/app/runtime_test.go internal/app/runtime_provider_test.go internal/auth/index.go internal/runner/environment.go internal/server/github_test.go
go test ./internal/app ./internal/auth ./internal/runner ./internal/server ./internal/policy ./internal/gitservice -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit runtime composition**

```bash
git add internal/app/runtime.go internal/app/runtime_test.go internal/app/runtime_provider_test.go internal/auth/index.go internal/runner/environment.go internal/server/github_test.go
git commit -m "feat(app): compose conditional provider runtime"
```

---

### Task 7: Prove mixed-runtime parity and credential isolation end to end

**Files:**
- Modify: `integration/testdata/policy.yaml:9-33`
- Modify: `integration/testdata/fake-provider.sh:11-21`
- Modify: `integration/testdata/fake-ssh.sh:9-22`
- Modify: `integration/forge_test.go:15-108,167-210`
- Modify: `integration/git_test.go:123-180`
- Modify: `integration/bubblewrap_test.go:121-164`
- Modify: `integration/leak_test.go:18-137`
- Modify: `integration/audit_assertions_test.go:185-187`
- Modify: `internal/testutil/server.go:53-96`
- Create: `integration/runtime_isolation_test.go`

**Interfaces:**
- Consumes: the real service and client binaries through `internal/testutil`.
- Produces: executable proof of GitHub parity, SSH isolation, mixed Gitea state, and safe errors.

- [ ] **Step 1: Make the integration policy explicit and mixed**

Add `tokenEnv: REPOWOLF_TOKEN_GITHUB` to the existing GitHub record.

Add this provider under `providers`:

```yaml
  gitea-lab:
    kind: gitea
    apiHost: gitea.example.invalid
    gitHost: gitea.example.invalid
    sshUser: git
    sshPort: 2222
    tokenEnv: REPOWOLF_TOKEN_GITEA
```

Add this ungranted record under `repositories`:

```yaml
  gitea-unused:
    provider: gitea-lab
    owner: alpha
    name: gitea-repo
    git:
      denyDeletes: true
      maxRefUpdates: 16
```

Keep all existing GitHub grants unchanged. This proves that Gitea state can exist without adding a Gitea operation.

- [ ] **Step 2: Strengthen fake child environment recording**

Make both fake scripts record these names:

```sh
printf 'GH_TOKEN=%s\n' "${GH_TOKEN-unset}"
printf 'GITHUB_TOKEN=%s\n' "${GITHUB_TOKEN-unset}"
printf 'GH_PROMPT_DISABLED=%s\n' "${GH_PROMPT_DISABLED-unset}"
printf 'GH_NO_UPDATE_NOTIFIER=%s\n' "${GH_NO_UPDATE_NOTIFIER-unset}"
printf 'NO_COLOR=%s\n' "${NO_COLOR-unset}"
printf 'REPOWOLF_TOKEN_AGENT=%s\n' "${REPOWOLF_TOKEN_AGENT-unset}"
printf 'REPOWOLF_TOKEN_GITHUB=%s\n' "${REPOWOLF_TOKEN_GITHUB-unset}"
printf 'REPOWOLF_TOKEN_GITEA=%s\n' "${REPOWOLF_TOKEN_GITEA-unset}"
printf 'SSH_AUTH_SOCK=%s\n' "${SSH_AUTH_SOCK-unset}"
printf 'GIT_PROTOCOL=%s\n' "${GIT_PROTOCOL-unset}"
```

The fake GitHub provider receives `GH_TOKEN` and the three fixed controls. The fake SSH process reports every credential as unset.

- [ ] **Step 3: Update broker fixtures with distinct source values**

Add these constants beside `providerCredential`:

```go
const (
	giteaCredential       = "gitea-provider-credential-marker"
	ambientGHCredential   = "ambient-gh-must-be-removed"
	ambientGitHubCredential   = "ambient-github-must-be-removed"
)
```

Replace ambient provider setup in every broker fixture with:

```go
"REPOWOLF_TOKEN_AGENT=" + agentToken,
"REPOWOLF_TOKEN_GITHUB=" + providerCredential,
"REPOWOLF_TOKEN_GITEA=" + giteaCredential,
"GH_TOKEN=" + ambientGHCredential,
"GITHUB_TOKEN=" + ambientGitHubCredential,
"SSH_AUTH_SOCK=/run/test-agent.sock",
"GIT_PROTOCOL=version=2",
```

Do not reuse any value across these sources.

- [ ] **Step 4: Add exact provider and SSH assertions**

Require the provider log to contain:

```text
GH_TOKEN=task13-provider-credential-marker
GITHUB_TOKEN=unset
GH_PROMPT_DISABLED=1
GH_NO_UPDATE_NOTIFIER=1
NO_COLOR=1
REPOWOLF_TOKEN_AGENT=unset
REPOWOLF_TOKEN_GITHUB=unset
REPOWOLF_TOKEN_GITEA=unset
```

Require the SSH log to contain:

```text
GH_TOKEN=unset
GITHUB_TOKEN=unset
REPOWOLF_TOKEN_AGENT=unset
REPOWOLF_TOKEN_GITHUB=unset
REPOWOLF_TOKEN_GITEA=unset
SSH_AUTH_SOCK=/run/test-agent.sock
GIT_PROTOCOL=version=2
```

Add `giteaCredential` and both ambient markers to every leak-marker assertion.

Update `TestMarkersRemainInTheirIntendedChannels` with these exact location rules:

```go
providerCredential:  {"provider.environment": true},
giteaCredential:     {},
ambientGHCredential: {},
ambientGitHubCredential: {},
```

Remove `"ssh.environment": true` from the `providerCredential` rule. Add all four provider values to `auditLeakMarkers()`.

Keep the existing client output, provider arguments, provider input, gRPC status, audit order, Git stream, and repository-state assertions unchanged.

- [ ] **Step 5: Add the real-process duplicate-value startup proof**

Add this helper to `internal/testutil/server.go`:

```go
// StartServerFailure starts one service attempt and returns its sanitized startup failure.
func StartServerFailure(t testing.TB, options ServerOptions) string {
	t.Helper()
	server, err := startServer(t, options, serverStartSettings{
		attempts:         1,
		readinessTimeout: defaultReadinessTimeout,
		address:          reserveAddress,
	})
	if err != nil {
		return err.Error()
	}
	_ = server.stop()
	t.Fatal("service reached readiness")
	return ""
}
```

Create `integration/runtime_isolation_test.go` with this test:

```go
func TestBrokerRejectsDuplicateCredentialBeforeReadiness(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	binaries := testutil.BuildBinaries(t, binDir)
	provider := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	failure := testutil.StartServerFailure(t, testutil.ServerOptions{
		Binary:      binaries.Service,
		PolicyPath:  filepath.Join("testdata", "policy.yaml"),
		Certificate: certificate,
		GHPath:      provider,
		SSHPath:     ssh,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken,
			"REPOWOLF_TOKEN_GITHUB=" + agentToken,
			"REPOWOLF_TOKEN_GITEA=" + giteaCredential,
		},
	})
	if !strings.Contains(failure, "REPOWOLF_TOKEN_AGENT") ||
		!strings.Contains(failure, "REPOWOLF_TOKEN_GITHUB") ||
		!strings.Contains(failure, "duplicate") {
		t.Fatalf("startup failure = %q", failure)
	}
	if strings.Contains(failure, agentToken) || strings.Contains(failure, giteaCredential) {
		t.Fatalf("startup failure disclosed a credential: %q", failure)
	}
}
```

The helper fails the test if the service reaches readiness.

- [ ] **Step 6: Run the focused integration tests**

Run:

```bash
gofmt -w internal/testutil/server.go integration/runtime_isolation_test.go integration/forge_test.go integration/git_test.go integration/bubblewrap_test.go integration/leak_test.go integration/audit_assertions_test.go
sh -n integration/testdata/fake-provider.sh integration/testdata/fake-ssh.sh
go test ./integration -run 'TestRestrictedGH|TestRealGit|TestBubblewrap|TestMarkersRemainInTheirIntendedChannels|TestBrokerRejectsDuplicateCredentialBeforeReadiness' -count=1
```

Expected: PASS. The tests use the real broker and client binaries with fake provider processes.

- [ ] **Step 7: Commit the end-to-end proof**

```bash
git add internal/testutil/server.go integration/testdata/policy.yaml integration/testdata/fake-provider.sh integration/testdata/fake-ssh.sh integration/forge_test.go integration/git_test.go integration/bubblewrap_test.go integration/leak_test.go integration/audit_assertions_test.go integration/runtime_isolation_test.go
git commit -m "test(integration): prove provider credential isolation"
```

---

### Task 8: Update active documentation and runnable examples

**Files:**
- Modify: `docs/configuration.md:6-10,43-90`
- Modify: `README.md:80-102`
- Modify: `scripts/repowolf-dogfood.sh:25-43,115-126`
- Modify: `examples/docker/.env.example:1-4`
- Modify: `examples/docker/compose.yaml:3-10`
- Modify: `examples/docker/config/repowolf.yaml:12-19`
- Modify: `examples/docker/config/repowolf-host.yaml:12-19`
- Modify: `examples/docker/README.md:25-38,109-158`
- Modify: `examples/docker/bootstrap.sh:159-165`
- Modify: `examples/docker/test-install-host-principal.sh:143-158,264-271`
- Modify: `scripts/ci/docker-example/bootstrap-disposable-state.sh:20-35`
- Modify: `scripts/ci/docker-example/assert-sandbox-boundary.sh:1-12`
- Modify: `scripts/ci/docker-example/assert-github-policy.sh:1-30`

**Interfaces:**
- Consumes: the final configuration and credential-source contract.
- Produces: runnable examples which use explicit provider token names.
- Produces: migration guidance without changing the configuration API version.

- [ ] **Step 1: Update the configuration guide**

Add `tokenEnv: REPOWOLF_TOKEN_GITHUB_PUBLIC` to the provider example. State these rules in plain language:

- A provider record stores only a token environment name.
- New GitHub and all Gitea records require an explicit `tokenEnv`.
- One GitHub record can omit the field during migration.
- Legacy migration accepts exactly one non-empty `GH_TOKEN` or `GITHUB_TOKEN`.
- Empty, null, both-present, and both-absent legacy states stop startup.
- Gitea provider records are runtime state only in Issue 1. They do not enable Gitea operations.

Replace the ambient provider-auth sentence with an explicit export example:

```sh
export REPOWOLF_TOKEN_GITHUB_PUBLIC='<provider-token>'
export REPOWOLF_TOKEN_EXAMPLE_AGENT='<generated-principal-token>'
```

- [ ] **Step 2: Update dogfood startup**

Add this provider field to the generated policy:

```yaml
    tokenEnv: REPOWOLF_TOKEN_GITHUB_PUBLIC
```

Load the real token into the configured source:

```bash
REPOWOLF_TOKEN_GITHUB_PUBLIC=$(real_gh_token)
if [ -z "$REPOWOLF_TOKEN_GITHUB_PUBLIC" ]; then
  echo "repowolf-dogfood: $(real_gh) auth token returned empty; cannot start broker" >&2
  return 1
fi
export REPOWOLF_TOKEN_GITHUB_PUBLIC
```

Do not export ambient `GH_TOKEN` from the broker wrapper.

- [ ] **Step 3: Update Compose and native-host examples**

Use `REPOWOLF_TOKEN_GITHUB_PUBLIC` in `.env.example`, `compose.yaml`, both example configuration files, README commands, bootstrap output, and service-environment fixtures.

Keep the generated GitHub child variable named `GH_TOKEN`. The operator-facing source name and child destination name are intentionally different.

Add this sandbox boundary assertion:

```sh
test -z "${REPOWOLF_TOKEN_GITHUB_PUBLIC+x}"
```

Update the disposable-state script to write:

```sh
printf 'REPOWOLF_TOKEN_GITHUB_PUBLIC=dummy-ci-token\n' >"$EXAMPLE_DIR/.env"
```

- [ ] **Step 4: Verify static documentation and shell changes directly**

The Testing Value Gate excludes new tests which only restate static YAML or documentation. Use direct checks instead.

Run:

```bash
bash -n scripts/repowolf-dogfood.sh examples/docker/bootstrap.sh examples/docker/install-host-broker.sh examples/docker/install-host-principal.sh examples/docker/test-install-host-principal.sh scripts/ci/docker-example/bootstrap-disposable-state.sh scripts/ci/docker-example/assert-github-policy.sh
sh -n scripts/ci/docker-example/assert-sandbox-boundary.sh integration/testdata/fake-provider.sh integration/testdata/fake-ssh.sh
nix develop -c scripts/ci/docker-example/lint.sh
REPOWOLF_TOKEN_GITHUB_PUBLIC=dummy REPOWOLF_TOKEN_AGENT=dummy docker compose -f examples/docker/compose.yaml config >/dev/null
rg -n 'kind: github|tokenEnv:|REPOWOLF_TOKEN_GITHUB_PUBLIC' README.md docs/configuration.md scripts/repowolf-dogfood.sh examples/docker/.env.example examples/docker scripts/ci/docker-example
```

Expected: shell syntax, shell lint, and Compose rendering pass. The search covers every modified active documentation, configuration, environment, and shell surface.

- [ ] **Step 5: Run the full repository verification gate**

Run:

```bash
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
git diff --check
```

Expected: every command exits zero. No generated Protobuf file changes because Issue 1 changes no API contract.

- [ ] **Step 6: Confirm scope and secret safety**

Run:

```bash
git status --short
git diff --name-only 5480835..HEAD
git diff -- docs/configuration.md README.md scripts/repowolf-dogfood.sh examples/docker scripts/ci/docker-example
```

Expected: only Issue 1 implementation, tests, active documentation, and examples appear. No real credential value appears.

- [ ] **Step 7: Commit active documentation and examples**

```bash
git add docs/configuration.md README.md scripts/repowolf-dogfood.sh examples/docker/.env.example examples/docker/compose.yaml examples/docker/config/repowolf.yaml examples/docker/config/repowolf-host.yaml examples/docker/README.md examples/docker/bootstrap.sh examples/docker/test-install-host-principal.sh scripts/ci/docker-example/bootstrap-disposable-state.sh scripts/ci/docker-example/assert-sandbox-boundary.sh scripts/ci/docker-example/assert-github-policy.sh
git commit -m "docs(config): require explicit provider token names"
```

- [ ] **Step 8: Verify the completed branch state**

Run:

```bash
git status --short
git log --oneline 5480835..HEAD
```

Expected: the worktree is clean. The log contains this plan commit and the eight task commits.
