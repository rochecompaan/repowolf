# README onboarding and multi-architecture distribution design

Status: approved revised design (2026-08-21)

## Goal

Make the README serve a developer who wants to isolate and secure GitHub
access for AI coding agents, whether working alone or in a team. The README
must explain the value in seconds, help the reader choose Docker or native
Linux, and lead to a successful first brokered GitHub operation.

Make the Docker path usable on common developer hosts by publishing the broker
image for both Linux amd64 and arm64.

## Audience

Primary: a hands-on developer evaluating and adopting RepoWolf for the first
time. The developer can use Linux, macOS, or Windows with WSL2.

Secondary: an operator who wants a long-running native Linux broker managed by
systemd, Home Manager, Kubernetes, or another supervisor.

## Current problems

- The introduction is accurate but dense. It assumes that the reader already
  knows the problem space.
- The section order follows a production deployment. It does not provide a
  short first-success path.
- Docker and native Linux appear as installation formats rather than setup
  choices. The README does not explain which one fits a reader's host or goal.
- Native artifacts support only Linux, but this constraint appears after the
  introduction rather than at the point where the reader chooses a setup.
- The capability model (providers, repositories, principals, and grants) is
  not explained before the full YAML example.
- The release workflow publishes the OCI image from an amd64 runner only. It
  does not publish a manifest containing both amd64 and arm64 images.

## Onboarding design

### Default approach

Lead with Docker Compose. It has the broadest host reach, requires no native
RepoWolf installation, and demonstrates the intended credential boundary with
a broker container and a credential-free sandbox container.

Present native Linux as a clear alternative for readers who want lower
overhead or service-manager integration. Do not give both paths equal visual
weight; this would add choice without helping macOS or Windows users.

### README structure

1. Logo and existing width.
2. What and why: 3-4 plain sentences. Sandboxes can still expose GitHub tokens
   and SSH keys. RepoWolf keeps those credentials broker-side and gives the
   sandbox restricted `gh` and Git entry points instead.
3. How it works: a small ASCII flow. A credential-free sandbox connects over
   TLS to a policy-enforcing broker, which authenticates to GitHub and emits
   audit JSON Lines.
4. Features: short bullets covering per-repository policy, the restricted
   `gh` subset, brokered Git over SSH, strict startup validation, audit output,
   and no provider credentials in the sandbox.
5. Choose your setup: the host-based decision table below.
6. Docker Compose quickstart: the default first-success path.
7. Enable Git access: a second milestone with its extra authentication and
   host-verification requirements.
8. Native Linux: a short explanation and links to installation,
   configuration, and deployment documentation.
9. Next steps: links to the detailed Docker example, moved reference docs, and
   the MVP design.

Use the existing CI workflow for a CI badge if its stable badge URL renders
without adding an external badge service. Do not add a badge that cannot be
verified directly.

### Setup decision table

The README will show this choice near the start:

| Host | Recommended setup | Notes |
| --- | --- | --- |
| Linux | Docker for the quickest start; native for a long-running broker | Native packages support amd64 and arm64 |
| macOS | Docker Desktop from Terminal | No native RepoWolf package |
| Windows | Docker Desktop through WSL2 | Run commands inside WSL2; PowerShell and cmd are not supported |

The Docker row applies to the Compose broker plus sandbox path. The native
host-broker installer remains Linux-only.

### Docker Compose quickstart

Use the existing `examples/docker` assets rather than duplicating policy and
bootstrap logic in the README. The quickstart must:

1. State the requirements: Git, Docker with Compose v2, Bash, and a GitHub
   token.
2. Clone RepoWolf and enter `examples/docker`.
3. Copy `.env.example` to `.env` and set `GH_TOKEN`.
4. Run `bootstrap.sh` for a repository the reader owns.
5. Build the sandbox image, start the broker, and wait for TLS readiness.
6. Run `gh repo view` from the sandbox container.
7. Run the existing boundary proof to show that the sandbox has no
   `GH_TOKEN`, OpenSSH client, key, or agent socket.
8. Link to the complete Docker example for policy-denial, Git, reset, and
   troubleshooting details.

The first success is `gh repo view`. It proves the brokered GitHub API path
without making SSH key setup a prerequisite.

### Enable Git access

Present Git as the next milestone. Explain that GitHub SSH access requires
broker-side authentication and verified host state, even for public
repositories.

The section will point to the detailed Docker example for preparing a
repository-scoped deploy key, verifying GitHub's published host fingerprints,
writing `known_hosts`, restarting the broker, and running `git ls-remote`.

Do not hide these requirements inside the quickstart. Do not put a private key,
real SSH client, or agent socket in the sandbox.

### Native Linux route

Explain when native Linux is a better fit:

- The host runs Linux amd64 or arm64.
- The operator wants lower container overhead.
- The broker should run under systemd or Home Manager.
- The sandbox can reach the host broker through an explicit address.

Link to the native release/Nix installation, configuration, and deployment
documents. Keep host-broker details out of the default Docker quickstart.

## Documentation layout

Move existing reference sections with light edits rather than rewriting them:

- `docs/installation.md` receives release archives, Nix packages, the OCI image,
  and the Linux platform limits for native artifacts.
- `docs/configuration.md` receives token and TLS bootstrap, service
  configuration, and client configuration. Add a short explanation of
  providers, repositories, principals, grants, and capabilities before the
  YAML example.
- `docs/deployment.md` receives OCI deployment, supervision with systemd or
  Home Manager, and Kubernetes.

The README links to each document at the relevant decision point.
`examples/docker/README.md` remains the detailed Docker walkthrough. Change it
only where the new onboarding or platform wording requires alignment.

## Multi-architecture OCI publication

### Image source

Keep `nix/oci.nix` as the single broker-image definition. Do not add a second
broker Dockerfile.

### Native builds

Use the native runners already present in CI and release workflows:

- `ubuntu-24.04` builds `linux/amd64`.
- `ubuntu-24.04-arm` builds `linux/arm64`.

Each architecture job must:

1. Confirm the native Go architecture.
2. Build `.#ociImage` with Nix.
3. Load the archive into Docker.
4. Run the image with `--version` as a smoke check.

CI performs these checks on pull requests. These are behavioral checks of the
built image, not tests that assert workflow YAML content.

### Release publication

After existing release smoke checks pass, run an architecture matrix with
package-write permission. Each job builds the Nix image and pushes an
architecture-specific tag:

- `<release-tag>-amd64`
- `<release-tag>-arm64`

A final manifest job runs only after both architecture jobs succeed. It:

1. Authenticates to GHCR.
2. Creates the canonical `<release-tag>` manifest from both architecture tags.
3. Inspects the published manifest.
4. Fails unless it contains exactly the required `linux/amd64` and
   `linux/arm64` platforms.

The implementation can use Docker Buildx only to create and inspect the
registry manifest. Buildx must not build the broker image.

### Failure behavior

If either architecture build or push fails, the canonical release tag is not
created. Architecture-specific staging tags can remain for diagnosis and a
workflow rerun. Atomic publication across GitHub release archives and GHCR is
outside this change; the guarantee applies to the canonical OCI tag.

Do not add a `latest` tag or change release versioning in this work.

## Platform wording and validation

The README will state the required setup for each host:

- Native RepoWolf: Linux amd64 and arm64.
- Docker: Linux, macOS with Docker Desktop and Terminal, and Windows with
  Docker Desktop through WSL2.
- Native PowerShell/cmd and Windows ARM: not supported by this onboarding path.

Do not label macOS or Windows as manually tested in the README until that is
true. The setup requirements can still be documented and supported as a
product decision.

### Required before the PR is complete

- Automated amd64 and arm64 OCI build and smoke checks pass.
- The Docker quickstart succeeds end-to-end on Linux.
- The sandbox boundary proof passes.
- `git ls-remote` succeeds after the documented deploy-key and `known_hosts`
  setup.
- Relative links resolve.
- Existing tests and Nix flake checks pass.

### Non-blocking external validation

Ask developers to run the same Docker workflow on:

- macOS Apple Silicon with Docker Desktop.
- macOS Intel with Docker Desktop.
- Windows 11 amd64 with WSL2 and Docker Desktop integration.

The checklist covers clean bootstrap, readiness, `gh repo view`, boundary
proof, Git `ls-remote`, and cleanup. Record feedback in PR notes or a follow-up
issue. These checks do not block the onboarding PR. Fix platform-specific
problems in follow-up changes when necessary.

## Writing style

Use short sentences, plain words, and active voice. Define uncommon terms near
their first use. Keep warnings next to the commands they constrain. Prefer one
recommended path over an unranked list of options.

## Out of scope

- Native macOS or Windows binaries and packages.
- Native PowerShell or cmd instructions.
- Windows ARM validation.
- A second broker image definition based on a Dockerfile.
- Changes to RepoWolf policy or runtime behavior.
- Rewriting plans, migration notes, verification history, or existing design
  specs unrelated to this onboarding work.
- Making external macOS or Windows validation a merge requirement.

## Acceptance criteria

1. A reader can select Docker or native Linux from one table without reading
   the reference documentation first.
2. Docker is the default quickstart and reaches a brokered `gh repo view`
   before requiring Git SSH setup.
3. The README proves the sandbox boundary and points to a separate Git setup
   milestone.
4. Native Linux users can find installation, configuration, and deployment
   instructions directly.
5. Reference content moves to the three new documents without losing security
   warnings or operational details.
6. Pull-request CI builds and smoke-tests Nix OCI images on native amd64 and
   arm64 runners.
7. A release canonical tag contains both `linux/amd64` and `linux/arm64`, and
   cannot be published from only one architecture job.
8. Linux onboarding commands work as written. External macOS and WSL2 feedback
   remains non-blocking.
