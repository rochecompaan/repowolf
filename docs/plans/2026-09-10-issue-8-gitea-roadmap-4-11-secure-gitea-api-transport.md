# Secure Gitea API Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Construct one pinned, security-bounded Gitea SDK client per configured Gitea provider without adding any Gitea RPC or `tea` command.

**Architecture:** A new `internal/providerhttp` package owns outbound authority validation, token injection, TLS roots, redirect policy, deadlines, and decoded-response accounting. `internal/provider/gitea` supplies only the host-to-base-URL conversion and SDK construction, while `internal/app` remains the closed composition root and stores one concrete SDK client per Gitea provider record. Mechanical Go 1.26 and dependency changes are verified directly rather than with tests that restate configuration files.

**Tech Stack:** Go 1.26, `net/http`, `crypto/tls`, `crypto/x509`, `code.gitea.io/sdk/gitea v0.25.1`, YAML v3, Nix flakes, GitHub Actions, GoReleaser, and controlled TLS test servers.

**Spec:** `docs/specs/2026-09-10-issue-8-gitea-roadmap-4-11-secure-gitea-api-transport-design.md`

## Global Constraints

- Keep configuration API `repowolf.dev/v1alpha1`; `caFile` is additive and Gitea-only.
- Require Go 1.26 in Go, Nix development/package/check, CI, OCI, and release paths.
- Pin the production dependency to exactly `code.gitea.io/sdk/gitea v0.25.1`; add no production `tea` dependency or executable.
- Production Gitea API URLs are exactly `https://<validated-apiHost>/`; do not add plain HTTP, custom ports, paths, queries, fragments, user info, skip-verification, or server-name overrides.
- Bind authority, token, trust roots, redirect policy, deadline, and response accounting to each individual provider client.
- Inject `Authorization: token <provider-token>` only after exact HTTPS authority validation; never mutate the caller's request or disclose credentials in URLs, logs, errors, or body diagnostics.
- Require TLS 1.3 and normal hostname verification. Optional `caFile` bundles augment, never replace, system roots and are bounded to 1 MiB.
- Reject every redirect, including same-authority redirects and status codes 301, 302, 303, 307, and 308.
- Cap each whole Gitea operation at two minutes; a shorter caller context wins.
- Limit every response body to 8 MiB after HTTP content decoding; exactly 8 MiB succeeds and one additional decoded byte fails.
- Client/root construction performs no network request. Provider availability and token validity remain lazy concerns.
- Keep GitHub, Git/SSH, policy, audit, readiness, and existing integration behavior unchanged.
- Add no Protobuf files, generated protocol changes, gRPC methods, services, capabilities, audit operations, provider registry, generic forge API, retries, or restricted `tea` commands.

## File Structure

### New files

- `internal/providerhttp/errors.go`: stable sentinel errors for safe transport classification.
- `internal/providerhttp/authority.go`: normalized HTTPS authority comparison and authenticated request cloning.
- `internal/providerhttp/roots.go`: bounded regular-file CA loading onto system roots.
- `internal/providerhttp/response.go`: streaming decoded-body budget and close/cancellation behavior.
- `internal/providerhttp/client.go`: immutable options, base transport assembly, deadline, and redirect policy.
- `internal/providerhttp/authority_test.go`: exact-authority, non-mutation, dispatch, and token-leak tests.
- `internal/providerhttp/roots_test.go`: system/private-root and invalid-file tests.
- `internal/providerhttp/client_test.go`: redirects, TLS 1.3, hostname, timeout, cancellation, and response-budget tests.
- `internal/provider/gitea/client.go`: narrow production and package-private test SDK constructors.
- `internal/provider/gitea/client_test.go`: controlled TLS SDK call and isolated-client tests.

### Modified files

- `go.mod`, `go.sum`: Go 1.26 and exact Gitea SDK dependency.
- `flake.nix`, `devenv.nix`: explicit `go_1_26` development toolchain.
- `nix/package-server.nix`, `nix/package-client.nix`, `nix/bubblewrap-check.nix`: matching `buildGo126Module` builders and refreshed module hashes.
- `.github/workflows/ci.yml`, `.github/workflows/release.yml`: verify the resolved Go major/minor in setup-go and Nix-backed release paths.
- `internal/config/types.go`, `internal/config/decode.go`, `internal/config/validate.go`: optional non-empty Gitea-only `caFile` field.
- `internal/config/provider_instance_test.go`, `internal/config/config_test.go`, `internal/config/testdata/valid.yaml`: strict field-state and compatibility coverage.
- `internal/app/providers.go`, `internal/app/providers_test.go`: build and retain one Gitea SDK client per provider instance.
- `internal/app/runtime_provider_test.go`: startup failure, Gitea-only, mixed, and multiple-client composition coverage.
- `docs/configuration.md`: document Gitea trust-root behavior and restart semantics.

---

### Task 1: Pin Go 1.26 and the Gitea SDK across build paths

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `flake.nix`
- Modify: `devenv.nix`
- Modify: `nix/package-server.nix`
- Modify: `nix/package-client.nix`
- Modify: `nix/bubblewrap-check.nix`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: the repository's existing Go/Nix/CI/OCI/GoReleaser build graph.
- Produces: Go 1.26 in every build path and exact module `code.gitea.io/sdk/gitea v0.25.1` for Task 5.
- Produces: Nix package functions that explicitly consume `buildGo126Module`.

- [ ] **Step 1: Update the Go source of truth and resolve the exact SDK module**

Run:

```bash
go mod edit -go=1.26
go get code.gitea.io/sdk/gitea@v0.25.1
go mod tidy
```

Then inspect `go.mod` and require a direct line exactly equivalent to:

```go
require code.gitea.io/sdk/gitea v0.25.1
```

Do not add `code.gitea.io/tea` or any executable dependency.

- [ ] **Step 2: Select Go 1.26 explicitly in Nix**

In both development environments replace unqualified Go with `go_1_26`:

```nix
# flake.nix devShell packages
packages = with pkgs; [ go_1_26 goreleaser jq shellcheck skopeo ];

# devenv.nix packages
pkgs.go_1_26
```

Change both package files from `{ lib, buildGoModule }` / `buildGoModule` to `{ lib, buildGo126Module }` / `buildGo126Module`. Change `nix/bubblewrap-check.nix` from `pkgs.buildGoModule` to `pkgs.buildGo126Module`. Keep OCI composition unchanged because `nix/oci.nix` consumes the Nix-built `repowolf` package.

- [ ] **Step 3: Add direct CI and release toolchain checks**

Immediately after each `actions/setup-go` step in `.github/workflows/ci.yml` and `.github/workflows/release.yml`, add:

```yaml
      - name: Confirm Go 1.26
        run: go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
```

In jobs which build releases only through `nix develop`, add or retain the same proof through:

```yaml
      - name: Confirm Nix Go 1.26
        run: nix develop -c sh -c "go env GOVERSION | grep -Eq '^go1[.]26([.]|$)'"
```

Do not add an automated test that parses YAML, `go.mod`, or Nix text; these commands directly verify the selected tools.

- [ ] **Step 4: Refresh fixed Nix module hashes**

Run each build, copy the reported expected hash into both package files and `nix/bubblewrap-check.nix`, and rerun until all succeed:

```bash
nix build .#repowolf --no-link --print-build-logs
nix build .#repowolf-client --no-link --print-build-logs
nix build .#checks.$(nix eval --impure --raw --expr builtins.currentSystem).bubblewrap --no-link --print-build-logs
```

Use the actual hashes emitted by Nix; do not guess them.

- [ ] **Step 5: Verify dependency and toolchain resolution directly**

Run:

```bash
go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
nix develop -c sh -c "go env GOVERSION | grep -Eq '^go1[.]26([.]|$)'"
go list -m -f '{{if eq .Path "code.gitea.io/sdk/gitea"}}{{.Path}} {{.Version}}{{end}}' all | grep -Fx 'code.gitea.io/sdk/gitea v0.25.1'
! go list -m all | grep -Eq '^code\.gitea\.io/tea([[:space:]]|$)'
go mod verify
```

Expected: both Go checks resolve 1.26, the SDK line matches exactly, no `tea` module is present, and module verification passes.

- [ ] **Step 6: Commit the build baseline**

```bash
git add go.mod go.sum flake.nix devenv.nix nix/package-server.nix nix/package-client.nix nix/bubblewrap-check.nix .github/workflows/ci.yml .github/workflows/release.yml
git commit -m "build: move all paths to Go 1.26"
```

---

### Task 2: Add strict Gitea-only `caFile` configuration

**Files:**
- Modify: `internal/config/types.go`
- Modify: `internal/config/decode.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/provider_instance_test.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/testdata/valid.yaml`

**Interfaces:**
- Consumes: strict `config.Decode(io.Reader)` and the existing validated host-only provider model.
- Produces: `config.Provider.CAFile string`, where `""` means syntactically omitted only.
- Produces: a non-empty configured path only for `ProviderGitea`; file I/O remains owned by Task 4.

- [ ] **Step 1: Write failing field-state and provider-kind tests**

Extend `internal/config/provider_instance_test.go` with table tests for omitted, non-empty string, empty string, null, non-string, direct-over-merge, and merge-only `caFile` states. Assert:

```go
if got := cfg.Providers["provider"].CAFile; got != wantCAFile {
	t.Fatalf("CAFile = %q, want %q", got, wantCAFile)
}
```

Add validation cases proving a non-empty `caFile` is accepted for Gitea and rejected for GitHub with an error naming both the provider ID and `caFile`. Keep existing Gitea records without `caFile` valid.

- [ ] **Step 2: Run the focused tests and observe failure**

```bash
go test ./internal/config -run 'CAFile|CaFile' -count=1
```

Expected: FAIL because `Provider.CAFile` and strict `caFile` state handling do not exist.

- [ ] **Step 3: Preserve omission while rejecting explicit empty/null values**

Add the public field:

```go
type Provider struct {
	Kind     ProviderKind `yaml:"kind"`
	APIHost  string       `yaml:"apiHost"`
	GitHost  string       `yaml:"gitHost"`
	SSHUser  string       `yaml:"sshUser"`
	SSHPort  uint16       `yaml:"sshPort"`
	TokenEnv string       `yaml:"tokenEnv"`
	CAFile   string       `yaml:"caFile"`
}
```

Represent `rawProvider.CAFile` with a presence-aware scalar decoder, as `tokenEnv` already does. Generalize the YAML-node null rejection helper so it checks both `tokenEnv` and `caFile`, including aliases and merge keys, and emits the correct field name. During normalization reject a present empty string before copying the value.

In `validateProvider`, add:

```go
if provider.CAFile != "" && provider.Kind != ProviderGitea {
	return fmt.Errorf("provider %q caFile is supported only for Gitea", id)
}
```

Do not access the filesystem in `Decode` or `Validate`.

- [ ] **Step 4: Keep shared fixtures explicitly compatible**

Add either an omitted `caFile` or one valid Gitea-only example where appropriate in `internal/config/config_test.go` and `internal/config/testdata/valid.yaml`. Do not add `caFile` to GitHub fixtures.

- [ ] **Step 5: Format and run configuration tests**

```bash
gofmt -w internal/config/types.go internal/config/decode.go internal/config/validate.go internal/config/provider_instance_test.go internal/config/config_test.go
go test ./internal/config -count=1
```

Expected: PASS, including omitted-field compatibility and strict empty/null rejection.

- [ ] **Step 6: Commit the configuration contract**

```bash
git add internal/config
git commit -m "feat(config): add Gitea trust root path"
```

---

### Task 3: Enforce exact authority, authentication, and redirects in `providerhttp`

**Files:**
- Create: `internal/providerhttp/errors.go`
- Create: `internal/providerhttp/authority.go`
- Create: `internal/providerhttp/client.go`
- Create: `internal/providerhttp/authority_test.go`
- Create: `internal/providerhttp/client_test.go`

**Interfaces:**
- Consumes: one immutable expected HTTPS authority, one provider token, and an optional injected `http.RoundTripper`.
- Produces: `providerhttp.New(providerhttp.Options) (*http.Client, error)`.
- Produces: stable `ErrAuthority`, `ErrAuthorization`, and `ErrRedirect` sentinels usable through `errors.Is`.
- Produces: an accepted cloned request with exactly `Authorization: token <token>`; the input request/header remains unchanged.

- [ ] **Step 1: Write failing exact-authority authentication tests**

Use a recording fake round tripper and table cases for accepted DNS case folding, implicit versus explicit HTTPS port 443, and canonical IPv4/IPv6 literals. Add rejected cases for HTTP, wrong host, non-443/effective port, URL user info, malformed authority, and a pre-existing `Authorization` header.

For every rejection require:

```go
if base.calls != 0 { t.Fatal("rejected request reached base transport") }
if !errors.Is(err, providerhttp.ErrAuthority) { /* or ErrAuthorization */ }
if strings.Contains(err.Error(), token) { t.Fatal("error disclosed token") }
```

For acceptance require one base call, exactly `token <provider-token>`, and deep equality of the original URL and headers before/after `RoundTrip`.

- [ ] **Step 2: Run the authority tests and observe failure**

```bash
go test ./internal/providerhttp -run 'Authority|Authorization' -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement normalized authority and request cloning**

Define the narrow options surface in `client.go`:

```go
type Options struct {
	Authority         string
	Token             string
	CAFile            string
	Timeout           time.Duration
	MaxResponseBytes  int64
	Base              http.RoundTripper
}

func New(options Options) (*http.Client, error)
```

Production callers pass host-only `Authority`, `2*time.Minute`, and `8<<20`. Tests may inject shorter positive limits and a base transport. Validate options without returning the token.

In `authority.go`, normalize DNS with ASCII case folding and IP literals with `netip.ParseAddr`; treat omitted HTTPS port as 443. Before calling the base transport, reject non-HTTPS, user info, mismatched effective authority, or existing authorization. Clone with `request.Clone(request.Context())`, clone headers again explicitly, and set exactly one authorization value on the clone.

- [ ] **Step 4: Write failing redirect tests through `http.Client.Do`**

Use an HTTPS test server whose handlers return 301, 302, 303, 307, and 308, including same-authority and cross-authority `Location` values. Require `errors.Is(err, ErrRedirect)`, one request only, no replay of POST bodies, and errors which contain neither token nor redirect location.

- [ ] **Step 5: Implement unconditional redirect rejection**

Set `http.Client.CheckRedirect` to return a stable wrapped `ErrRedirect` without formatting the target URL. Keep the transport as the sole authorization owner; do not set SDK or default-client credentials.

- [ ] **Step 6: Format and run focused tests**

```bash
gofmt -w internal/providerhttp/errors.go internal/providerhttp/authority.go internal/providerhttp/client.go internal/providerhttp/authority_test.go internal/providerhttp/client_test.go
go test ./internal/providerhttp -run 'Authority|Authorization|Redirect' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit authority and redirect controls**

```bash
git add internal/providerhttp
git commit -m "feat(providerhttp): secure request authority"
```

---

### Task 4: Add trust roots, hard deadlines, and decoded response budgets

**Files:**
- Modify: `internal/providerhttp/errors.go`
- Modify: `internal/providerhttp/client.go`
- Modify: `internal/providerhttp/client_test.go`
- Create: `internal/providerhttp/roots.go`
- Create: `internal/providerhttp/roots_test.go`
- Create: `internal/providerhttp/response.go`

**Interfaces:**
- Consumes: `Options.CAFile`, `Timeout`, and `MaxResponseBytes` from Task 3.
- Produces: TLS configuration rooted in system certificates plus an optional bounded PEM bundle, with `MinVersion: tls.VersionTLS13`.
- Produces: stable `ErrTrustRoots`, `ErrTimeout`, and `ErrResponseLimit` sentinels.
- Produces: a streaming response body whose visible decoded-byte boundary is inclusive and whose `Close` closes the underlying body.

- [ ] **Step 1: Write failing CA-file tests**

In `roots_test.go`, generate a private test CA and prove an omitted path preserves public/system roots while the configured CA is appended. Add missing, empty, malformed, no-valid-certificate, oversized (`1<<20 + 1` bytes), directory, FIFO/non-regular, and unreadable-file cases. Skip only the unreadable permission case when running as a user that can still read it. Require `errors.Is(err, ErrTrustRoots)` and errors that name the path but contain no PEM body or token.

- [ ] **Step 2: Implement fail-closed bounded root loading**

Use this private shape:

```go
const maxCAFileBytes int64 = 1 << 20
func loadRootCAs(path string) (*x509.CertPool, error)
```

Call `x509.SystemCertPool()` and fail if it returns an error or nil pool. For a configured path, open it, require `Stat().Mode().IsRegular()`, read at most `maxCAFileBytes+1`, reject empty/oversized input, and require `AppendCertsFromPEM` to return true. Never replace the system pool.

When no base transport is injected, construct a dedicated `http.Transport` with a cloned TLS config containing:

```go
&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
```

Leave `ServerName` empty so Go derives it from the validated request host and performs normal hostname verification.

- [ ] **Step 3: Write failing TLS and operation-deadline tests**

Use controlled TLS servers to prove: a private CA succeeds only when appended, an untrusted CA fails, a hostname mismatch fails, and a TLS 1.2-only server fails. Add a stalled-header server and a response body that stalls after headers. Inject a short timeout for tests, require `errors.Is(err, ErrTimeout)` or `context.DeadlineExceeded`, and prove a still-shorter caller deadline/cancellation wins and closes the body.

- [ ] **Step 4: Implement the whole-operation deadline**

Bind the configured timeout to each cloned request context before dispatch and retain its cancel function until the response body closes, reaches EOF, hits a limit, or returns an error. Map only transport-owned deadline expiry to `ErrTimeout`; preserve caller `context.Canceled` and `context.DeadlineExceeded`. Ensure header wait, upload, and body reads all observe the same deadline. Also set `http.Client.Timeout` to the same value as a defense-in-depth hard cap.

- [ ] **Step 5: Write failing decoded-response budget tests**

For both 200 and error statuses, test zero/short bodies, exactly `8<<20`, and `8<<20 + 1`. Add gzip responses so the compressed wire body is small but decoded content crosses the limit. Read in small and large chunks, and prove no over-limit bytes are returned to the caller. Use a recording `io.ReadCloser` to prove explicit close, EOF, limit failure, and cancellation close the provider body.

- [ ] **Step 6: Implement streaming inclusive response accounting**

In `response.go`, wrap the body visible after the base `RoundTripper` returns. Read at most `remaining+1`; return the final in-budget bytes normally at exactly the boundary, but if one additional byte is observed, close the underlying body and return `ErrResponseLimit` without body content. Apply the wrapper regardless of status code. Preserve streaming and make `Close` idempotently cancel the deadline and close the provider body.

- [ ] **Step 7: Run the full transport package under the race detector**

```bash
gofmt -w internal/providerhttp
go test -race ./internal/providerhttp -count=1
```

Expected: PASS for authority, trust roots, TLS, redirects, timeout/cancellation, decoded limits, closure, and leak assertions.

- [ ] **Step 8: Commit bounded HTTP behavior**

```bash
git add internal/providerhttp
git commit -m "feat(providerhttp): bound TLS API responses"
```

---

### Task 5: Construct and exercise the pinned Gitea SDK client

**Files:**
- Create: `internal/provider/gitea/client.go`
- Create: `internal/provider/gitea/client_test.go`

**Interfaces:**
- Consumes: validated `config.Provider`, its bound token, and `providerhttp.New`.
- Produces: `providergitea.New(config.Provider, string) (*gitea.Client, error)`.
- Produces: package-private `newClient(baseURL string, options providerhttp.Options) (*gitea.Client, error)` for controlled HTTPS test servers only.
- Produces: an SDK configured with `gitea.SetHTTPClient(httpClient)` and `gitea.SetGiteaVersion("")`, but never `gitea.SetToken`.

- [ ] **Step 1: Write failing constructor and no-network tests**

Add compile-time and behavior tests for the production constructor:

```go
func New(provider config.Provider, token string) (*sdk.Client, error)
```

Require it to derive `https://<apiHost>/`, pass `provider.CAFile`, use the hard constants `2*time.Minute` and `8<<20`, and reject non-Gitea/invalid direct records safely. Use an injected base which would fail the test if called during construction; this proves construction does not probe identity or version.

- [ ] **Step 2: Implement narrow SDK construction without SDK authentication**

In `client.go`, define:

```go
const (
	operationTimeout = 2 * time.Minute
	maxResponseBytes = int64(8 << 20)
)
```

The exported path accepts only validated host-only provider data and derives the URL. The package-private seam parses an explicit HTTPS test base URL, derives its authority, and calls `providerhttp.New`. Construct the SDK with:

```go
sdk.NewClient(baseURL,
	sdk.SetHTTPClient(httpClient),
	sdk.SetGiteaVersion(""),
)
```

`SetGiteaVersion("")` is required because SDK v0.25.1 otherwise calls `/api/v1/version` during `NewClient`; skipping SDK version discovery preserves lazy startup. Never pass `sdk.SetToken`.

- [ ] **Step 3: Write the controlled TLS SDK demonstration**

Create a TLS server presenting a certificate from a generated test CA and implement the SDK version endpoint. Call:

```go
version, _, err := client.ServerVersion()
```

Require the exact request path, TLS authority, `Authorization: token <expected>`, and returned version. Add cases where the endpoint redirects, stalls, returns more than 8 MiB decoded JSON/error content, uses an untrusted certificate, or is called through a mismatched authority. Assert stable failures and absence of token, redirect location, and response body in errors.

- [ ] **Step 4: Prove two SDK clients cannot cross-route credentials**

Build two clients with distinct TLS authorities, tokens, and CA files. Each must reach only its matching endpoint and send only its matching token. Attempt cross-authority requests through each client's underlying transport test seam and require pre-dispatch `ErrAuthority` with zero token leakage.

- [ ] **Step 5: Run SDK and transport tests**

```bash
gofmt -w internal/provider/gitea/client.go internal/provider/gitea/client_test.go
go test -race ./internal/providerhttp ./internal/provider/gitea -count=1
go list -m code.gitea.io/sdk/gitea | grep -Fx 'code.gitea.io/sdk/gitea v0.25.1'
```

Expected: all controlled-server cases pass and the exact SDK remains pinned.

- [ ] **Step 6: Commit SDK construction**

```bash
git add internal/provider/gitea
git commit -m "feat(gitea): construct bounded SDK client"
```

---

### Task 6: Compose one Gitea client per runtime provider

**Files:**
- Modify: `internal/app/providers.go`
- Modify: `internal/app/providers_test.go`
- Modify: `internal/app/runtime_provider_test.go`

**Interfaces:**
- Consumes: `providergitea.New(config.Provider, string)` from Task 5 and provider-ID credential bindings from `credentials.Snapshot`.
- Produces: `providerInstance.gitea *gitea.Client` for every Gitea record; no public registry or service.
- Preserves: existing `providerInstance.github server.GitHubExecutor` and trusted GitHub dispatch behavior.

- [ ] **Step 1: Write failing provider-instance client tests**

Update `providers_test.go` to require every Gitea instance to retain a non-nil concrete SDK client and every GitHub instance to retain a nil Gitea client. Replace the old assertion that Gitea merely stores raw inert token state. Add two Gitea records and verify their client pointers differ.

Add an injectable package-private Gitea constructor function parameter or narrow variable seam to the provider builder tests so they can record provider ID-derived configuration and tokens without network calls. Require sorted construction order, exact provider/token pairing, no GitHub regression, and nil result on any constructor error.

- [ ] **Step 2: Run focused app tests and observe failure**

```bash
go test ./internal/app -run 'TestBuildProviderInstances|TestNewRuntimeBuilds' -count=1
```

Expected: FAIL because Gitea provider instances do not yet contain SDK clients.

- [ ] **Step 3: Extend the closed composition switch**

Add the concrete slot:

```go
type providerInstance struct {
	provider config.Provider
	token    string
	legacy   bool
	github   server.GitHubExecutor
	gitea    *giteasdk.Client
}
```

In the existing `case config.ProviderGitea`, call the narrow constructor and wrap errors with the provider ID but never the token:

```go
client, err := newGiteaClient(provider, token)
if err != nil {
	return nil, fmt.Errorf("create Gitea provider %q: %w", id, err)
}
instance.gitea = client
```

Keep the sorted closed switch, private map, and GitHub executor assembly. Do not add a Gitea server executor yet.

- [ ] **Step 4: Add runtime startup and compatibility coverage**

In `runtime_provider_test.go`, give the Gitea-only fixture a valid local CA file where needed and retain the assertion that no `gh` executable is resolved. Add a malformed/missing CA case which requires `app.NewRuntime` to return nil before readiness and an error naming the provider/path but no token. Keep mixed GitHub/Gitea startup green.

Because runtime internals are private, assert multiple-client retention in package `app` tests; use external `app_test` tests only for startup/readiness and observable dependency behavior.

- [ ] **Step 5: Run app and adjacent package tests**

```bash
gofmt -w internal/app/providers.go internal/app/providers_test.go internal/app/runtime_provider_test.go
go test -race ./internal/app ./internal/config ./internal/credentials ./internal/providerhttp ./internal/provider/gitea ./internal/runner ./internal/server -count=1
```

Expected: PASS; Gitea-only still needs no `gh`, mixed GitHub behavior remains unchanged, and invalid client construction prevents readiness.

- [ ] **Step 6: Commit runtime composition**

```bash
git add internal/app/providers.go internal/app/providers_test.go internal/app/runtime_provider_test.go
git commit -m "feat(app): retain per-provider Gitea clients"
```

---

### Task 7: Document trust behavior and run the repository verification gate

**Files:**
- Modify: `docs/configuration.md`

**Interfaces:**
- Consumes: the completed configuration and transport behavior.
- Produces: operator guidance for optional Gitea `caFile`, restart requirements, and immutable security limits.
- Produces: final direct evidence for Go/Nix/CI/OCI/release configuration without tests that restate static files.

- [ ] **Step 1: Update the active configuration guide**

Add a Gitea example:

```yaml
  gitea-lab:
    kind: gitea
    apiHost: gitea.example.com
    gitHost: gitea.example.com
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITEA_LAB
    caFile: /run/repowolf/gitea-ca.pem
```

State that `caFile` is optional, non-empty, Gitea-only, bounded to a 1 MiB regular readable PEM file, and augments system roots. Document TLS 1.3, hostname verification, universal redirect rejection, two-minute hard operations, 8 MiB decoded responses, and restart requirements. Do not claim any Gitea RPC or `tea` command exists.

- [ ] **Step 2: Verify static build configuration directly**

The Testing Value Gate excludes new tests that merely inspect dependency versions, workflow YAML, Nix text, or documentation. Run direct resolution/build checks instead:

```bash
go env GOVERSION | grep -Eq '^go1\.26([.]|$)'
nix develop -c sh -c "go env GOVERSION | grep -Eq '^go1[.]26([.]|$)'"
go list -m code.gitea.io/sdk/gitea | grep -Fx 'code.gitea.io/sdk/gitea v0.25.1'
! go list -m all | grep -Eq '^code\.gitea\.io/tea([[:space:]]|$)'
go mod verify
nix build .#repowolf .#repowolf-client .#ociImage --no-link --print-build-logs
nix develop -c goreleaser build --snapshot --clean
```

Expected: all selected tools/builds use Go 1.26, the exact SDK is present, no `tea` module is present, and server/client/OCI/release builds succeed.

- [ ] **Step 3: Run project validation commands**

No `AGENTS.md` exists in this worktree or its repository parents, so use the repository's CI-defined validation gate:

```bash
go tool buf lint
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
scripts/ci/oci/smoke-image.sh "$(go env GOARCH)"
git diff --check
```

Expected: every command exits zero. `scripts/check-generated.sh` reports no generated Protobuf change; release archives and the OCI image smoke successfully on the native architecture.

- [ ] **Step 4: Confirm issue scope and security-sensitive omissions**

Run:

```bash
git status --short
git diff --name-only a988c95..HEAD
! git diff a988c95..HEAD --name-only | grep -E '(^proto/|^gen/|(^|/)tea(/|$))'
rg -n 'SetToken|InsecureSkipVerify|CheckRedirect|MinVersion|MaxResponseBytes|caFile' internal docs/configuration.md
```

Expected: only this issue's build, configuration, transport, SDK, runtime, tests, and documentation files changed; no Protobuf/generated/`tea` surface appears; SDK authentication and insecure TLS are absent; the expected bounded controls are visible.

- [ ] **Step 5: Commit documentation**

```bash
git add docs/configuration.md
git commit -m "docs(gitea): describe secure API transport"
```

- [ ] **Step 6: Verify the completed implementation branch**

```bash
git status --short
git log --oneline a988c95..HEAD
```

Expected: the worktree is clean and the log contains the plan commit plus the seven implementation task commits.
