# README and onboarding redesign

Status: approved design (2026-08-21)

## Goal

Make the README serve a new user: a developer who wants to isolate and
secure GitHub access for AI coding agents, working alone or in a team.
The README must answer "what is this and why do I want it" in seconds,
then lead to a working local setup in minutes.

## Audience

Primary: a hands-on developer evaluating and adopting RepoWolf for the
first time. Secondary: team-scale operators, who follow links from the
README into the deployment documentation.

## Current problems

- The introduction is accurate but dense. It assumes the reader already
  knows the problem space. There is no problem statement and no "who is
  this for".
- The section order follows a production deployment: installation,
  tokens, TLS, full configuration, client configuration, supervision,
  Kubernetes. A newcomer gets the deployment manual, not a try-it path.
- The capability model (providers, repositories, principals, grants) is
  never explained. The reader must infer it from a full YAML example.
- The fastest existing onboarding paths are buried: `examples/docker`
  gets one link inside the OCI section, and the devenv dogfood setup is
  not mentioned at all.

## Design

### README structure

1. Logo (unchanged).
2. What and why: 3-4 plain sentences. AI coding agents run in sandboxes,
   but sandboxes still hold GitHub tokens and SSH keys. RepoWolf is a
   broker that keeps credentials service-side, so a sandbox gets a
   credential-free `gh` and `git` instead. State who it is for.
3. How it works: a small ASCII diagram. Sandbox entry points
   (`gh`, `repowolf-git-ssh`, no credentials) connect over mTLS to the
   broker. The broker enforces per-repository policy (capabilities,
   `denyRefs`, `denyDeletes`), authenticates to the provider, and emits
   audit JSON Lines.
4. Features: short bullet list covering per-repository policy, the
   restricted `gh` subset, brokered Git over SSH, strict
   startup-validated configuration, the audit log, and "no secrets in
   the sandbox".
5. Quickstart: self-contained, per the flow below.
6. Next steps: links to `docs/installation.md`, `docs/configuration.md`,
   `docs/deployment.md`, `examples/docker/README.md`, and the MVP design
   spec.
7. CI and release badges at the top, only if a workflow file exists in
   the repository. Verified during implementation; skipped otherwise.

### Content moves

Existing sections move with light edits (lead-in sentences, heading
levels), not rewrites:

- `docs/installation.md` receives Installation (release archives, Nix,
  OCI pull).
- `docs/configuration.md` receives "Bootstrap tokens and TLS", "Service
  configuration", and "Client configuration". Add one short "policy
  model" paragraph before the YAML that explains providers,
  repositories, principals, and grants.
- `docs/deployment.md` receives the OCI `docker run` walkthrough,
  "Supervision" (systemd, Home Manager), and "Kubernetes".

The README links to each document at the relevant point.

### Quickstart

The quickstart models the sandbox boundary with a demo directory. No
real sandbox is needed. Steps:

1. Install the release binaries (one command block, reusing the existing
   `go env GOARCH` pattern).
2. Generate a token: `TOKEN=$(repowolf token generate)`.
3. Create a private CA and server certificate:
   `repowolf cert init --output "$PWD/tls" --dns localhost --ip 127.0.0.1`.
4. Write a minimal `repowolf.yaml` via heredoc (~25 lines): one
   `github` provider, one repository the reader owns, one principal
   named `me` with `repository:read`, `git:read`, and `git:write`.
5. Start the broker in the current shell so it inherits `SSH_AUTH_SOCK`:
   `GH_TOKEN=$(gh auth token) REPOWOLF_TOKEN_ME=$TOKEN repowolf serve --config repowolf.yaml`.
6. In a second shell, play the sandbox role: create `~/repowolf-demo/bin`
   with `gh` and `repowolf-git-ssh` links to `repowolf-client`, export
   `REPOWOLF_ENDPOINT`, `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, and
   `GIT_SSH_COMMAND=repowolf-git-ssh`, then run `gh repo view` and
   `git clone` against the configured repository.
7. Close with two sentences on what happened: the demo environment holds
   no token and no keys; the broker authenticated and enforced policy.

The quickstart must be run end-to-end on a real machine during
implementation (localhost broker, real `gh repo view` and `git clone`).
Any step that does not work as written gets fixed before the PR opens.

### Writing style

Match the existing README voice: short sentences, plain words, active
voice, no filler. Follow the project writing rules: accuracy first,
clarity second, brevity third.

## Verification

Documentation is excluded from automated tests (Testing Value Gate).
Verify instead:

1. Run the quickstart end-to-end locally and confirm `gh repo view` and
   `git clone` succeed through the broker.
2. Check every relative link between the README and the moved documents
   resolves.
3. Confirm the existing test suite stays green; no code changes are
   expected.
4. Re-read the final README top-to-bottom as a new user would.

## Out of scope

- No code changes.
- No changes to `examples/docker` beyond fixing links if they break.
- No edits to `docs/specs`, `docs/plans`, `docs/migration`, or
  `docs/verification` content.
- No mention of the devenv dogfood setup in the README; it is a
  contributor concern, not new-user onboarding.
