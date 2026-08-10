# CI Shell Extraction Design

Status: conversationally approved design, pending written-spec review. Audience: RepoWolf maintainers who run or modify the Docker example smoke workflow.

## Goal

Make `.github/workflows/ci.yml` easier to scan by moving substantial inline shell programs into normal source files with syntax highlighting, ShellCheck coverage, direct local entry points, and behavioral CI execution.

Preserve the workflow's named-step granularity and keep CI exercising the documented Docker example instead of a parallel test-only implementation.

## Scope

Extract an inline shell program only when it contains more than five semantic shell commands.

- A command split across several physical lines counts as one command.
- Commands inside nested shell programs such as `sh -c '…'` count toward the threshold.
- Heredoc content written in another language does not count as shell commands.
- Shorter multiline blocks remain inline.
- The threshold is a design and review rule, not a new automated workflow-content test.

The current workflow has six qualifying steps:

1. Build sandbox image from current-commit archives.
2. Bootstrap disposable state and verify broker ownership.
3. Install observable fake SSH fixture.
4. Assert GitHub policy outcomes.
5. Assert broker launched fake SSH with upload-pack argv.
6. Assert sandbox boundary.

## Non-goals

- Extracting every `run:` command or every multiline block.
- Combining the Docker smoke job into one monolithic script.
- Changing RepoWolf production Go behavior, configuration schemas, policy, or credential boundaries.
- Replacing the documented example with a CI-only bootstrap or readiness implementation.
- Turning private functions from `examples/docker/bootstrap.sh` into a supported sourced library.
- Adding static tests that assert workflow YAML text, command counts, file names, or dependency versions.
- Enforcing ShellCheck over every repository shell script in this change.
- Adding shfmt or a repository-wide shell formatting policy.
- Publishing or tagging `v0.1.0`.

## Decisions

- **One primary script per qualifying workflow step.** GitHub retains the existing step names, logs, ordering, and failure isolation.
- **CI-only scripts live under `scripts/ci/docker-example/`.** They are locally runnable and not tied to GitHub Actions environment variables.
- **Scripts locate the repository themselves.** A script derives the repository root from its own path and does not depend on the caller's current directory.
- **Reuse stable executable interfaces.** CI invokes the documented `bootstrap.sh`, `wait-for-broker.sh`, Compose files, and behavior-test scripts. It does not source their private functions.
- **Fixtures are normal source files.** Embedded Go programs and expected output become language-appropriate files instead of shell heredocs.
- **Nested sandbox shell is lintable.** The sandbox-boundary script has a POSIX-compatible inner mode and passes itself to the sandbox over standard input.
- **ShellCheck is pinned with the project toolchain.** Add it to both the flake development shell and `devenv.nix`.
- **Behavior is the test surface.** Extracted scripts run in the real Docker smoke job; lint and syntax checks complement rather than replace that smoke coverage.

## Directory layout

```text
scripts/ci/docker-example/
├── lint.sh
├── build-sandbox-image.sh
├── bootstrap-disposable-state.sh
├── install-fake-ssh-fixture.sh
├── assert-github-policy.sh
├── assert-fake-ssh.sh
├── assert-sandbox-boundary.sh
└── fixtures/
    ├── expected-ssh-argv.txt
    ├── fake-ssh/
    │   └── main.go
    └── release-server/
        └── main.go
```

The two Go fixtures use separate package directories so `go test ./...` and `go vet ./...` can inspect both `package main` programs without duplicate `main` symbols.

## Script contracts

The five Bash orchestration scripts and `lint.sh` use Bash strict mode. They fail on unset variables, failed commands, and failed pipelines. `assert-sandbox-boundary.sh` uses POSIX `set -eu` in both modes so the same file runs under the host shell and the image's `/bin/sh`. CI invokes every primary script without positional arguments; only the sandbox script's internal re-entry supplies `--inside`.

### `build-sandbox-image.sh`

Responsibilities:

- Build snapshot release archives from the current checkout.
- Copy the checksums and Linux archives into a temporary release fixture.
- Build and run `fixtures/release-server/main.go` on localhost.
- Wait for the bounded fixture-readiness condition.
- Build `repowolf-sandbox:local` against that fixture.

The script owns and removes the temporary fixture, server process, and server log through success-, error-, and signal-safe cleanup traps.

### `bootstrap-disposable-state.sh`

Responsibilities:

- Create a temporary SSH key and known-hosts fixture.
- Write the existing dummy provider token to the ignored example `.env`.
- Invoke the public `examples/docker/bootstrap.sh` interface with the fixture paths.
- Remove the temporary SSH fixture even when bootstrap or validation fails.
- Validate broker config ownership/readability and effective SSH configuration exactly as the current step does.

The temporary private key remains test-only and is never printed.

### `install-fake-ssh-fixture.sh`

Responsibilities:

- Build `fixtures/fake-ssh/main.go` as the static observable fake SSH tool.
- Prepare its host-visible argv log.
- Render the smoke-only SSH tool path into disposable broker config.
- Apply the required broker ownership and modes.
- Validate the final config as the broker UID.

The ownership helper remains CI-specific. It is not extracted from or shared with the private `rw_as_host` implementation in `bootstrap.sh` because the mount paths, data ownership, and lifecycle differ.

### `assert-github-policy.sh`

Responsibilities:

- Preserve the expected upstream provider failure for repository view with the dummy token.
- Assert the accepted repository-view audit event.
- Preserve the expected policy denial for run listing.
- Assert the terminal GitHub RPC operation, denied outcome, and `PermissionDenied` reason.
- Assert that `github.run_list` was never accepted.

The script uses a temporary broker log and removes it on exit.

### `assert-fake-ssh.sh`

Responsibilities:

- Preserve the expected fake-SSH failure from `git ls-remote`.
- Assert the accepted and failed `git.upload-pack` audit events.
- Compare the observed argv file with `fixtures/expected-ssh-argv.txt`.
- Assert `GIT_TERMINAL_CATEGORY_PROVIDER_FAILURE`.

The script uses a temporary broker log and removes it on exit.

### `assert-sandbox-boundary.sh`

The script has two modes:

- **Host mode:** locate the example directory and run the same script inside the sandbox via non-interactive standard input.
- **Inner mode:** use POSIX shell syntax to assert that `GH_TOKEN` is absent, the `gh` and Git-SSH shims target `repowolf-client`, real `ssh` is absent, and the effective UID is `65532`.

Keeping both modes in one file preserves one primary script for the workflow step while allowing ShellCheck to parse the inner assertions.

## Workflow integration

Add a Docker-example shell lint step that runs from the repository root:

```sh
nix develop -c scripts/ci/docker-example/lint.sh
```

Each qualifying workflow step retains its current name and becomes a direct invocation of its corresponding script from the repository root. Existing short blocks remain inline, including:

- Bubblewrap user-namespace setup.
- OCI image build/load smoke.
- Example syntax and focused behavior checks.
- Broker image build/load.
- Host installer behavior checks.
- Broker startup/readiness diagnostics.
- Always-run Compose teardown.

The job-level `REPOWOLF_IMAGE=repowolf:mvp`, Compose overlay, artifact names, step order, and `needs: test` dependency remain unchanged.

## Reuse analysis

The useful sharing boundary already exists at executable and declarative interfaces:

- `bootstrap.sh` is the documented state generator and is invoked directly by CI.
- `wait-for-broker.sh` is the bounded readiness probe used by both documentation and CI.
- `compose.yaml` and `compose.smoke.yaml` define normal and smoke-only topology.
- Installer behavior tests exercise the public installer processes.

No new sourced shell library is justified. The apparent overlap consists mainly of small Docker ownership commands, path setup, cleanup traps, and Compose invocations. Their inputs, security assumptions, and lifecycles differ between user bootstrap and CI fixtures. Sharing those internals would make the security-sensitive example depend on a test harness, expand its supported API, and complicate Bash 3.2 and ShellCheck handling without removing meaningful domain duplication.

CI scripts may repeat small path-derivation or Compose setup expressions when that keeps each step independently runnable. A CI-only helper should be introduced only if implementation reveals a larger cohesive behavior shared by several scripts, not merely a few common lines.

## Linting and syntax checks

Add `shellcheck` to:

- `flake.nix` default development-shell packages.
- `devenv.nix` packages.

`scripts/ci/docker-example/lint.sh` explicitly checks:

- Every shell file under `scripts/ci/docker-example/`.
- `examples/docker/bootstrap.sh`.
- `examples/docker/wait-for-broker.sh`.

It runs the appropriate shell syntax check and ShellCheck dialect for each file. Real findings receive behavior-preserving fixes. Intentional constructs may use narrow, adjacent ShellCheck directives with a reason; broad file-wide suppression is not acceptable.

The existing example syntax-and-behavior step continues to cover the remaining installer and behavior-test scripts with `bash -n` and direct execution. This change does not expand ShellCheck to those large existing files unless an extracted script directly sources or invokes them as a reusable implementation boundary.

## Failure handling and cleanup

- Every extracted script propagates non-zero failures to its workflow step.
- Expected negative tests use explicit conditionals and fail if the command unexpectedly succeeds.
- Temporary directories, processes, SSH fixtures, and logs use scoped cleanup traps.
- Cleanup never removes persistent example state that another workflow step requires.
- The existing always-run Compose teardown remains the final job cleanup.
- Provider tokens, private keys, and principal tokens are never printed.
- Failure diagnostics may print fixture-server or broker logs, which contain only the existing dummy CI provider token context and audited operation metadata, not token values.

## Verification

### Automated behavior

- `scripts/ci/docker-example/lint.sh` passes under `nix develop`.
- Existing example behavior tests pass.
- Each of the six extracted scripts runs through the real `docker-example-smoke` job.
- Existing `go test -race ./...`, `go vet ./...`, Go formatting, Nix flake checks, image smoke, amd64 release smoke, and arm64 release smoke remain green.

### Direct checks

- Parse `.github/workflows/ci.yml` as YAML.
- Confirm the six qualifying steps invoke scripts and shorter blocks remain inline.
- Run Compose configuration validation with the required dummy environment values.
- Inspect the final diff for generated archives, logs, credentials, temporary state, or unrelated changes.
- Confirm branch, remote, and PR checks at the exact pushed head.

No new automated test will assert workflow YAML contents, dependency-list text, or command-count structure. Those are static configuration properties covered by parsing, linting, direct inspection, and the real workflow run.

## Acceptance criteria

- `.github/workflows/ci.yml` contains no inline shell program with more than five semantic commands from the identified Docker smoke steps.
- The six existing step names and failure boundaries remain visible in GitHub Actions.
- All extracted and directly shared shell scripts receive syntax and ShellCheck coverage.
- Individual CI scripts locate the repository and run locally from any current directory; the pinned `nix develop -c scripts/ci/docker-example/lint.sh` entry point runs from the repository root, or `lint.sh` runs directly inside an active development shell.
- Embedded Go programs and expected SSH argv are normal fixture files with language-appropriate highlighting.
- CI continues to exercise `bootstrap.sh`, `wait-for-broker.sh`, Compose definitions, and installer behavior rather than duplicated equivalents.
- No new cross-boundary sourced shell library is introduced.
- Cleanup, non-disclosure, policy/audit assertions, and sandbox-boundary behavior remain intact.
- No release or tag is created.
