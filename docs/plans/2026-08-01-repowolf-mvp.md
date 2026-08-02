# RepoWolf MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a standalone, sandbox-agnostic RepoWolf service and credential-free client shims that broker exact-repository GitHub, Gitea, and Git SSH operations over authenticated TLS gRPC.

**Architecture:** `repowolf serve` loads strict YAML, environment-sourced bearer tokens, TLS material, policy, and pinned provider tools into an immutable runtime snapshot. A separate multicall `repowolf-client` binary runs as restricted `gh`, `tea`, and `repowolf-git-ssh` shims; unary typed RPCs handle forge operations and bounded bidirectional streams handle Git upload-pack and receive-pack.

**Tech Stack:** Go 1.25.10+, gRPC-Go v1.83.0, Protobuf-Go v1.36.11, protoc-gen-go-grpc v1.6.2, Buf v1.72.0, YAML v3.0.1, go-cmp v0.7.0, OpenSSH, Git, Nix flakes, OCI images, GitHub Actions.

**Design spec:** `docs/specs/2026-08-01-repowolf-mvp-design.md`

## Global Constraints

- The Go module is `github.com/rochecompaan/repowolf`; the minimum Go language version is `1.25.10`.
- Keep service/admin and client artifacts separate. A sandbox receives only `repowolf-client` and its `gh`, `tea`, and `repowolf-git-ssh` links.
- RepoWolf does not create, register, inspect, or attest sandboxes. Do not add `repowolf run`, sandbox drivers, or Unix-socket transport.
- All client traffic uses gRPC over HTTP/2 with mandatory server-side TLS. MVP client authentication is an opaque bearer token from `REPOWOLF_TOKEN`.
- Service token values come only from environment variables named in strict YAML. Never place token values or digests in YAML, logs, audit records, errors, argv, URLs, or provider input.
- Policy authority is principal × exact repository × capability. Never infer authority from working directories, Git remotes, request hosts, or repository-local files.
- The shared capability vocabulary is exactly `repository:read`, `issues:read`, `issues:write`, `pull_requests:read`, `pull_requests:write`, `actions:read`, `statuses:read`, `git:read`, and `git:write`.
- Pull-request merge, arbitrary API/GraphQL, workflow writes, release/repository administration, aliases, extensions, browser/editor launch, raw argv forwarding, and shell execution remain unsupported.
- Provider operations are provider-specific typed Protobuf messages. Clients cannot supply executable paths, hosts, SSH users/options, CLI profiles, URLs, headers, or argument boundaries.
- Resolve `gh`, `tea`, and `ssh` once at service startup, canonicalize them, and pin them for the process lifetime. Absolute YAML overrides are allowed.
- Preserve provider-authentication environment values byte-for-byte. Remove all configured RepoWolf token variables and internal `REPOWOLF_*` control variables before spawning provider tools.
- Provider tools run with generated argv, private temporary cwd, bounded I/O, private process groups, deadlines, cancellation, and complete reaping. They never open the agent checkout.
- Effective gRPC send/receive message limits are capped at 1 MiB in both directions. Git data chunks are 64 KiB; push-prefix buffering is capped at 1 MiB.
- Receive-pack validates syntax, capabilities, update count, delete policy, and denied refs before forwarding client update bytes. `refs/heads/main` is denied by default.
- An exact-main denial test uses only generated policy and an offline fake host; never test denial against a real forge main branch.
- GitHub/Gitea branch protections remain authoritative for ancestry-sensitive rules.
- Audit output is safe JSON Lines on stdout. Never audit bodies, comments, pack data, raw stdout/stderr, provider argv/environment, token material, or TLS private data.
- Configuration, tokens, certificates, and executable resolution are startup-only in MVP; changes require restart.
- Linux amd64/arm64 and macOS amd64/arm64 are MVP targets. Windows is deferred.
- Prefer focused modules under roughly 200 meaningful lines. Split parser, validator, transport, runner, provider, and rendering responsibilities; do not create `utils`, `helpers`, or other junk-drawer packages.
- Production behavior follows TDD. Static workflow, lockfile, packaging metadata, and documentation use direct syntax/build verification instead of tests that restate configuration.
- Each task ends with `gofmt`, focused tests, `go test -race` for affected packages where supported, and a Conventional Commits-style commit.
- Do not modify `roche-pi` or clubhouse in this plan. Their cutover receives a separate plan after standalone feature parity and review.

---

## Planned File Structure

| Path | Responsibility |
|---|---|
| `cmd/repowolf/main.go` | Service/admin command dispatch only |
| `cmd/repowolf-client/main.go` | Multicall client mode dispatch only |
| `proto/repowolf/v1/*.proto` | Versioned typed public protocol |
| `gen/repowolf/v1/*.pb.go` | Checked-in generated Protobuf/gRPC code |
| `scripts/` | Protobuf generation and generated-code reproducibility checks |
| `internal/config/` | Strict YAML decoding, defaults, semantic validation |
| `internal/app/` | Runtime assembly and service command lifecycle |
| `internal/cli/` | Admin command parsing and user-facing output |
| `internal/auth/` | Token generation, environment loading, constant-time digest matching, gRPC auth |
| `internal/tlsconfig/` | Server TLS loading and local certificate bootstrap |
| `internal/policy/` | Exact-repository authorization and push policy |
| `internal/runner/` | Pinned tools, provider environment, bounded process lifecycle |
| `internal/audit/` | Safe JSONL event schema and serialized writer |
| `internal/rpcstatus/` | Stable domain-to-gRPC/client error mapping |
| `internal/server/` | gRPC assembly, health, lifecycle, concurrency limits |
| `internal/provider/github/` | GitHub typed validation, command generation, output normalization |
| `internal/provider/gitea/` | Gitea typed validation, command generation, output normalization |
| `internal/gitproto/` | Git pkt-line, advertisement, capability, receive-pack parsing |
| `internal/gitservice/` | Upload-pack/receive-pack gRPC stream orchestration |
| `internal/clientconfig/` | Endpoint, CA, token, TLS gRPC dialing |
| `internal/client/github/` | Restricted `gh` parser and renderer |
| `internal/client/gitea/` | Restricted `tea` parser and renderer |
| `internal/client/gitssh/` | SSH argv parser and Git stream relay |
| `internal/testutil/` | Fake executable, certificate, and in-process server fixtures |
| `integration/` | TLS, fake provider, Git, Gitea, leak, and Bubblewrap tests |
| `nix/` and `flake.nix` | Split Nix service/client packages and checks |
| `nix/oci.nix` | OCI service image with host provider tools |
| `.goreleaser.yaml` | Cross-platform binary archives and checksums |
| `.github/workflows/` | Behavior verification and release automation |

Generated Protobuf files and test fixtures may exceed normal module-size guidance. Handwritten source files remain focused by responsibility.

---

### Task 1: Establish the Go, Protobuf, and command foundation

**Files:**
- Create: `go.mod`
- Create: `go.sum`
- Create: `buf.yaml`
- Create: `buf.gen.yaml`
- Create: `internal/protogen/generate.go`
- Create: `scripts/generate.sh`
- Create: `scripts/check-generated.sh`
- Create: `proto/repowolf/v1/common.proto`
- Create: `gen/repowolf/v1/common.pb.go`
- Create: `internal/buildinfo/version.go`
- Create: `cmd/repowolf/main.go`
- Create: `cmd/repowolf-client/main.go`
- Create: `internal/protogen/contract_test.go`
- Modify: `README.md`

**Interfaces:**
- Produces: Protobuf package `github.com/rochecompaan/repowolf/gen/repowolf/v1` with `RepositorySelector`, `RequestContext`, and `Repository` messages.
- Produces: `buildinfo.Version` set to `"dev"` unless overridden with `-ldflags`.
- Produces: command entry points that fail with concise usage until later task handlers are registered.

- [ ] **Step 1: Initialize the module and pin runtime/tool dependencies**

Run:

```bash
go mod init github.com/rochecompaan/repowolf
go mod edit -go=1.25.10
go get google.golang.org/grpc@v1.83.0
go get google.golang.org/protobuf@v1.36.11
go get gopkg.in/yaml.v3@v3.0.1
go get github.com/google/go-cmp@v0.7.0
go get -tool github.com/bufbuild/buf/cmd/buf@v1.72.0
go get -tool google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go get -tool google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
```

Expected: `go.mod` names the RepoWolf module, declares Go 1.25.10, and pins the listed modules/tools.

- [ ] **Step 2: Add the Buf configuration and common protocol**

Create `buf.yaml`:

```yaml
version: v2
modules:
  - path: proto
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

Create `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-go
    out: gen
    opt:
      - paths=source_relative
  - local: protoc-gen-go-grpc
    out: gen
    opt:
      - paths=source_relative
```

Create `proto/repowolf/v1/common.proto`:

```proto
syntax = "proto3";

package repowolf.v1;

option go_package = "github.com/rochecompaan/repowolf/gen/repowolf/v1;repowolfv1";

message RepositorySelector {
  string host = 1;
  string owner = 2;
  string name = 3;
  uint32 ssh_port = 4;
}

message RequestContext {
  RepositorySelector repository = 1;
}

message Repository {
  string id = 1;
  string provider_id = 2;
  string owner = 3;
  string name = 4;
}
```

Create `scripts/generate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
tools="$(mktemp -d)"
trap 'rm -rf "$tools"' EXIT
cd "$root"
go build -o "$tools/protoc-gen-go" google.golang.org/protobuf/cmd/protoc-gen-go
go build -o "$tools/protoc-gen-go-grpc" google.golang.org/grpc/cmd/protoc-gen-go-grpc
PATH="$tools:$PATH" go tool buf generate
```

Create `scripts/check-generated.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
snapshot="$(mktemp -d)"
trap 'rm -rf "$snapshot"' EXIT
cp -R "$root/gen" "$snapshot/gen"
"$root/scripts/generate.sh"
diff -ru "$snapshot/gen" "$root/gen"
```

Create `internal/protogen/generate.go`:

```go
package protogen

//go:generate ../../scripts/generate.sh
```

Run:

```bash
chmod +x scripts/generate.sh scripts/check-generated.sh
```

- [ ] **Step 3: Generate code and write the protocol round-trip test**

Run:

```bash
go generate ./internal/protogen
```

Create `internal/protogen/contract_test.go`:

```go
package protogen_test

import (
    "testing"

    repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
    "google.golang.org/protobuf/proto"
)

func TestRequestContextRoundTrip(t *testing.T) {
    input := &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{
        Host: "github.com", Owner: "alphaexplorationco", Name: "clubhouse_infra",
    }}
    raw, err := proto.Marshal(input)
    if err != nil {
        t.Fatal(err)
    }
    output := new(repowolfv1.RequestContext)
    if err := proto.Unmarshal(raw, output); err != nil {
        t.Fatal(err)
    }
    if !proto.Equal(input, output) {
        t.Fatalf("round trip = %#v, want %#v", output, input)
    }
}
```

- [ ] **Step 4: Add minimal command dispatch and README usage**

Create `internal/buildinfo/version.go`:

```go
package buildinfo

var Version = "dev"
```

Implement `cmd/repowolf/main.go` so `repowolf --version` prints `buildinfo.Version`, unknown commands print `usage: repowolf <serve|config|token|cert>`, and exit status is 2. Implement `cmd/repowolf-client/main.go` so it detects `filepath.Base(os.Args[0])` and reports supported modes `gh`, `tea`, and `repowolf-git-ssh`; no network behavior is added yet.

Update `README.md` with the product boundary, the three client names, and a link to the approved spec.

- [ ] **Step 5: Verify the foundation**

Run:

```bash
go tool buf lint
scripts/check-generated.sh
gofmt -w cmd internal gen
go test ./...
go vet ./...
```

Expected: Buf passes, generation is reproducible, Go tests and vet pass. No static-file-content test is added; direct generation/lint/build verification is the Testing Value Gate result.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum buf.yaml buf.gen.yaml scripts proto gen internal cmd README.md
git commit -m "chore: establish RepoWolf Go foundation"
```

---

### Task 2: Implement strict YAML configuration and immutable policy types

**Files:**
- Create: `internal/config/types.go`
- Create: `internal/config/defaults.go`
- Create: `internal/config/decode.go`
- Create: `internal/config/validate.go`
- Create: `internal/config/config_test.go`
- Create: `internal/config/testdata/valid.yaml`
- Create: `internal/config/testdata/duplicate-key.yaml`
- Create: `internal/config/testdata/unknown-field.yaml`

**Interfaces:**
- Produces: `config.Decode(io.Reader) (config.Config, error)`.
- Produces: `config.LoadFile(path string) (config.Config, error)`.
- Produces: `config.Config.Validate() error` and `config.Config.Repository(id string) (config.Repository, bool)`.
- Produces: exact `config.Capability`, `config.Provider`, `config.Repository`, `config.Principal`, `config.Grant`, `config.PushPolicy`, and `config.Limits` types consumed by auth, policy, server, and provider tasks.

- [ ] **Step 1: Write failing strict-decoding and semantic tests**

Create table-driven tests covering:

```go
func TestDecodeRejectsDuplicateKey(t *testing.T) {
    _, err := config.Decode(strings.NewReader("apiVersion: repowolf.dev/v1alpha1\nlisten: :8443\nlisten: :9443\n"))
    if err == nil || !strings.Contains(err.Error(), "duplicate") {
        t.Fatalf("error = %v, want duplicate-key error", err)
    }
}

func TestDecodeRejectsUnknownField(t *testing.T) {
    _, err := config.Decode(strings.NewReader("apiVersion: repowolf.dev/v1alpha1\nlisten: :8443\nunknown: true\n"))
    if err == nil || !strings.Contains(err.Error(), "unknown") {
        t.Fatalf("error = %v, want unknown-field error", err)
    }
}

func TestValidateRejectsWildcardRepository(t *testing.T) {
    cfg := validConfig()
    repo := cfg.Repositories["clubhouse"]
    repo.Owner = "alpha*"
    cfg.Repositories["clubhouse"] = repo
    if err := cfg.Validate(); err == nil {
        t.Fatal("Validate accepted wildcard owner")
    }
}
```

Also cover unsupported `apiVersion`, undefined providers/repositories, duplicate capabilities, `git:write` without `git:read`, token environment names outside `^REPOWOLF_TOKEN_[A-Z0-9_]+$`, invalid absolute tool overrides, invalid provider SSH ports, invalid refs, zero limits, a `maxMessageBytes` value above the hard 1 MiB cap, and default `refs/heads/main` denial.

- [ ] **Step 2: Run the tests to verify RED**

```bash
go test ./internal/config
```

Expected: FAIL because `config.Decode`, types, and validation do not exist.

- [ ] **Step 3: Define focused configuration types and defaults**

Use these public shapes in `internal/config/types.go`:

```go
package config

import "time"

type Capability string

const (
    RepositoryRead   Capability = "repository:read"
    IssuesRead       Capability = "issues:read"
    IssuesWrite      Capability = "issues:write"
    PullRequestsRead Capability = "pull_requests:read"
    PullRequestsWrite Capability = "pull_requests:write"
    ActionsRead      Capability = "actions:read"
    StatusesRead     Capability = "statuses:read"
    GitRead          Capability = "git:read"
    GitWrite         Capability = "git:write"
)

type Config struct {
    APIVersion   string                `yaml:"apiVersion"`
    Listen       string                `yaml:"listen"`
    TLS          TLS                   `yaml:"tls"`
    Tools        Tools                 `yaml:"tools"`
    Providers    map[string]Provider   `yaml:"providers"`
    Repositories map[string]Repository `yaml:"repositories"`
    Principals   map[string]Principal  `yaml:"principals"`
    Limits       Limits                `yaml:"limits"`
}

type TLS struct {
    Certificate string `yaml:"certificate"`
    PrivateKey  string `yaml:"privateKey"`
}

type Tools struct {
    GH  *string `yaml:"gh"`
    Tea *string `yaml:"tea"`
    SSH *string `yaml:"ssh"`
}

type ProviderKind string

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
    TeaLogin string       `yaml:"teaLogin"`
}

type Repository struct {
    Provider string     `yaml:"provider"`
    Owner    string     `yaml:"owner"`
    Name     string     `yaml:"name"`
    Git      PushPolicy `yaml:"git"`
}

type Principal struct {
    TokenEnvs []string `yaml:"tokenEnvs"`
    Grants    []Grant  `yaml:"grants"`
}
type Grant struct {
    Repository   string       `yaml:"repository"`
    Capabilities []Capability `yaml:"capabilities"`
}
type PushPolicy struct {
    DenyRefs      []string `yaml:"denyRefs"`
    DenyDeletes   bool     `yaml:"denyDeletes"`
    MaxRefUpdates int      `yaml:"maxRefUpdates"`
}
type Limits struct {
    MaxConcurrentRequests             int           `yaml:"maxConcurrentRequests"`
    MaxConcurrentRequestsPerPrincipal int           `yaml:"maxConcurrentRequestsPerPrincipal"`
    MaxMessageBytes                   int           `yaml:"maxMessageBytes"`
    MaxStreamChunkBytes               int           `yaml:"maxStreamChunkBytes"`
    MaxPushPrefixBytes                int           `yaml:"maxPushPrefixBytes"`
    MaxGitBytesPerDirection           int64         `yaml:"maxGitBytesPerDirection"`
    InitialStreamTimeout              time.Duration `yaml:"-"`
    OperationTimeout                  time.Duration `yaml:"-"`
    IdleStreamTimeout                 time.Duration `yaml:"-"`
}
```

Use raw duration strings in a private decode struct and normalize them into `time.Duration`. Provider `SSHPort` defaults to 22. Resource defaults are: 8 concurrent requests, 4 per principal, 1 MiB messages, 64 KiB chunks, 1 MiB push prefix, 8 GiB per Git direction, 5-second initial stream timeout, 10-minute unary timeout, and 2-minute idle stream timeout.

- [ ] **Step 4: Implement strict decoding and validation**

Use `yaml.Decoder.KnownFields(true)`, reject a second YAML document, and walk the `yaml.Node` mapping tree before struct decoding to reject duplicate keys at every depth. Apply defaults before overlaying user values; deep-copy default slices.

Validation must canonicalize no paths and mutate no caller-owned input. It validates syntax and references only. Keep validators in `validate.go` split by provider, repository, principal, push policy, and limits.

- [ ] **Step 5: Run focused and race tests**

```bash
gofmt -w internal/config
go test -race ./internal/config
go test ./...
```

Expected: all config cases pass, including duplicate/unknown rejection and exact-main defaulting.

- [ ] **Step 6: Commit**

```bash
git add internal/config
git commit -m "feat(config): add strict policy loading"
```

---

### Task 3: Add opaque token generation and environment authentication

**Files:**
- Create: `internal/auth/token.go`
- Create: `internal/auth/index.go`
- Create: `internal/auth/context.go`
- Create: `internal/auth/token_test.go`
- Create: `internal/auth/index_test.go`
- Create: `internal/cli/token.go`
- Modify: `cmd/repowolf/main.go`

**Interfaces:**
- Consumes: `config.Config.Principals` and each principal's `TokenEnvs`.
- Produces: `auth.Generate(io.Reader) (string, error)`.
- Produces: `auth.Load(map[string]config.Principal, auth.LookupEnv) (*auth.Index, error)`.
- Produces: `(*auth.Index).Authenticate(token string) (principal string, ok bool)`.
- Produces: `auth.WithPrincipal(context.Context, string)` and `auth.Principal(context.Context) (string, bool)`.

- [ ] **Step 1: Write failing generation, loading, duplicate, and disclosure tests**

Use fixed 32-byte entropy to assert the `rw1_` prefix and URL-safe encoding. Test missing, empty, malformed, and duplicate environment token values. Test that returned errors contain environment variable names but never raw token values.

```go
func TestLoadDoesNotDiscloseMalformedToken(t *testing.T) {
    secret := "not-a-valid-token"
    principals := map[string]config.Principal{
        "agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}},
    }
    _, err := auth.Load(principals, func(name string) (string, bool) {
        return secret, true
    })
    if err == nil || strings.Contains(err.Error(), secret) {
        t.Fatalf("unsafe error: %v", err)
    }
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/auth
```

Expected: FAIL because auth package does not exist.

- [ ] **Step 3: Implement token generation and constant-time digest matching**

`Generate` reads exactly 32 random bytes and returns `rw1_` plus unpadded base64url. `Load` accepts only that format, computes SHA-256 digests, rejects duplicate token values across principals, and stores only `(digest, principal)` entries. `Authenticate` parses before hashing, compares the candidate against every stored digest with `subtle.ConstantTimeCompare` without early return, and returns a principal only when exactly one entry matches.

Implement the CLI handler:

```go
func RunTokenGenerate(stdout io.Writer, random io.Reader) error {
    token, err := auth.Generate(random)
    if err != nil { return err }
    _, err = fmt.Fprintln(stdout, token)
    return err
}
```

No command writes YAML or environment files.

- [ ] **Step 4: Run focused tests and leak assertions**

```bash
gofmt -w internal/auth internal/cli cmd/repowolf
go test -race ./internal/auth ./internal/cli
go test ./...
```

Expected: all auth tests pass and no failure string includes test token bytes.

- [ ] **Step 5: Commit**

```bash
git add internal/auth internal/cli/token.go cmd/repowolf/main.go
git commit -m "feat(auth): add environment token authentication"
```

---

### Task 4: Implement TLS loading and local certificate bootstrap

**Files:**
- Create: `internal/tlsconfig/load.go`
- Create: `internal/tlsconfig/init.go`
- Create: `internal/tlsconfig/load_test.go`
- Create: `internal/tlsconfig/init_test.go`
- Create: `internal/cli/cert.go`
- Modify: `cmd/repowolf/main.go`

**Interfaces:**
- Produces: `tlsconfig.LoadServer(certPath, keyPath string) (*tls.Config, error)`.
- Produces: `tlsconfig.Init(tlsconfig.InitOptions) (tlsconfig.Files, error)`.
- `InitOptions` contains `OutputDir string`, `DNSNames []string`, `IPAddresses []net.IP`, `Now func() time.Time`, and `Random io.Reader`.

- [ ] **Step 1: Write failing TLS and certificate tests**

Cover TLS 1.3 minimum, wrong key rejection, CA verification, DNS/IP SAN verification, restrictive private-key modes, atomic no-overwrite behavior, random serial numbers, CA `IsCA`, and server `ExtKeyUsageServerAuth`.

```go
func TestInitRefusesOverwrite(t *testing.T) {
    dir := t.TempDir()
    if _, err := tlsconfig.Init(testOptions(dir)); err != nil { t.Fatal(err) }
    if _, err := tlsconfig.Init(testOptions(dir)); !errors.Is(err, fs.ErrExist) {
        t.Fatalf("second Init error = %v, want fs.ErrExist", err)
    }
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/tlsconfig
```

Expected: FAIL because TLS package does not exist.

- [ ] **Step 3: Implement PEM loading and certificate generation**

Use Ed25519 keys, random 128-bit serial numbers, a 5-year CA certificate, and a 397-day server certificate. Write private keys with mode `0600` and certificates with `0644` through create-exclusive temporary files followed by atomic rename. Remove partial files on failure.

`LoadServer` uses `tls.LoadX509KeyPair` and returns:

```go
&tls.Config{
    MinVersion:   tls.VersionTLS13,
    Certificates: []tls.Certificate{certificate},
}
```

Client-side CA loading is deliberately implemented later in `internal/clientconfig`; the credential-free client does not import the service certificate-bootstrap package.

- [ ] **Step 4: Add `repowolf cert init` parsing**

Support exact flags:

```text
--output <directory>          required
--dns <name>                 repeatable
--ip <address>               repeatable
```

Require at least one DNS or IP SAN. Print only generated public paths and private-key paths; never print private key bytes.

- [ ] **Step 5: Verify**

```bash
gofmt -w internal/tlsconfig internal/cli cmd/repowolf
go test -race ./internal/tlsconfig ./internal/cli
go test ./...
```

Expected: TLS tests pass without installing anything into a host trust store.

- [ ] **Step 6: Commit**

```bash
git add internal/tlsconfig internal/cli/cert.go cmd/repowolf/main.go
git commit -m "feat(tls): add server certificate bootstrap"
```

---

### Task 5: Build exact-repository authorization and push policy

**Files:**
- Create: `internal/policy/snapshot.go`
- Create: `internal/policy/authorize.go`
- Create: `internal/policy/push.go`
- Create: `internal/policy/authorize_test.go`
- Create: `internal/policy/push_test.go`

**Interfaces:**
- Consumes: validated `config.Config`.
- Produces: `policy.New(config.Config) (*policy.Snapshot, error)`.
- Produces: `(*policy.Snapshot).Resolve(principal string, selector policy.Selector, capability config.Capability) (policy.ResolvedRepository, error)`.
- `policy.Selector` contains optional `Kind config.ProviderKind` plus exact `Host`, `SSHPort`, `Owner`, and `Name`; every nonzero/nonempty selector field must match configured policy.
- `policy.ResolvedRepository` contains `ID string`, `Repository config.Repository`, and its referenced `Provider config.Provider` so downstream adapters never perform a second untrusted lookup.
- Produces: `policy.ValidatePush(config.PushPolicy, []policy.Update) error`.
- `policy.Update` contains `OldOID`, `NewOID`, and `Ref` strings.
- Exposes sentinels `policy.ErrDenied`, `policy.ErrRepository`, and `policy.ErrRefPolicy` without revealing whether an unauthorized repository is configured.

- [ ] **Step 1: Write failing authorization matrix tests**

Cover exact grants, multi-repository principals, missing capabilities, unknown principal, unknown/unauthorized repository anti-enumeration, `git:write` policy, deletes, max update count, malformed refs, SHA-1/SHA-256 zero OIDs, and default main denial.

```go
func TestUnauthorizedAndUnknownRepositoryAreIndistinguishable(t *testing.T) {
    snapshot := testSnapshot(t)
    _, unknown := snapshot.Resolve("infra-agent", policy.Selector{
        Kind: config.ProviderGitHub, Host: "github.com", Owner: "none", Name: "missing",
    }, config.RepositoryRead)
    _, unauthorized := snapshot.Resolve("infra-agent", policy.Selector{
        Kind: config.ProviderGitHub, Host: "github.com", Owner: "other", Name: "private",
    }, config.RepositoryRead)
    if !errors.Is(unknown, policy.ErrDenied) || !errors.Is(unauthorized, policy.ErrDenied) {
        t.Fatalf("unknown=%v unauthorized=%v", unknown, unauthorized)
    }
    if unknown.Error() != unauthorized.Error() {
        t.Fatalf("errors enumerate policy: %q != %q", unknown, unauthorized)
    }
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/policy
```

Expected: FAIL because policy package does not exist.

- [ ] **Step 3: Implement immutable grant indexes and push validation**

Define `ResolvedRepository` with the exact fields in Interfaces. Deep-copy repository/provider/policy slices into `Snapshot`. Build `map[principal]map[repository]map[Capability]struct{}` once. `Resolve` searches only that principal's granted repositories, matches every nonzero/nonempty selector field against the referenced provider/repository, rejects ambiguous matches, returns one copied aggregate, and uses generic `ErrDenied` for unknown, unauthorized, and ambiguous lookups.

Port the existing syntactic push-policy behavior from:

```text
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/policy/push.go
```

Adapt it to the new config types and preserve its tests before adding SHA-256 and anti-enumeration cases.

- [ ] **Step 4: Verify**

```bash
gofmt -w internal/policy
go test -race ./internal/policy
go test ./...
```

Expected: complete authorization and push matrices pass.

- [ ] **Step 5: Commit**

```bash
git add internal/policy
git commit -m "feat(policy): enforce exact repository grants"
```

---

### Task 6: Port and generalize the bounded provider process runner

**Files:**
- Create: `internal/runner/tools.go`
- Create: `internal/runner/environment.go`
- Create: `internal/runner/command.go`
- Create: `internal/runner/call.go`
- Create: `internal/runner/stream.go`
- Create: `internal/runner/process_unix.go`
- Create: `internal/runner/tools_test.go`
- Create: `internal/runner/environment_test.go`
- Create: `internal/runner/call_test.go`
- Create: `internal/runner/stream_test.go`

**Interfaces:**
- Produces: `runner.ResolveTools(config.Tools, runner.LookPath) (runner.Toolset, error)`.
- Produces: `runner.ProviderEnvironment(base []string, excluded []string) []string`.
- Produces: `(*runner.Runner).Call(context.Context, runner.Command) (runner.Result, error)`.
- Produces: `(*runner.Runner).Start(context.Context, runner.Command) (*runner.Process, error)` for Git streams.
- `runner.Command` contains only pinned `Path`, generated `Args`, bounded `Stdin`, exact `Env`, `Timeout`, `StdoutLimit`, and `StderrLimit`.

- [ ] **Step 1: Port existing lifecycle tests before implementation**

Copy and adapt behavior tests from:

```text
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/runner/runner_test.go
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/runner/stream_test.go
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/runner/stream_lifecycle_test.go
```

Add tests proving canonical one-time tool resolution, no per-request `PATH` lookup, private cwd, output bounds, timeout, cancellation, process-group cleanup, and token/internal environment removal while provider auth remains byte-identical.

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/runner
```

Expected: FAIL because runner implementation is absent.

- [ ] **Step 3: Implement tool resolution and provider environment filtering**

`ResolveTools` resolves only configured provider kinds. For each tool, use an absolute override or `LookPath`, call `filepath.EvalSymlinks`, require a regular executable file, and store the canonical absolute path.

`ProviderEnvironment` parses environment keys at the first `=` and removes only exact excluded names plus internal names beginning `REPOWOLF_`. Preserve ordering and bytes for every other entry; do not inspect provider-auth values.

- [ ] **Step 4: Implement bounded calls and streams**

Port the reusable process authority and bounded I/O logic from the current broker runner. Replace GitHub-specific call types with `runner.Command`/`runner.Result`. On Linux and macOS use a private process group, and permit negative-PGID signaling only from the direct parent runner authority. Create one private temporary cwd per call and remove it after reaping.

`Result` contains `Stdout []byte`, `ExitCode int`, `TimedOut bool`, and safe byte counts. Raw stderr is bounded for internal classification and discarded before returning to callers.

- [ ] **Step 5: Verify race and cleanup behavior**

```bash
gofmt -w internal/runner
go test -race ./internal/runner
go test ./...
```

Expected: no leaked children or temporary directories; cancellation and output-limit cases pass.

- [ ] **Step 6: Commit**

```bash
git add internal/runner
git commit -m "feat(runner): add bounded provider execution"
```

---

### Task 7: Assemble authenticated gRPC service lifecycle, health, errors, and audit

**Files:**
- Create: `proto/repowolf/v1/meta.proto`
- Create: `gen/repowolf/v1/meta.pb.go`
- Create: `internal/auth/interceptor.go`
- Create: `internal/auth/interceptor_test.go`
- Create: `internal/audit/event.go`
- Create: `internal/audit/writer.go`
- Create: `internal/audit/writer_test.go`
- Create: `internal/rpcstatus/status.go`
- Create: `internal/rpcstatus/status_test.go`
- Create: `internal/server/options.go`
- Create: `internal/server/server.go`
- Create: `internal/server/lifecycle.go`
- Create: `internal/server/server_test.go`
- Create: `internal/app/runtime.go`
- Create: `internal/app/serve.go`
- Create: `internal/cli/config.go`
- Modify: `cmd/repowolf/main.go`

**Interfaces:**
- Consumes: config, auth index, TLS config, policy snapshot, toolset, and audit writer.
- Produces: `auth.UnaryServerInterceptor` and `auth.StreamServerInterceptor`.
- Produces: `audit.Writer.Write(audit.Event) error` with serialized JSONL writes.
- Produces: `server.New(server.Options) (*server.Server, error)` and `(*server.Server).Serve(context.Context, net.Listener) error`.
- Produces: standard TLS-protected unauthenticated gRPC health service.
- Produces: runtime context accessors for authenticated principal and request ID.

- [ ] **Step 1: Write failing interceptor, audit, status, and lifecycle tests**

Test missing/malformed/invalid bearer metadata, unary/stream principal context, unauthenticated health access, 1 MiB options, global/per-principal concurrency, stable anti-enumeration statuses, JSONL serialization under concurrency, safe field allowlist, graceful stop, forced cancellation, and readiness transitions.

```go
func TestAuditNeverSerializesSensitiveFields(t *testing.T) {
    event := audit.Event{Principal: "agent", Operation: "issue.create", Outcome: "completed"}
    var output bytes.Buffer
    if err := audit.NewWriter(&output).Write(event); err != nil { t.Fatal(err) }
    for _, forbidden := range []string{"token", "body", "argv", "stderr", "environment"} {
        if strings.Contains(strings.ToLower(output.String()), forbidden) {
            t.Fatalf("audit contains forbidden field %q: %s", forbidden, output.String())
        }
    }
}
```

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/auth ./internal/audit ./internal/rpcstatus ./internal/server ./internal/app
```

Expected: FAIL because service infrastructure is absent.

- [ ] **Step 3: Add request metadata and regenerate Protobuf**

Define only safe response metadata in `meta.proto`:

```proto
syntax = "proto3";
package repowolf.v1;
option go_package = "github.com/rochecompaan/repowolf/gen/repowolf/v1;repowolfv1";

message ResponseMeta {
  string request_id = 1;
}
```

Regenerate and verify a clean second generation.

- [ ] **Step 4: Implement interceptors, status mapping, and audit**

Bearer parsing accepts exactly one `authorization` value with exact `Bearer ` prefix. Health service methods bypass bearer auth but remain behind TLS. All other calls attach principal and a random request ID to context.

Map domain sentinels to `Unauthenticated`, `PermissionDenied`, `InvalidArgument`, `Unimplemented`, `Unavailable`, `DeadlineExceeded`, and `ResourceExhausted`. Never include policy existence or raw provider errors.

Audit writes one JSON object per line to stdout through a mutex-protected encoder and reports sink failure to the server; accepted operations must not silently lose their terminal event. Sanitized operational diagnostics use stderr so audit stdout remains machine-readable JSONL.

- [ ] **Step 5: Implement runtime assembly and graceful lifecycle**

`internal/app/runtime.go` performs this startup order:

```text
Decode/validate YAML → load token envs → load TLS → resolve/pin tools →
build policy snapshot → build provider env → create server → mark ready
```

`repowolf config validate --config <path>` performs structural/reference validation only. `repowolf serve --config <path>` performs full runtime validation and binds only after success.

Use `grpc.MaxRecvMsgSize(1_048_576)`, `grpc.MaxSendMsgSize(1_048_576)`, keepalive limits, and chained auth/concurrency/audit interceptors. Graceful shutdown uses a bounded grace timer then cancels active work and invokes runner cleanup.

- [ ] **Step 6: Verify**

```bash
scripts/check-generated.sh
gofmt -w internal cmd gen
go test -race ./internal/auth ./internal/audit ./internal/rpcstatus ./internal/server ./internal/app
go test ./...
go vet ./...
```

Expected: auth, health, lifecycle, audit, and status tests pass.

- [ ] **Step 7: Commit**

```bash
git add proto gen internal cmd/repowolf/main.go
git commit -m "feat(server): add authenticated gRPC lifecycle"
```

---

### Task 8: Port GitHub typed operations behind the gRPC service

**Files:**
- Create: `proto/repowolf/v1/github.proto`
- Create: `gen/repowolf/v1/github.pb.go`
- Create: `gen/repowolf/v1/github_grpc.pb.go`
- Create: `internal/provider/github/operation.go`
- Create: `internal/provider/github/validate.go`
- Create: `internal/provider/github/command.go`
- Create: `internal/provider/github/normalize.go`
- Create: `internal/provider/github/adapter.go`
- Create: `internal/provider/github/adapter_test.go`
- Create: `internal/provider/github/inert_test.go`
- Create: `internal/server/github.go`
- Create: `internal/server/github_test.go`

**Interfaces:**
- Produces: gRPC `GitHubService.Execute(GitHubRequest) returns (GitHubResponse)`.
- Produces: `github.Adapter.Execute(context.Context, policy.ResolvedRepository, *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error)`.
- Consumes: pinned `gh`, provider record, runner, policy authorization, and typed Protobuf.

- [ ] **Step 1: Define the complete GitHub oneof contract**

`GitHubRequest` contains `RequestContext` plus one operation from this exact set:

```text
repository_view
issue_list, issue_view, issue_create, issue_edit, issue_comment, issue_close, issue_reopen
pull_list, pull_view, pull_create, pull_edit, pull_comment, pull_close, pull_reopen, pull_ready, pull_checks
run_list, run_view
status_view
```

Define operation-specific messages with typed `uint64` numbers/limits, enum states, strings, repeated labels/assignees, and explicit presence via `optional` fields. `GitHubResponse` mirrors the oneof with normalized result records and `ResponseMeta`. Do not add raw JSON or arbitrary key/value fields.

Use this exact capability map:

```text
repository_view                                      → repository:read
issue_list, issue_view                               → issues:read
issue_create/edit/comment/close/reopen               → issues:write
pull_list, pull_view                                 → pull_requests:read
pull_create/edit/comment/close/reopen/pull_ready     → pull_requests:write
pull_checks, status_view                             → statuses:read
run_list, run_view                                   → actions:read
```

- [ ] **Step 2: Port existing GitHub behavior tests and verify RED**

Port relevant tests from:

```text
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/github/
```

Preserve boundary, inert-data, pagination, checks/status, output normalization, body-size, and operation tests. Replace custom JSON protocol fixtures with typed Protobuf fixtures and fake-runner expectations.

Run:

```bash
go generate ./internal/protogen
go test ./internal/provider/github ./internal/server
```

Expected: FAIL because adapter/service implementations are absent.

- [ ] **Step 3: Port validators and normalization into focused modules**

Port pure validation/normalization behavior from current `internal/github` files, changing imports to RepoWolf config/Protobuf types. Keep request validation separate from command generation and output parsing. Reject empty/oversized titles/bodies, invalid states, zero numbers, unsupported combinations, and unknown oneof variants.

- [ ] **Step 4: Implement generated `gh` command plans**

Each operation returns a `runner.Command` with fixed argument boundaries. Always generate exact `--repo owner/name`, `--hostname <apiHost>` where needed, noninteractive flags, and JSON fields selected by the service. Use `--body-file -` or bounded stdin where supported. A free-form value may occupy only the next value position of a fixed generated flag.

No adapter method accepts client argv, environment names, URLs, headers, API paths, or executable paths.

- [ ] **Step 5: Implement adapter and gRPC service authorization**

`server.githubService.Execute` determines the capability from the oneof before provider execution, authorizes the authenticated principal and repository ID, confirms the provider kind is GitHub, and delegates to the adapter. Accepted, denied, completed, cancelled, and failed outcomes write audit events.

- [ ] **Step 6: Verify GitHub behavior and inertness**

```bash
scripts/check-generated.sh
gofmt -w internal/provider/github internal/server gen
go test -race ./internal/provider/github ./internal/server
go test ./...
```

Expected: all ported current-broker GitHub tests pass against typed requests; injected leading dashes/newlines remain single inert values.

- [ ] **Step 7: Commit**

```bash
git add proto/repowolf/v1/github.proto gen/repowolf/v1 internal/provider/github internal/server
git commit -m "feat(github): add typed provider operations"
```

---

### Task 9: Build the restricted `gh` compatibility client

**Files:**
- Create: `internal/clientconfig/config.go`
- Create: `internal/clientconfig/tls.go`
- Create: `internal/clientconfig/dial.go`
- Create: `internal/clientconfig/dial_test.go`
- Create: `internal/client/repository.go`
- Create: `internal/client/github/flags.go`
- Create: `internal/client/github/parse.go`
- Create: `internal/client/github/issue.go`
- Create: `internal/client/github/pull.go`
- Create: `internal/client/github/run.go`
- Create: `internal/client/github/render.go`
- Create: `internal/client/github/client.go`
- Create: `internal/client/github/parse_test.go`
- Create: `internal/client/github/client_test.go`
- Modify: `cmd/repowolf-client/main.go`

**Interfaces:**
- Produces: `clientconfig.LoadEnv() (clientconfig.Config, error)` using `REPOWOLF_ENDPOINT`, `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, and optional `REPOWOLF_SERVER_NAME`.
- Produces: `clientconfig.Dial(context.Context, Config) (*grpc.ClientConn, error)`.
- Produces: `github.Parse(args []string, cwd string) (*repowolfv1.GitHubRequest, error)`.
- Produces: `github.Run(context.Context, []string, io.Writer, io.Writer) int`.

- [ ] **Step 1: Port the current `gh` parser tests and verify RED**

Port parser/format behavior from current files:

```text
internal/client/flags.go
internal/client/format.go
internal/client/gh_issue.go
internal/client/gh_parse.go
internal/client/gh_pull.go
internal/client/gh_run.go
internal/client/repository.go
```

Keep the exact supported command surface from Task 8. Add explicit rejection tests for `gh api`, `repo list`, aliases, extensions, merge, workflow writes, unknown flags, multiple repository selectors, and prompts.

- [ ] **Step 2: Implement endpoint/TLS/token configuration**

Require an `https://` endpoint and nonempty `REPOWOLF_TOKEN` in correct format. When `REPOWOLF_CA_FILE` is set, require a readable file containing public CA material and build a new `x509.CertPool` from it; when it is unset, use `x509.SystemCertPool()` so platform-installed trust roots remain supported. Set the endpoint-derived or explicit server name and never enable insecure verification. `Dial` uses that client-only TLS config, per-RPC bearer credentials that require transport security, blocking dial with a bounded timeout, and 1 MiB send/receive call options.

- [ ] **Step 3: Implement repository hint parsing**

Explicit `--repo owner/name` wins. Otherwise inspect local Git remote only to form a `RepositorySelector{Host, Owner, Name}` request hint. Never translate it into authority or a policy ID on the client. Missing owner/name produces a client error instructing use of `--repo`; the service resolves the selector only within the authenticated principal's grants and returns a generic denial for unknown, unauthorized, or ambiguous matches.

- [ ] **Step 4: Implement native-compatible parsing and rendering**

Build typed oneof requests only. Render normalized responses into stable table/text output and preserve `--json` only for an explicit allowlist of typed fields. Do not pass jq/templates through to the service in MVP.

Client errors print concise diagnostics without endpoint policy details and return shell status 1; usage errors return 2; signal termination uses conventional status.

- [ ] **Step 5: Run a real TLS in-process client test**

Use `bufconn` for unit RPC behavior and a loopback TLS listener for CA/server-name verification. Assert that invalid CA, missing token, unsupported command, and denied repository fail before or at the expected boundary.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/clientconfig internal/client/github cmd/repowolf-client
go test -race ./internal/clientconfig ./internal/client/github
go test ./...
git add internal/clientconfig internal/client cmd/repowolf-client/main.go
git commit -m "feat(client): add restricted gh compatibility"
```

---

### Task 10: Implement Gitea typed operations with installed `tea`

**Files:**
- Create: `proto/repowolf/v1/gitea.proto`
- Create: `gen/repowolf/v1/gitea.pb.go`
- Create: `gen/repowolf/v1/gitea_grpc.pb.go`
- Create: `internal/provider/gitea/operation.go`
- Create: `internal/provider/gitea/validate.go`
- Create: `internal/provider/gitea/command.go`
- Create: `internal/provider/gitea/normalize.go`
- Create: `internal/provider/gitea/adapter.go`
- Create: `internal/provider/gitea/adapter_test.go`
- Create: `internal/provider/gitea/inert_test.go`
- Create: `internal/server/gitea.go`
- Create: `internal/server/gitea_test.go`

**Interfaces:**
- Produces: gRPC `GiteaService.Execute(GiteaRequest) returns (GiteaResponse)`.
- Produces: `gitea.Adapter.Execute(context.Context, policy.ResolvedRepository, *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error)`.
- Uses installed `tea` with the provider's fixed `TeaLogin`, exact `owner/name`, and JSON output.

- [ ] **Step 1: Define the complete Gitea oneof contract**

Use provider-specific messages for:

```text
repository_view
issue_list, issue_view, issue_create, issue_edit, issue_comment, issue_close, issue_reopen
pull_list, pull_view, pull_create, pull_edit, pull_comment, pull_close, pull_reopen, pull_checks
run_list, run_view
```

Do not define merge, approve/reject, checkout, clean, review mutation, action cancel/delete/log mutation, arbitrary API, admin, login, repository list/search/create/edit/delete, or browser operations.

Use this exact capability map:

```text
repository_view                          → repository:read
issue_list, issue_view                   → issues:read
issue_create/edit/comment/close/reopen   → issues:write
pull_list, pull_view                     → pull_requests:read
pull_create/edit/comment/close/reopen    → pull_requests:write
pull_checks                              → statuses:read
run_list, run_view                       → actions:read
```

- [ ] **Step 2: Write fake-`tea` tests and verify RED**

Use a fake executable that records NUL-separated argv and emits fixed JSON. Assert exact generated commands for `tea` 0.14.0 semantics, including fixed `--login`, `--repo owner/name`, `--output json`, issue/pull fields, and action run list/view. Assert unsupported provider operations return `Unimplemented` without starting `tea`.

- [ ] **Step 3: Implement validation, commands, and normalization**

Keep Gitea validation independent from GitHub even when fields resemble each other. Every command uses an explicit subcommand and output field allowlist. Comments use the fixed `tea comment` form. Close/reopen use only those exact subcommands. Pull checks are derived from typed `ci` fields in pull output; no arbitrary action endpoint is available.

- [ ] **Step 4: Implement authorization and audit service**

Map operations to shared capabilities, authorize principal/repository, require provider kind `gitea`, and emit the same safe audit lifecycle as GitHub.

- [ ] **Step 5: Verify and commit**

```bash
scripts/check-generated.sh
gofmt -w internal/provider/gitea internal/server gen
go test -race ./internal/provider/gitea ./internal/server
go test ./...
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1 internal/provider/gitea internal/server
git commit -m "feat(gitea): add typed provider operations"
```

---

### Task 11: Build the restricted `tea` compatibility client

**Files:**
- Create: `internal/client/gitea/flags.go`
- Create: `internal/client/gitea/parse.go`
- Create: `internal/client/gitea/issue.go`
- Create: `internal/client/gitea/pull.go`
- Create: `internal/client/gitea/action.go`
- Create: `internal/client/gitea/render.go`
- Create: `internal/client/gitea/client.go`
- Create: `internal/client/gitea/parse_test.go`
- Create: `internal/client/gitea/client_test.go`
- Modify: `cmd/repowolf-client/main.go`

**Interfaces:**
- Consumes: shared `clientconfig.Config`/`Dial` and Gitea generated client.
- Produces: `gitea.Parse(args []string, cwd string) (*repowolfv1.GiteaRequest, error)`.
- Produces: `gitea.Run(context.Context, []string, io.Writer, io.Writer) int`.

- [ ] **Step 1: Write native-syntax parser tests and verify RED**

Cover `tea repos owner/name`, issue list/view/create/edit/comment/close/reopen, pull list/view/create/edit/comment/close/reopen, and action run list/view. Cover native aliases only where they map unambiguously to an approved operation.

Reject login/logout, API, admin, merge, approve/reject, review mutation, checkout, clean, repo list/search/create/edit/delete, action cancel/delete, workflow/secret/variable changes, debug, prompts, unknown output formats, and arbitrary fields.

- [ ] **Step 2: Implement parser and typed field allowlists**

Require explicit `owner/name` or derive an untrusted `RepositorySelector` from local Git metadata. Never pass a `tea` login/profile from the client. A remote-derived host is only a selector field checked against the authenticated principal's exact grants; it cannot override the configured provider host.

- [ ] **Step 3: Implement rendering and gRPC execution**

Use the shared TLS/token dialer, add bearer credentials only through per-RPC metadata, and render typed Gitea results. Support table/text and a typed JSON field allowlist; no templates or arbitrary jq.

- [ ] **Step 4: Verify and commit**

```bash
gofmt -w internal/client/gitea cmd/repowolf-client
go test -race ./internal/client/gitea
go test ./...
git add internal/client/gitea cmd/repowolf-client/main.go
git commit -m "feat(client): add restricted tea compatibility"
```

---

### Task 12: Extract and preserve the hardened Git protocol parser

**Files:**
- Create: `internal/gitproto/advertised.go`
- Create: `internal/gitproto/capabilities.go`
- Create: `internal/gitproto/certificate.go`
- Create: `internal/gitproto/certificate_validate.go`
- Create: `internal/gitproto/pktline.go`
- Create: `internal/gitproto/receive.go`
- Create: `internal/gitproto/signature.go`
- Create: `internal/gitproto/validate.go`
- Create: corresponding `internal/gitproto/*_test.go` and fuzz tests

**Interfaces:**
- Produces: `gitproto.ParseAdvertisement(io.Reader, int) (gitproto.Advertisement, error)`.
- Produces: `gitproto.ParseReceivePack(io.Reader, gitproto.ReceiveOptions) (gitproto.ReceiveResult, error)`.
- Produces: `gitproto.ValidateRequestedCapabilities(requested, advertised gitproto.Capabilities) error`.
- `ReceiveResult` contains exact buffered `Prefix []byte`, parsed `Updates []policy.Update`, and requested capabilities.

- [ ] **Step 1: Copy the current parser and tests without behavior changes**

Copy all handwritten Go files and tests from:

```text
/home/roche/projects/pi/roche-pi/packages/jailed-github-broker/internal/gitproto/
```

Change only module imports and package dependencies required to compile. Preserve certificate, SHA-1/SHA-256, shallow, capability, pkt-line limit, delete, grammar, and v2.50 regression tests.

- [ ] **Step 2: Run the ported suite before adapting APIs**

```bash
go test -race ./internal/gitproto
```

Expected: PASS with behavior identical to the embedded broker. If the mechanical port fails, fix the port before changing APIs.

- [ ] **Step 3: Adapt the public parser result to new policy updates**

Expose buffered prefix bytes and `[]policy.Update` without exposing parser internals. Keep the packet maximum `65520`, caller-supplied push-prefix cap, advertised/requested capability validation, and zero-copy bounds where already proven.

- [ ] **Step 4: Add fuzz targets**

Add:

```go
func FuzzParseReceivePack(f *testing.F) {
    for _, seed := range receiveSeeds() { f.Add(seed) }
    f.Fuzz(func(t *testing.T, raw []byte) {
        _, _ = gitproto.ParseReceivePack(bytes.NewReader(raw), gitproto.ReceiveOptions{
            MaxBytes: 1 << 20,
            MaxCommands: 16,
        })
    })
}
```

Add corresponding pkt-line, advertisement, and capability fuzzers. Seed them with valid SHA-1, SHA-256, shallow, signed-push, and malformed examples from the ported tests.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/gitproto
go test -race ./internal/gitproto
go test ./...
git add internal/gitproto
git commit -m "feat(git): extract hardened protocol parser"
```

---

### Task 13: Implement bounded Git upload-pack and receive-pack gRPC services

**Files:**
- Create: `proto/repowolf/v1/git.proto`
- Create: `gen/repowolf/v1/git.pb.go`
- Create: `gen/repowolf/v1/git_grpc.pb.go`
- Create: `internal/gitservice/chunk.go`
- Create: `internal/gitservice/upload.go`
- Create: `internal/gitservice/receive.go`
- Create: `internal/gitservice/copy.go`
- Create: `internal/gitservice/upload_test.go`
- Create: `internal/gitservice/receive_test.go`
- Create: `internal/gitservice/limits_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Produces: gRPC `GitService.UploadPack(stream GitFrame) returns (stream GitFrame)`.
- Produces: gRPC `GitService.ReceivePack(stream GitFrame) returns (stream GitFrame)`.
- First client frame is `GitOpen{repository}` with an exact host/owner/name selector; later frames are data; client sends an explicit half-close.
- Server frames contain data or a sanitized terminal status. Each data payload is at most 64 KiB.

- [ ] **Step 1: Define Git stream Protobuf and failing tests**

Use this frame shape:

```proto
message GitOpen { RepositorySelector repository = 1; }
message GitData { bytes data = 1; }
enum GitTerminalCategory {
  GIT_TERMINAL_CATEGORY_UNSPECIFIED = 0;
  GIT_TERMINAL_CATEGORY_COMPLETED = 1;
  GIT_TERMINAL_CATEGORY_PERMISSION_DENIED = 2;
  GIT_TERMINAL_CATEGORY_INVALID_REQUEST = 3;
  GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE = 4;
  GIT_TERMINAL_CATEGORY_DEADLINE_EXCEEDED = 5;
  GIT_TERMINAL_CATEGORY_LIMIT_EXCEEDED = 6;
  GIT_TERMINAL_CATEGORY_UNAVAILABLE = 7;
}
message GitTerminal {
  int32 exit_code = 1;
  GitTerminalCategory category = 2;
}
message GitFrame {
  oneof payload {
    GitOpen open = 1;
    GitData data = 2;
    GitTerminal terminal = 3;
  }
}
service GitService {
  rpc UploadPack(stream GitFrame) returns (stream GitFrame);
  rpc ReceivePack(stream GitFrame) returns (stream GitFrame);
}
```

Test missing/duplicate open, data-before-open, unauthorized repositories, provider mismatch, chunk/message/total/idle limits, cancellation, disconnect, process cleanup, and audit terminal events.

- [ ] **Step 2: Run tests to verify RED**

```bash
go generate ./internal/protogen
go test ./internal/gitservice
```

Expected: FAIL because Git service is absent.

- [ ] **Step 3: Implement upload-pack orchestration**

Authorize `git:read`, require the configured provider Git host/user/port, and generate exactly `ssh -T -p <configured-port> -- <configured-user>@<configured-host> "git-upload-pack '<validated-owner>/<validated-name>.git'"`. Owner/name grammar excludes shell metacharacters and the complete remote command is service-generated. Start the pinned SSH process through `runner.Start`. Copy client frames to stdin and stdout to 64 KiB server frames with independent byte counters, idle timers, backpressure, cancellation, fixed diagnostics, and complete reaping.

- [ ] **Step 4: Implement receive-pack prevalidation**

Authorize both `git:read` and `git:write`. Generate the same fixed SSH argv with remote service `git-receive-pack '<validated-owner>/<validated-name>.git'`, relay only the server advertisement to the client, then buffer client data into `gitproto.ParseReceivePack` up to `MaxPushPrefixBytes`. Validate requested capabilities and `policy.ValidatePush` before writing `ReceiveResult.Prefix` to SSH stdin.

On denial, write zero client update bytes, cancel/reap SSH, send a sanitized terminal category, and audit refs/update count plus `providerInputBytes: 0`. After acceptance, forward the exact validated prefix followed by remaining bounded frames in order.

- [ ] **Step 5: Port current stream lifecycle regressions**

Adapt reusable tests from current `internal/server/git_test.go`, `input_pump_test.go`, `stream_limit_integration_test.go`, and `write_timeout_test.go` to gRPC streams. Preserve disconnect, blocked child, stderr, timeout, and zero-byte denial regressions.

- [ ] **Step 6: Verify and commit**

```bash
scripts/check-generated.sh
gofmt -w internal/gitservice internal/server gen
go test -race ./internal/gitservice ./internal/server
go test ./...
git add proto/repowolf/v1/git.proto gen/repowolf/v1 internal/gitservice internal/server
git commit -m "feat(git): add bounded gRPC transport"
```

---

### Task 14: Implement the `repowolf-git-ssh` client shim

**Files:**
- Create: `internal/client/gitssh/parse.go`
- Create: `internal/client/gitssh/parse_test.go`
- Create: `internal/client/gitssh/relay.go`
- Create: `internal/client/gitssh/relay_test.go`
- Create: `internal/client/gitssh/client.go`
- Modify: `cmd/repowolf-client/main.go`

**Interfaces:**
- Produces: `gitssh.Parse(args []string) (gitssh.Request, error)`.
- `gitssh.Request` contains only `*repowolfv1.RepositorySelector{Host, SshPort, Owner, Name}` and `Operation` (`upload-pack` or `receive-pack`); the credential-free client never imports server policy/config packages.
- Produces: `gitssh.Run(context.Context, []string, io.Reader, io.Writer, io.Writer) int`.

- [ ] **Step 1: Port current SSH parser tests and add network cases**

Port behavior from current `internal/client/ssh.go` and `ssh_test.go`. Accept only the Git-generated SSH forms needed for `git-upload-pack` and `git-receive-pack`. Parse optional `-p` as an untrusted numeric selector (default 22); the service must match it to the configured provider `SSHPort` before execution. Reject arbitrary SSH flags, proxy commands, remote commands other than Git upload/receive-pack, non-`git` users, malformed hosts, shells, environment options, and malformed repository slugs. The parsed host/owner/name remains untrusted until service policy resolution.

- [ ] **Step 2: Run tests to verify RED**

```bash
go test ./internal/client/gitssh
```

Expected: FAIL because client shim does not exist.

- [ ] **Step 3: Implement bidirectional gRPC relay**

Use shared client TLS/token configuration. Send one open frame, then read stdin into 64 KiB data frames while concurrently copying server frames to stdout. Preserve ordering, half-close on stdin EOF, handle cancellation/signals, enforce local byte limits, and emit only the fixed diagnostic `repowolf git transport failed` on stderr.

The client never prints gRPC details, endpoint internals, repository policy, or provider stderr.

- [ ] **Step 4: Test against an in-process fake Git service**

Cover successful upload/receive echo, server denial before stdin forwarding, client disconnect, server disconnect, oversized frame, invalid terminal category, and exit-code mapping. Assert no goroutine remains blocked with `goleak`-style deterministic channel/timeouts without adding a new dependency.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -w internal/client/gitssh cmd/repowolf-client
go test -race ./internal/client/gitssh
go test ./...
git add internal/client/gitssh cmd/repowolf-client/main.go
git commit -m "feat(client): add Git SSH transport shim"
```

---

### Task 15: Add end-to-end TLS, fake-provider, Git, Gitea, and leak verification

**Files:**
- Create: `internal/testutil/cert.go`
- Create: `internal/testutil/fakeexec.go`
- Create: `internal/testutil/server.go`
- Create: `integration/forge_test.go`
- Create: `integration/git_test.go`
- Create: `integration/gitea_test.go`
- Create: `integration/leak_test.go`
- Create: `integration/testdata/policy.yaml`
- Create: `integration/testdata/fake-provider.sh`
- Create: `integration/testdata/fake-ssh.sh`

**Interfaces:**
- Produces reusable test harness that starts a real loopback TLS gRPC service with environment token auth and fake pinned tools.
- Proves all three client modes work only through the public command binaries and network protocol.

- [ ] **Step 1: Write a failing black-box forge smoke test**

Build `repowolf` and `repowolf-client` into a temp directory, symlink client modes, start the server with generated TLS, and invoke:

```text
gh issue list --repo alpha/repo
tea issues list --repo alpha/repo
gh pr checks 7 --repo alpha/repo
tea actions runs list --repo alpha/repo
```

The fake provider records generated argv and returns typed fixture JSON. Assert successful native output and safe audit metadata.

- [ ] **Step 2: Write a failing offline Git proof**

Use real local Git plus `GIT_SSH_COMMAND=<temp>/repowolf-git-ssh` against the fake SSH executable. Prove upload-pack streams, allowed feature-branch receive-pack streams after prefix validation, and `refs/heads/main` denial sends zero client update bytes to fake SSH.

Never contact GitHub or Gitea in this test.

- [ ] **Step 3: Add disposable Gitea integration**

Start a pinned disposable Gitea container, configure a service-side `tea` login and SSH credentials through test Secrets, create one test repository through setup code outside RepoWolf, and execute the supported `tea` read/write surface through `repowolf-client`. Exclude merge/admin/API operations and tear down the container/repository.

Gate with an explicit integration build tag and run it in Linux CI where container support is present:

```bash
go test -tags=integration ./integration -run TestGitea
```

- [ ] **Step 4: Add leak and anti-enumeration scans**

Seed unique marker strings for token, provider credential, issue body, comment, pack payload, provider stderr, argv, and environment. Collect client stdout/stderr and audit JSONL. Assert each marker appears only in its intended fake provider input/output path and never in forbidden channels.

Compare unknown and unauthorized repository gRPC status/code/message byte-for-byte.

- [ ] **Step 5: Implement fixtures until tests pass**

Use only generated test certificates, fake binaries, local Git, and disposable Gitea. Tests must not depend on host `gh`/`tea` authentication or external network access except pulling the pinned Gitea image in the tagged job.

- [ ] **Step 6: Verify and commit**

```bash
gofmt -w internal/testutil integration
go test -race ./...
go test -tags=integration ./integration -run TestGitea
go vet ./...
git add internal/testutil integration
git commit -m "test: add RepoWolf end-to-end security coverage"
```

---

### Task 16: Package split binaries for Nix, OCI, and releases

**Files:**
- Create: `flake.nix`
- Create: `nix/package-server.nix`
- Create: `nix/package-client.nix`
- Create: `nix/checks.nix`
- Create: `nix/oci.nix`
- Create: `.goreleaser.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Modify: `README.md`

**Interfaces:**
- Produces: `.#repowolf` service/admin package.
- Produces: `.#repowolf-client` with `gh`, `tea`, and `repowolf-git-ssh` links and no host provider tools in its closure.
- Produces: OCI service image containing `repowolf`, `gh`, `tea`, OpenSSH, and CA roots.
- Produces: Linux/macOS amd64/arm64 release archives and checksums.

- [ ] **Step 1: Add split Nix packages**

Use `buildGoModule` twice with exact `subPackages`:

```nix
# server
subPackages = [ "cmd/repowolf" ];

# client
subPackages = [ "cmd/repowolf-client" ];
postInstall = ''
  ln -s repowolf-client $out/bin/gh
  ln -s repowolf-client $out/bin/tea
  ln -s repowolf-client $out/bin/repowolf-git-ssh
'';
```

Use the Nix fake-hash cycle once, replace `lib.fakeHash` with the reported `vendorHash`, and stage all referenced Nix files before normal flake checks.

Add a closure check that allows the client output's own `bin/gh` and `bin/tea` symlink names but fails if the closure includes the `pkgs.gh` or `pkgs.tea` provider packages, OpenSSH, service TLS keys, service config, or the server output.

- [ ] **Step 2: Add the OCI image**

Use `pkgs.dockerTools.buildLayeredImage` in `nix/oci.nix` with `name = "repowolf"`, `tag = "mvp"`, the RepoWolf service package, `pkgs.gh`, `pkgs.tea`, `pkgs.openssh`, `pkgs.cacert`, and minimal NSS files. Set `User = "65532:65532"`, `Entrypoint = [ "${repowolf}/bin/repowolf" ]`, `Cmd = [ "serve" ]`, `ExposedPorts."8443/tcp" = {}`, and `WorkingDir = "/tmp"`. Do not bake policy, tokens, provider credentials, or TLS keys into image layers. Export the OCI archive as flake package `ociImage`; consuming the published image does not require Nix.

- [ ] **Step 3: Add GoReleaser and CI**

Build `repowolf` and `repowolf-client` for Linux/macOS amd64/arm64 with version ldflags. CI runs:

```bash
go tool buf lint
scripts/check-generated.sh
gofmt -l .
go vet ./...
go test -race ./...
nix flake check --accept-flake-config --print-build-logs
```

Linux CI additionally runs tagged Gitea and Bubblewrap tests plus the OCI smoke. macOS CI runs unit/integration tests that do not require containers. Release workflow runs only on version tags, emits binary archive checksums, builds `.#ociImage`, and publishes it with `skopeo copy docker-archive:"$image" docker://ghcr.io/rochecompaan/repowolf:"$tag"` using the workflow's package token.

No tests assert workflow YAML, lockfile contents, dependency versions, or static Nix values; those receive direct syntax/build verification.

- [ ] **Step 4: Document installation and service configuration**

Add README sections for binary, Nix, and OCI installation; `repowolf token generate`; `repowolf cert init`; strict YAML; service token envs; client endpoint/token/CA variables; systemd/Home Manager supervision; and Kubernetes ConfigMap/Secret mounts.

- [ ] **Step 5: Verify and commit**

```bash
gofmt -l .
go tool buf lint
go test -race ./...
go vet ./...
nix flake check --accept-flake-config --print-build-logs
image="$(nix build .#ociImage --no-link --print-out-paths)"
docker load -i "$image"
docker run --rm repowolf:mvp --version
```

Expected: both Nix packages build, the client closure leak check passes, and the OCI image contains no configured secrets.

```bash
git add flake.nix nix .goreleaser.yaml .github README.md go.mod go.sum
git commit -m "build: package RepoWolf service and clients"
```

---

### Task 17: Prove Bubblewrap compatibility and prepare the separate migration handoff

**Files:**
- Create: `integration/bubblewrap_test.go`
- Create: `integration/testdata/jail-command.sh`
- Create: `nix/bubblewrap-check.nix`
- Create: `docs/migration/roche-pi.md`
- Create: `docs/verification/mvp.md`
- Modify: `nix/checks.nix`

**Interfaces:**
- Proves the Nix client closure can operate inside a real Bubblewrap jail using only endpoint, bearer token, trusted CA, repository mapping, and `GIT_SSH_COMMAND`.
- Produces an exact rollback-safe checklist for a later `roche-pi`/clubhouse migration plan; it does not modify those repositories.

- [ ] **Step 1: Write the failing real-jail test**

Construct a Bubblewrap environment with an empty home, clear environment, selected read-only Nix closures, writable fixture checkout, `/proc`, `/dev`, isolated `/tmp`, and shared network namespace. Inject only:

```text
REPOWOLF_ENDPOINT
REPOWOLF_TOKEN
REPOWOLF_CA_FILE
GIT_SSH_COMMAND
```

Run restricted `gh`, restricted `tea`, `git fetch`, and an offline denied-main push. Assert real host `gh`, `tea`, `ssh`, provider config, SSH keys, `SSH_AUTH_SOCK`, server binary, and service token environment names are unavailable inside.

- [ ] **Step 2: Run the test to verify RED**

```bash
go test -tags=integration ./integration -run TestBubblewrap
```

Expected: FAIL until the Nix client closure/check wiring is complete.

- [ ] **Step 3: Wire the Nix Bubblewrap check and make it pass**

Add the client package and its runtime closure only. Keep service/provider packages outside the jail. Preserve stdin/TTY behavior by explicitly forwarding stdin when launching asynchronous Bubblewrap processes. The test must leave no tracked checkout/ref mutation and no surviving process.

- [ ] **Step 4: Write the migration handoff**

`docs/migration/roche-pi.md` must name the later source files to change:

```text
/home/roche/projects/pi/roche-pi/nix/lib/mk-jailed-pi.nix
/home/roche/projects/pi/roche-pi/nix/lib/jailed-github-broker-lifecycle.nix
/home/roche/projects/pi/roche-pi/nix/packages/jailed-github-broker.nix
/home/roche/projects/pi/roche-pi/modules/checks/jailed-github-broker-real-jail.nix
/home/roche/projects/clubhouse/clubhouse_infra/devenv.nix
```

Document: deploy Home Manager service; generate/mount CA and role token; add RepoWolf client closure/env; run parity and leak suites; smoke authenticated read/fetch and approved non-main writes; keep current broker rollback; remove embedded broker only in a separate later change.

- [ ] **Step 5: Record final evidence**

Run the full release-candidate suite and record exact commands/results in `docs/verification/mvp.md`:

```bash
go tool buf lint
scripts/check-generated.sh
gofmt -l .
go vet ./...
go test -race ./...
go test -tags=integration ./integration
nix flake check --accept-flake-config --print-build-logs
nix build .#ociImage --no-link
```

Request a fresh adversarial review against `docs/specs/2026-08-01-repowolf-mvp-design.md`. Fix findings with focused tests and rerun the affected plus full suites before recording readiness.

- [ ] **Step 6: Commit**

```bash
git add integration nix docs/migration docs/verification
git commit -m "test: verify sandbox integration and migration readiness"
```

---

## Completion Gate

Before presenting branch-completion options:

- [ ] Every task commit is present and review findings are resolved.
- [ ] `git status --short` is empty.
- [ ] Generated Protobuf output is reproducible.
- [ ] `go tool buf lint`, `gofmt -l`, `go vet`, `go test -race ./...`, tagged integration tests, full Nix flake checks, and OCI smoke all pass from the final commit.
- [ ] Exact-main denial is proven offline with zero client update bytes forwarded.
- [ ] Client closure/image inspection proves no server tools, credentials, service configuration, or usable host paths are present.
- [ ] Audit/error leak scans pass with unique marker values.
- [ ] Linux and macOS release targets build.
- [ ] The current embedded broker remains untouched and available for rollback.
- [ ] Offer squash merge into `main` locally as the default integration option; do not offer a regular merge unless explicitly requested.
