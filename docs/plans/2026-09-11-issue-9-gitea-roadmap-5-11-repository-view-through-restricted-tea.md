# Restricted `tea` Repository View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent inspect one authorized, configured Gitea repository through a strict `tea` personality and the complete authenticated RepoWolf client-to-provider path.

**Architecture:** Add a provider-specific Gitea Protobuf service, client parser/renderer, SDK repository adapter, and provider-ID dispatcher. Extract only the shared unary provider lifecycle already proven by GitHub and Gitea—authorization, accepted audit, response metadata/limits, and terminal audit annotations—while retaining provider-specific validation and execution. Compose and register GitHub and Gitea independently, then prove the complete path against a digest-pinned Gitea 1.27.2 container.

**Tech Stack:** Go 1.26 (the issue-#8 baseline), Protocol Buffers/gRPC, `code.gitea.io/sdk/gitea` v0.25.1, Nix, Docker, and the existing RepoWolf integration harness.

**Spec:** `docs/specs/2026-09-11-issue-9-gitea-roadmap-5-11-repository-view-through-restricted-tea-design.md`

## Global Constraints

- Begin implementation from the merged issue-#8 secure-transport baseline (`internal/provider/gitea`, `internal/providerhttp`, `Provider.CAFile`, and one `*gitea.Client` per provider instance must be present). If this worktree still predates that merge, integrate the predecessor before Task 1; do not recreate or weaken it in this issue.
- Keep the protocol additive in package `repowolf.v1`; generated Go files change only through `scripts/generate.sh`.
- Support only `tea repos <owner>/<name> --repo <owner>/<name> [--output table|simple|json]` and the `repo` alias; require the two case-folded selectors to agree.
- Default output to `simple`; reject every unlisted command, positional value, flag, `--flag=value` spelling, inference path, prompt, editor, stdin, login, help, and version path.
- Keep argv bounded to 64 arguments and 64 KiB, valid UTF-8, and free of NUL bytes.
- Require exactly `repository:read`; preserve indistinguishable permission denial for unknown, ungranted, missing-capability, and wrong-provider-kind requests.
- Use the configured canonical owner/name for exactly one SDK `GetRepo` call; never accept provider URL, host, path, token, or SDK options from the request.
- Preserve issue-#8 HTTPS authority, CA, timeout, redirect, token, and 8 MiB HTTP response controls unchanged.
- Bound final Protobuf responses and fully prepared client output independently to 8 MiB.
- Audit only the existing bounded schema. Never audit selector text, repository content, URLs, topics, tokens, HTTP bodies, SDK errors, or rendered output.
- Keep GitHub and Git behavior byte-for-byte compatible; register GitHub and Gitea services independently and only when their complete executors exist.
- Do not add issue, pull-request, comment, pagination, generic forge, registry, raw API, or production upstream-`tea` behavior.
- No `AGENTS.md` exists in this worktree or its repository parents. Use `.github/workflows/ci.yml`, `scripts/check-generated.sh`, and `scripts/check-release.sh` as the validation authority.

## Testing Value Gate

The parser, renderer, adapter, service, lifecycle/audit, dispatch, and real-provider tests below all prove reusable production behavior or security-sensitive boundaries and can fail on meaningful regressions. Do not add tests that merely restate Protobuf text, Nix symlink declarations, image tags, or workflow YAML; verify those surfaces directly with generation checks, built-artifact invocation, image inspection, and CI execution.

## File Structure

### New files

- `proto/repowolf/v1/gitea.proto`: minimal typed Gitea repository-view service contract.
- `internal/client/gitea/parse.go`: bounded `tea` argv grammar and selector normalization.
- `internal/client/gitea/render.go`: deterministic simple/table/JSON response rendering.
- `internal/client/gitea/client.go`: configuration, TLS gRPC execution, diagnostics, and exit codes.
- `internal/client/gitea/parse_test.go`, `fuzz_test.go`, `render_test.go`, `client_test.go`: parser, renderer, and execution contracts.
- `internal/provider/gitea/operation.go`: request validation, capability, and canonical operation mapping.
- `internal/provider/gitea/adapter.go`: cancellable SDK repository operation and response normalization.
- `internal/provider/gitea/adapter_test.go`: fake-SDK operation, normalization, error, and cancellation proof.
- `internal/server/provider_lifecycle.go`: private shared unary provider lifecycle.
- `internal/server/provider_context.go`: bounded terminal-audit metadata carried in request context.
- `internal/server/provider_lifecycle_test.go`: GitHub/Gitea lifecycle and terminal audit regressions.
- `internal/server/gitea.go`, `gitea_test.go`, `gitea_response_limit_test.go`: Gitea service and limit behavior.
- `internal/app/gitea_executor.go`, `gitea_executor_test.go`: trusted provider-ID dispatch.
- `integration/gitea_repository_test.go`: real Gitea 1.27.2 full-path repository-view test.
- `integration/testdata/gitea-policy.yaml`: isolated real-Gitea broker policy fixture.

### Modified files

- `gen/repowolf/v1/gitea.pb.go`, `gitea_grpc.pb.go`: generated API output.
- `cmd/repowolf-client/main.go`, `main_test.go`: multicall `tea` personality.
- `internal/server/github.go`, `github_test.go`, `github_response_limit_test.go`, `audit.go`, `interceptor.go`, `options.go`, `server.go`, `server_test.go`: shared lifecycle adoption, terminal metadata, and conditional registrations.
- `internal/app/providers.go`, `providers_test.go`, `runtime.go`, `runtime_provider_test.go`: wrap issue-#8 SDK clients, dispatch by trusted provider ID, and compose Gitea service dependencies.
- `internal/rpcstatus/status.go`, `status_test.go`: stable sanitized provider-error mapping if a new Gitea domain sentinel is needed.
- `cmd/repowolf-client/main.go`, `cmd/repowolf-client/main_test.go`: personality routing and signal behavior.
- `nix/package-client.nix`, `nix/checks.nix`, `nix/bubblewrap-check.nix`: package and verify the `tea` alias without adding upstream `tea`.
- `examples/docker/sandbox/Dockerfile`, `examples/docker/README.md`, `scripts/ci/docker-example/assert-sandbox-boundary.sh`: expose and exercise the packaged alias in the sandbox artifact.
- `scripts/check-release.sh`: exercise `tea` in release-archive smoke validation.
- `internal/testutil/fakeexec.go`, `fakeexec_test.go`: build a `tea` test alias alongside existing multicall links.
- `.github/workflows/ci.yml`: run the opt-in real-Gitea integration gate; verify directly rather than testing YAML text.
- `integration/audit_assertions_test.go`, `integration/leak_test.go`: canonical terminal metadata and Gitea secret markers.

---

### Task 1: Add the minimal Gitea protocol

**Files:**
- Create: `proto/repowolf/v1/gitea.proto`
- Generate: `gen/repowolf/v1/gitea.pb.go`
- Generate: `gen/repowolf/v1/gitea_grpc.pb.go`

**Interfaces:**
- Produces: `GiteaService.Execute(context.Context, *GiteaRequest) (*GiteaResponse, error)`.
- Produces: `GiteaRequest.context`, one `repository_view` request branch, `GiteaResponse.meta`, one `repository_view` result branch, and `GiteaRepositoryRecord`.
- Uses field numbers `1` for context/meta and `10` for the first operation/result, matching the additive GitHub shape.

- [ ] **Step 1: Define the additive protocol**

Create `proto/repowolf/v1/gitea.proto` with the following exact message surface (standard `snake_case` JSON names follow from these field names):

```proto
syntax = "proto3";
package repowolf.v1;
option go_package = "github.com/rochecompaan/repowolf/gen/repowolf/v1;repowolfv1";

import "google/protobuf/timestamp.proto";
import "repowolf/v1/common.proto";
import "repowolf/v1/meta.proto";

service GiteaService {
  // buf:lint:ignore RPC_REQUEST_STANDARD_NAME
  // buf:lint:ignore RPC_RESPONSE_STANDARD_NAME
  rpc Execute(GiteaRequest) returns (GiteaResponse);
}

message GiteaRepositoryViewRequest {}
message GiteaRequest {
  RequestContext context = 1;
  oneof operation { GiteaRepositoryViewRequest repository_view = 10; }
}
message GiteaRepositoryRecord {
  string full_name = 1;
  string description = 2;
  string default_branch = 3;
  string url = 4;
  string ssh_url = 5;
  string clone_url = 6;
  bool private = 7;
  bool archived = 8;
  bool fork = 9;
  bool mirror = 10;
  bool empty = 11;
  uint64 stars = 12;
  uint64 forks = 13;
  uint64 open_issues = 14;
  uint64 size = 15;
  repeated string topics = 16;
  google.protobuf.Timestamp created = 17;
  google.protobuf.Timestamp updated = 18;
}
message GiteaRepositoryViewResult { GiteaRepositoryRecord repository = 1; }
message GiteaResponse {
  ResponseMeta meta = 1;
  oneof result { GiteaRepositoryViewResult repository_view = 10; }
}
```

- [ ] **Step 2: Generate and validate the contract directly**

Run:

```bash
scripts/generate.sh
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
git diff -- proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go
```

Expected: generation is stable; only the minimal Gitea messages/service are present. The Testing Value Gate rejects a test that only parses the `.proto` text.

- [ ] **Step 3: Commit the protocol**

```bash
git add proto/repowolf/v1/gitea.proto gen/repowolf/v1/gitea.pb.go gen/repowolf/v1/gitea_grpc.pb.go
git commit -m "feat(gitea): add repository view protocol"
```

---

### Task 2: Implement strict `tea` parsing and bounded rendering

**Files:**
- Create: `internal/client/gitea/parse.go`
- Create: `internal/client/gitea/parse_test.go`
- Create: `internal/client/gitea/fuzz_test.go`
- Create: `internal/client/gitea/render.go`
- Create: `internal/client/gitea/render_test.go`

**Interfaces:**
- Produces: `Parse(args []string) (command, error)` where `command` contains one typed `*repowolfv1.GiteaRequest` and one `outputFormat` (`simple`, `table`, or `json`).
- Produces: `render(command, *repowolfv1.GiteaResponse) ([]byte, error)`; it validates the response before returning a complete document.
- The request selector sets `RepositorySelector.Owner` and `Name` only; host and SSH port stay empty so the server remains authoritative.

- [ ] **Step 1: Write the failing parser table**

Cover both command aliases, `--repo`/`-r`, `--output`/`-o`, all three formats, default `simple`, case-folded selector agreement, and these failures: missing positional or flag selector, conflicting selectors, duplicate long/short aliases, malformed Gitea slugs, extra positionals, `--flag=value`, login/remote/help/version/list forms, unknown output, 65 args, over-64-KiB argv, invalid UTF-8, and NUL.

Use assertions shaped like:

```go
parsed, err := Parse([]string{"repos", "Group/Repo", "-r", "group/repo", "-o", "json"})
if err != nil || parsed.format != outputJSON {
    t.Fatalf("Parse() = %#v, %v", parsed, err)
}
selector := parsed.request.GetContext().GetRepository()
if selector.GetOwner() != "Group" || selector.GetName() != "Repo" || selector.GetHost() != "" {
    t.Fatalf("selector = %#v", selector)
}
```

- [ ] **Step 2: Run the parser tests and confirm RED**

```bash
go test ./internal/client/gitea -run '^TestParse' -count=1
```

Expected: FAIL because the package and parser do not exist.

- [ ] **Step 3: Implement the closed parser**

Use private constants `maximumArguments = 64` and `maximumArgvBytes = 64 << 10`. Parse tokens sequentially; map long and short spellings to one canonical flag identity before duplicate detection. Validate each owner/name with the configured Gitea grammar (`[A-Za-z0-9][A-Za-z0-9._-]{0,99}`, excluding `.`/`..`, and excluding a repository `.git` suffix). Preserve positional casing in the typed selector but compare `strings.EqualFold(positional, explicit)`.

- [ ] **Step 4: Add parser fuzzing**

Seed every accepted form and representative rejected forms. Split arbitrary bytes into at most 64 argv entries and assert that successful parses always contain exactly `GiteaRequest_RepositoryView`, one non-empty valid selector, no host/port, and a known format. The fuzz target must not panic.

- [ ] **Step 5: Write failing exact renderer tests**

Build a full `GiteaRepositoryRecord` fixture and assert:

```text
simple: one "key: value\n" line for all 18 fields
 table: one tab-separated 18-column header and one data row
  json: one newline-terminated object with exactly 18 snake-case typed keys
```

Assert the fixed spec order, comma-and-space topic joining for text formats, topic arrays and native scalar types for JSON, UTC RFC 3339 timestamps, control-character replacement only in simple/table, missing request ID/result/repository rejection, invalid timestamp rejection, exact 8 MiB success, one byte over rejection, and zero writes because `render` returns bytes before the caller writes.

- [ ] **Step 6: Run renderer tests and confirm RED**

```bash
go test ./internal/client/gitea -run '^TestRender' -count=1
```

Expected: FAIL because rendering does not exist.

- [ ] **Step 7: Implement renderer validation and formatting**

Define the field order once as a private slice used by simple/table and JSON shaping. Validate `response.GetMeta().GetRequestId()`, `response.GetRepositoryView()`, its repository, and both valid Protobuf timestamps. Convert timestamps with `AsTime().UTC().Format(time.RFC3339)`. Marshal an ordered JSON struct (not a map), append one newline, and reject `len(output) > 8<<20` before returning it.

- [ ] **Step 8: Format and run parser/renderer tests and fuzz seeds**

```bash
gofmt -w internal/client/gitea
go test ./internal/client/gitea -count=1
go test ./internal/client/gitea -run '^$' -fuzz '^FuzzParse$' -fuzztime=10s
```

Expected: PASS.

- [ ] **Step 9: Commit parsing and rendering**

```bash
git add internal/client/gitea
git commit -m "feat(tea): parse and render repository view"
```

---

### Task 3: Wire the `tea` multicall personality and package alias

**Files:**
- Create: `internal/client/gitea/client.go`
- Create: `internal/client/gitea/client_test.go`
- Modify: `cmd/repowolf-client/main.go`
- Modify: `cmd/repowolf-client/main_test.go`
- Modify: `nix/package-client.nix`
- Modify: `nix/checks.nix`
- Modify: `nix/bubblewrap-check.nix`
- Modify: `examples/docker/sandbox/Dockerfile`
- Modify: `examples/docker/README.md`
- Modify: `scripts/ci/docker-example/assert-sandbox-boundary.sh`
- Modify: `scripts/check-release.sh`
- Modify: `internal/testutil/fakeexec.go`
- Modify: `internal/testutil/fakeexec_test.go`

**Interfaces:**
- Produces: `gitea.Run(ctx context.Context, args []string, stdout, stderr io.Writer) int`.
- Consumes: `clientconfig.LoadEnv`, `clientconfig.Dial`, and `repowolfv1.NewGiteaServiceClient`.
- Produces: executable basename `tea` as a symlink to `repowolf-client`; no upstream `tea` closure dependency.

- [ ] **Step 1: Write failing client and multicall tests**

Test usage failures return 2 with exactly `tea: expected repos OWNER/REPO --repo OWNER/REPO [--output table|simple|json]\n`, without echoing rejected input. Test configuration, connection, RPC/response/render, and short-write failures return 1 with stable category diagnostics. Extend basename dispatch and signal-helper tests so `tea` preserves SIGINT/SIGTERM exit behavior.

- [ ] **Step 2: Run the focused tests and confirm RED**

```bash
go test ./internal/client/gitea ./cmd/repowolf-client -run 'TestRun|TestMode|TestClientPreservesSignal' -count=1
```

Expected: FAIL because `Run` and the `tea` personality do not exist.

- [ ] **Step 3: Implement the client shell**

Follow the existing GitHub transport sequence but keep Gitea diagnostics separate:

```go
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int
func executeCommand(ctx context.Context, client repowolfv1.GiteaServiceClient, parsed command, stdout io.Writer) error
```

Parse before loading environment, dial TLS using existing client configuration, apply the existing two-minute operation timeout, call only `GiteaService.Execute`, render fully, then use an exact-write loop. Preserve signal causes and never inspect stdin, cwd, TTY, provider environment, or upstream `tea` configuration.

- [ ] **Step 4: Add multicall dispatch**

Make `modeForBase` accept `tea`; dispatch it to `clientgitea.Run`. Update unknown-mode usage to `usage: gh | tea | repowolf-git-ssh`. Keep Git SSH stdin behavior unchanged. Extend `testutil.Binaries` and `BuildBinaries` with `Tea string` and create the same test-only symlink used by black-box integration tests.

- [ ] **Step 5: Expose and verify the built alias directly**

Add `ln -s repowolf-client $out/bin/tea` to `nix/package-client.nix`. In `nix/checks.nix`, assert the symlink target and invoke the built alias with a rejected command to verify the bounded `tea` usage diagnostic; keep upstream `tea` absent from the closure. Extend `nix/bubblewrap-check.nix`, the sandbox Dockerfile/README, `assert-sandbox-boundary.sh`, and `scripts/check-release.sh` anywhere they enumerate or invoke client personalities. Do not add a test that merely greps these declarations.

Run:

```bash
gofmt -w internal/client/gitea/client.go internal/client/gitea/client_test.go cmd/repowolf-client/main.go cmd/repowolf-client/main_test.go internal/testutil/fakeexec.go internal/testutil/fakeexec_test.go
go test ./internal/client/gitea ./cmd/repowolf-client ./internal/testutil -count=1
nix build .#repowolf-client --no-link --print-out-paths
bash -n scripts/check-release.sh scripts/ci/docker-example/assert-sandbox-boundary.sh
scripts/check-release.sh
```

Expected: Go and release smoke tests pass; the Nix output and release archive expose `tea` as the RepoWolf multicall client, and the sandbox path invokes it without containing upstream `tea`.

- [ ] **Step 6: Commit the personality and package alias**

```bash
git add internal/client/gitea/client.go internal/client/gitea/client_test.go cmd/repowolf-client/main.go cmd/repowolf-client/main_test.go internal/testutil/fakeexec.go internal/testutil/fakeexec_test.go nix/package-client.nix nix/checks.nix nix/bubblewrap-check.nix examples/docker/sandbox/Dockerfile examples/docker/README.md scripts/ci/docker-example/assert-sandbox-boundary.sh scripts/check-release.sh
git commit -m "feat(tea): add restricted client personality"
```

---

### Task 4: Add the Gitea repository SDK adapter

**Files:**
- Create: `internal/provider/gitea/operation.go`
- Create: `internal/provider/gitea/adapter.go`
- Create: `internal/provider/gitea/adapter_test.go`
- Modify: `internal/rpcstatus/status.go`
- Modify: `internal/rpcstatus/status_test.go`

**Interfaces:**
- Produces: `ValidateRequest(*repowolfv1.GiteaRequest) error`, `Capability(*GiteaRequest) (config.Capability, error)`, and `OperationName(*GiteaRequest) (string, error)` returning `repository:read` and `gitea.repository_view` only.
- Produces: `NewRepositoryAdapter(*sdk.Client) (*RepositoryAdapter, error)`.
- Produces: `RepositoryAdapter.Execute(context.Context, policy.ResolvedRepository, *GiteaRequest) (*GiteaResponse, error)`.
- Internally consumes a fakeable `GetRepo(context.Context, owner, name string) (*sdk.Repository, error)` seam; production wraps issue-#8's single SDK client and serializes `SetContext` plus `GetRepo` so concurrent requests cannot exchange contexts.

- [ ] **Step 1: Write failing operation and adapter tests**

Test nil/missing/wrong request branches, one canonical capability/name, one `GetRepo` call using configured canonical owner/name, complete field mapping, topic-order preservation, request cancellation, nil SDK result, and no retry. Add malformed repository cases for wrong `FullName`, invalid UTF-8/NUL strings or topics, negative counters/size, empty URLs, zero timestamps, and `Updated.Before(Created)`.

Use a fake shaped as:

```go
type fakeRepositoryGetter struct {
    calls int
    owner, name string
    repository *sdk.Repository
    err error
}
func (f *fakeRepositoryGetter) GetRepo(ctx context.Context, owner, name string) (*sdk.Repository, error)
```

- [ ] **Step 2: Run tests and confirm RED**

```bash
go test ./internal/provider/gitea -run 'Test.*Repository|TestGiteaOperation' -count=1
```

Expected: FAIL because the repository operation and adapter do not exist.

- [ ] **Step 3: Implement validation and canonical operation mapping**

Accept only a non-nil `GiteaRequest_RepositoryView`. Validate the typed selector in the service task, not in the adapter. Keep this package provider-specific; do not introduce a forge DTO or operation registry.

- [ ] **Step 4: Implement cancellable SDK execution and normalization**

The production SDK wrapper must lock around `client.SetContext(ctx)` and `client.GetRepo(owner, name)` because SDK v0.25.1 stores context on the client. Map `context.Canceled` and `context.DeadlineExceeded` unchanged; add `rpcstatus.ErrProviderFailure` and map it to `codes.Unavailable` with the stable message `provider failure`. Return that sentinel for all other provider/HTTP failures without wrapping SDK text into a client-visible or audit-visible value.

Normalize into `GiteaRepositoryRecord`; convert non-negative SDK `int` values to `uint64` only after validation and create timestamps with `timestamppb.New`. Use `HTMLURL` for `url`, plus `SSHURL` and `CloneURL`, all required non-empty.

- [ ] **Step 5: Run adapter and race tests**

```bash
gofmt -w internal/provider/gitea/operation.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/rpcstatus/status.go internal/rpcstatus/status_test.go
go test -race ./internal/provider/gitea ./internal/rpcstatus -count=1
```

Expected: PASS; the fake records one call and the race detector reports no SDK-context race.

- [ ] **Step 6: Commit the adapter**

```bash
git add internal/provider/gitea/operation.go internal/provider/gitea/adapter.go internal/provider/gitea/adapter_test.go internal/rpcstatus/status.go internal/rpcstatus/status_test.go
git commit -m "feat(gitea): adapt repository view through SDK"
```

---

### Task 5: Extract the shared provider lifecycle and bounded terminal audit metadata

**Files:**
- Create: `internal/server/provider_context.go`
- Create: `internal/server/provider_lifecycle.go`
- Create: `internal/server/provider_lifecycle_test.go`
- Modify: `internal/server/audit.go`
- Modify: `internal/server/interceptor.go`
- Modify: `internal/server/github.go`
- Modify: `internal/server/github_test.go`
- Modify: `internal/server/github_response_limit_test.go`
- Modify: `integration/audit_assertions_test.go`

**Interfaces:**
- Produces private `providerLifecycle.Resolve(ctx, selector, capability, expectedKind, operation) (policy.ResolvedRepository, error)`.
- Produces private `providerLifecycle.Complete(ctx, proto.Message, func(*repowolfv1.ResponseMeta)) error`.
- Produces request-scoped terminal metadata containing only canonical operation, trusted provider/repository, and Protobuf input/output sizes.
- `githubService` retains provider-specific validation, selector construction, capability mapping, operation mapping, and executor dispatch.

- [ ] **Step 1: Write failing lifecycle and terminal-audit tests**

Run identical GitHub and synthetic Gitea cases through the unary interceptor. Prove accepted-then-terminal ordering, canonical operation, trusted provider/repository, `proto.Size(request)` input bytes, `proto.Size(response)` output bytes, output zero on nil response, and completed/denied/cancelled/failed outcomes. Prove validation and pre-resolution denial leave provider/repository empty, early interceptor rejection retains the gRPC method name, accepted-audit failure prevents execution, and no repository content or error text enters events.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/server -run 'TestProviderLifecycle|TestUnaryAudit.*Provider|TestGitHubService' -count=1
```

Expected: FAIL because terminal metadata and the shared lifecycle do not exist.

- [ ] **Step 3: Add bounded request-context metadata**

At unary-interceptor entry, compute input bytes only when the request implements `proto.Message`. Carry a private mutable metadata record in the derived context; do not export it outside `internal/server`. Initialize operation to `info.FullMethod`, then permit the validated lifecycle to replace it with a constant canonical operation and, only after successful resolution, attach trusted provider/repository.

After the handler returns, compute output bytes from a non-nil `proto.Message`; keep zero otherwise. `writeTerminal` copies only approved fields into `audit.Event`. Stream audit behavior remains unchanged.

- [ ] **Step 4: Extract lifecycle authorization, accepted audit, and completion**

`Resolve` must: obtain principal/request ID; set the canonical operation; call exact policy resolution; enforce `repository.Provider.Kind == expectedKind`; attach trusted provider/repository only after that check; and write the accepted event before returning. `Complete` must reject nil responses, attach `ResponseMeta{RequestId: requestID}`, and enforce `proto.Size(response) <= responseLimitBytes`.

- [ ] **Step 5: Migrate GitHub without behavior drift**

Replace duplicated principal/policy/kind/accepted/meta/size code in `githubService.Execute` with the lifecycle. Keep `ValidateGitHubRequest`, `Capability`, `OperationName`, `githubSelector`, executor call, errors, response bytes, and command behavior unchanged. Update exact GitHub audit expectations so successful terminal events use `github.*`, include provider/repository/input/output bytes, and still follow accepted events.

- [ ] **Step 6: Run server and existing GitHub integration regressions**

```bash
gofmt -w internal/server/provider_context.go internal/server/provider_lifecycle.go internal/server/provider_lifecycle_test.go internal/server/audit.go internal/server/interceptor.go internal/server/github.go internal/server/github_test.go internal/server/github_response_limit_test.go integration/audit_assertions_test.go
go test -race ./internal/server ./integration -run 'TestProviderLifecycle|TestGitHub|TestRestrictedGH|TestMarkersRemainInTheirIntendedChannels' -count=1
```

Expected: PASS; GitHub output/policy remains unchanged while terminal audit gains bounded canonical metadata.

- [ ] **Step 7: Commit the shared lifecycle**

```bash
git add internal/server integration/audit_assertions_test.go
git commit -m "refactor(server): share provider request lifecycle"
```

---

### Task 6: Add the Gitea service, trusted dispatch, and runtime composition

**Files:**
- Create: `internal/server/gitea.go`
- Create: `internal/server/gitea_test.go`
- Create: `internal/server/gitea_response_limit_test.go`
- Modify: `internal/server/options.go`
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`
- Create: `internal/app/gitea_executor.go`
- Create: `internal/app/gitea_executor_test.go`
- Modify: `internal/app/providers.go`
- Modify: `internal/app/providers_test.go`
- Modify: `internal/app/runtime.go`
- Modify: `internal/app/runtime_provider_test.go`

**Interfaces:**
- Produces: `server.GiteaExecutor.Execute(context.Context, policy.ResolvedRepository, *GiteaRequest) (*GiteaResponse, error)`.
- Produces: one app dispatcher map keyed only by `ResolvedRepository.Repository.Provider`.
- Consumes: issue-#8's `providerInstance.gitea *sdk.Client`, wrapping each client with `providergitea.NewRepositoryAdapter`.
- Server options share one provider policy but register GitHub and Gitea independently when their executor is non-nil.

- [ ] **Step 1: Write failing Gitea service tests**

Prove typed operation and selector validation happens before policy; selector owner/name are required while host/SSH port must be empty; `repository:read` succeeds; unknown, missing capability, and wrong kind all return `policy.ErrDenied`; accepted audit failure, invalid request, denial, and nil executor prevent adapter calls; success attaches request ID; exact 8 MiB succeeds and one byte over fails.

- [ ] **Step 2: Run service tests and confirm RED**

```bash
go test ./internal/server -run '^TestGitea' -count=1
```

Expected: FAIL because the service does not exist.

- [ ] **Step 3: Implement the provider-specific service shell**

Validate with `providergitea.ValidateRequest`, derive capability/operation with the provider package, build `policy.Selector{Kind: config.ProviderGitea, Owner: owner, Name: name}`, call shared lifecycle `Resolve`, dispatch, then call lifecycle `Complete`. Do not accept host, SSH port, URLs, IDs, or arbitrary fields.

- [ ] **Step 4: Write failing app dispatch and runtime matrix tests**

Create two Gitea provider instances with distinct fake SDK clients. Resolve the second provider and assert only its adapter receives canonical casing. Test missing/mismatched instances fail closed with no fallback. Extend runtime tests for GitHub-only, Gitea-only, and mixed registration; Gitea service must be omitted if no complete Gitea executor exists and GitHub registration must remain independent.

- [ ] **Step 5: Run app tests and confirm RED**

```bash
go test ./internal/app -run 'TestGiteaExecutor|TestNewRuntime.*Gitea|TestBuildProviderInstances' -count=1
```

Expected: FAIL because the Gitea executor is not composed.

- [ ] **Step 6: Implement trusted provider-ID dispatch and composition**

Build `giteaExecutor.adapters map[string]server.GiteaExecutor` from non-nil issue-#8 SDK clients. Select only `repository.Repository.Provider`, reject missing entries with `rpcstatus.ErrServiceUnavailable`, and never inspect untrusted selector data for provider choice. Pass the policy snapshot and independent GitHub/Gitea executors into `server.New`; register each service only when its executor is complete.

- [ ] **Step 7: Run app/server race tests**

```bash
gofmt -w internal/server/gitea.go internal/server/gitea_test.go internal/server/gitea_response_limit_test.go internal/server/options.go internal/server/server.go internal/server/server_test.go internal/app/gitea_executor.go internal/app/gitea_executor_test.go internal/app/providers.go internal/app/providers_test.go internal/app/runtime.go internal/app/runtime_provider_test.go
go test -race ./internal/app ./internal/server ./internal/provider/gitea ./internal/policy -count=1
```

Expected: PASS; multiple Gitea clients cannot cross-route authority, token, or canonical repository identity.

- [ ] **Step 8: Commit service composition**

```bash
git add internal/server internal/app
git commit -m "feat(gitea): serve repository view by provider"
```

---

### Task 7: Prove the packaged client-to-broker path with fake adapters

**Files:**
- Modify: `cmd/repowolf-client/main_test.go`
- Modify: `internal/server/provider_lifecycle_test.go`
- Modify: `internal/app/gitea_executor_test.go`
- Modify: `integration/audit_assertions_test.go`
- Modify: `integration/leak_test.go`
- Create: `integration/tea_test.go`

**Interfaces:**
- Consumes: the built `tea` alias, TLS client configuration, real gRPC server, fake Gitea adapter seam, and audit sink.
- Produces: executable proof that malformed/unauthorized calls never reach an adapter and successful output/audit contain no provider credential.

- [ ] **Step 1: Add a fake-adapter full-path fixture**

Start a real TLS server with a fake `server.GiteaExecutor`, run the built multicall binary under basename `tea`, and assert exact simple and JSON output. Record the typed request and trusted resolved repository; assert the request carries one selector and no token/provider endpoint.

- [ ] **Step 2: Add denial, cancellation, and leak cases**

Run an ungranted selector and assert the stable operation diagnostic, no adapter call, and terminal-only denied audit with empty provider/repository. Cancel a blocked fake adapter and assert signal exit behavior and a canonical cancelled terminal event. Add Gitea token, SDK-error, description, topic, URL, and argv markers to leak assertions for client output, diagnostics, audit, and process arguments.

- [ ] **Step 3: Run focused black-box tests**

```bash
gofmt -w cmd/repowolf-client/main_test.go internal/server/provider_lifecycle_test.go internal/app/gitea_executor_test.go integration/audit_assertions_test.go integration/leak_test.go integration/tea_test.go
go test -race ./cmd/repowolf-client ./internal/app ./internal/server ./integration -run 'Tea|Gitea|ProviderLifecycle|MarkersRemain' -count=1
```

Expected: PASS with exact output and safe audit ordering.

- [ ] **Step 4: Commit fake-adapter end-to-end proof**

```bash
git add cmd/repowolf-client/main_test.go internal/server/provider_lifecycle_test.go internal/app/gitea_executor_test.go integration/audit_assertions_test.go integration/leak_test.go integration/tea_test.go
git commit -m "test(gitea): prove restricted tea request path"
```

---

### Task 8: Add the real Gitea 1.27.2 integration gate and verify the repository

**Files:**
- Create: `integration/gitea_repository_test.go`
- Create: `integration/testdata/gitea-policy.yaml`
- Modify: `.github/workflows/ci.yml`
- Modify: `integration/audit_assertions_test.go`
- Modify: `integration/leak_test.go`
- Modify: `internal/testutil/server.go`
- Modify: `internal/testutil/server_test.go`

**Interfaces:**
- Consumes: Docker, digest-pinned `gitea/gitea:1.27.2`, real issue-#8 SDK transport, TLS RepoWolf broker, packaged `tea`, and audit JSONL.
- Produces: one opt-in Linux integration test, `TestRestrictedTeaRepositoryViewAgainstGitea`, required in CI.

- [ ] **Step 1: Resolve and record an immutable Gitea image digest**

Run:

```bash
docker pull docker.io/gitea/gitea:1.27.2
docker image inspect docker.io/gitea/gitea:1.27.2 --format '{{index .RepoDigests 0}}'
```

Store the returned value verbatim as the test's `giteaImage` constant and require it to contain `:1.27.2@sha256:` (or use the equivalent repository digest plus an adjacent `giteaVersion = "1.27.2"` assertion). Do not leave a mutable tag or placeholder in committed code.

- [ ] **Step 2: Build a hermetic real-provider fixture**

Under a Linux integration build tag, generate a test CA and server certificate for the random loopback Gitea authority, mount them with disposable storage, and start the pinned container with Gitea HTTPS enabled. Wait on `/api/v1/version` using that CA, create an admin/token through documented non-interactive Gitea commands, then use the API to create `CanonicalOwner/CanonicalRepo`, commit content, and set repository description, default branch, booleans, topics, and timestamps/counters that Gitea permits. Register cleanup before seeding and capture container logs on failure.

- [ ] **Step 3: Start RepoWolf and run packaged `tea`**

Add `GiteaAPIHost string` and `GiteaCAFile string` to `testutil.ServerOptions`, and render those two values into `integration/testdata/gitea-policy.yaml` alongside the canonical repository, one granted and one ungranted principal selector, and a unique Gitea token marker. Start the real TLS broker, invoke the packaged alias for simple and JSON output, and assert all 18 fields, canonical upstream casing, typed JSON values, accepted/completed audit ordering, and bounded byte counts.

- [ ] **Step 4: Prove denial, cancellation, and credential non-disclosure**

Assert an ungranted selector produces the same sanitized denial with no upstream repository call. Cancel a blocked request and verify the terminal outcome. Inspect sandbox/client environment, process arguments, stdout/stderr, broker audit, and container-facing request capture so the Gitea token marker appears only in the broker's authenticated HTTP request and nowhere else.

- [ ] **Step 5: Add the explicit CI gate and run it**

Add a CI step (not a YAML-content test) that runs:

```bash
go test -tags=gitea_integration ./integration -run '^TestRestrictedTeaRepositoryViewAgainstGitea$' -count=1 -v
```

The test may skip locally when Docker is unavailable, but when the integration build tag is set in CI it must fail rather than skip if Docker or the pinned image cannot run.

- [ ] **Step 6: Run generated, static, race, fuzz, Nix, OCI, and release gates**

Because no `AGENTS.md` exists, run the repository CI/release commands directly:

```bash
go tool buf lint
go tool buf breaking --against '.git#branch=origin/main'
scripts/check-generated.sh
test -z "$(gofmt -l .)"
go vet ./...
go test -race ./...
go test ./internal/client/gitea -run '^$' -fuzz '^FuzzParse$' -fuzztime=30s
go test -tags=gitea_integration ./integration -run '^TestRestrictedTeaRepositoryViewAgainstGitea$' -count=1 -v
nix develop -c scripts/ci/oci/lint.sh
nix flake check --accept-flake-config --print-build-logs
scripts/check-release.sh
git diff --check
```

Expected: every command exits zero. Existing GitHub, Git, security, generated-code, integration, package, OCI, and release behavior remains green.

- [ ] **Step 7: Verify scope and production artifact contents**

Run:

```bash
git status --short
git diff --name-only 8bd95f5..HEAD
client="$(nix build .#repowolf-client --no-link --print-out-paths)"
test "$(readlink "$client/bin/tea")" = repowolf-client
find "$client" -type f -o -type l | sort
```

Expected: only issue-#8 baseline integration plus issue-#9 protocol/client/adapter/server/app/test/package files appear; `tea` is the RepoWolf symlink and no upstream `tea` executable is present. No issue or pull-request Gitea messages or commands exist.

- [ ] **Step 8: Commit the real-provider gate**

```bash
git add integration/gitea_repository_test.go integration/testdata/gitea-policy.yaml integration/audit_assertions_test.go integration/leak_test.go internal/testutil/server.go internal/testutil/server_test.go .github/workflows/ci.yml
git commit -m "test(gitea): verify repository view against Gitea"
```

- [ ] **Step 9: Confirm the completed branch state**

```bash
git status --short
git log --oneline 8bd95f5..HEAD
```

Expected: the worktree is clean and the log contains the plan commit plus the eight implementation commits.
