# RepoWolf Devenv Dogfood Design

**Date:** 2026-08-05
**Status:** Approved

## Goal

Create a repository-local devenv environment that uses RepoWolf's own exported Nix packages while providing the existing development toolchain. Entering the repository through direnv must expose:

- `repowolf`
- `repowolf-client`
- `go`
- `goreleaser`
- `skopeo`

The environment must consume the same `repowolf` and `repowolf-client` package outputs that external flake users consume.

## Non-goals

This change does not:

- start or configure the RepoWolf service;
- create policy, TLS, token, or provider-secret files;
- remove or change the existing `devShells` flake output;
- add git hooks, background processes, or service orchestration;
- change RepoWolf production packages or runtime behavior.

## Design

### Local package input

`devenv.yaml` will declare RepoWolf as a local Git flake input using `git+file:.?ref=HEAD`. `devenv.nix` will select both packages from:

```nix
inputs.repowolf.packages.${pkgs.stdenv.system}
```

This approach exercises the public flake package contract instead of recreating the packages with `callPackage`. The Git-backed input excludes ignored `.devenv` state, unlike a `path:` input, and remains portable because the reference is relative to the repository. Explicit `ref=HEAD` prevents `devenv.lock` from recording and rewriting task-specific branch names; a temporary-repository prototype confirmed the lock stays unchanged after a branch rename.

The environment will keep devenv's rolling Nixpkgs input for development tools. RepoWolf packages remain built by the Nixpkgs revision pinned by RepoWolf's own `flake.lock`, exactly as they are for external flake consumers.

### Environment contents

`devenv.nix` will add:

```nix
packages = [
  pkgs.go
  pkgs.goreleaser
  pkgs.skopeo
  repowolfPackages.repowolf
  repowolfPackages."repowolf-client"
];
```

No shell hook, service, process, environment secret, or runtime configuration is required for this first dogfood step.

### Activation and generated state

A committed `.envrc` will evaluate `devenv direnvrc` to install the version-compatible direnv integration, then call `use devenv`. This enables automatic activation for developers who authorize the directory with direnv. Manual `devenv shell` remains available.

`devenv.lock` will be committed so devenv and development-tool inputs are reproducible. The repository `.gitignore` will explicitly re-include the committed activation file with `!.envrc`, overriding user-level Git exclude files that commonly ignore `.envrc`. Local generated state and overrides will remain untracked through these entries:

- `.devenv*`
- `devenv.local.nix`
- `devenv.local.yaml`
- `.direnv`

The existing `.worktrees/` rule remains unchanged.

### Existing flake compatibility

The existing `flake.nix` development shell remains supported. The devenv environment mirrors its Go, GoReleaser, and Skopeo tools while adding the two RepoWolf package outputs. This avoids forcing existing `nix develop` users to migrate as part of the initial dogfood setup.

## Failure behavior

Nix evaluation or package-build failures must fail environment activation; the configuration will not fall back to mutable host-installed RepoWolf binaries. Missing direnv authorization affects only automatic activation—developers can still run `devenv shell` explicitly.

No secret values or machine-specific absolute paths may be committed.

## Verification

This is static development-environment configuration, so the Testing Value Gate does not justify a new automated test. Direct verification will prove behavior:

1. Stage new Nix-referenced files before normal Git-backed evaluation so the local flake input can see them.
2. Generate and commit `devenv.lock` through a real devenv evaluation.
3. Run `devenv shell --` checks confirming all five commands resolve from the environment.
4. Run `repowolf --version` and the credential-free `repowolf-git-ssh -G git@github.com` capability probe inside the environment to prove both package outputs execute successfully; also confirm the `gh` and `repowolf-git-ssh` links resolve.
5. Run `nix flake check --accept-flake-config --print-build-logs` to preserve existing package/check coverage.
6. Parse `devenv.nix` with `nix-instantiate --parse`; the real devenv evaluation validates YAML and module semantics; then run `git diff --check`.
7. Confirm no generated `.devenv*`, `.direnv`, or local override state appears in Git status.

## Acceptance criteria

- Automatic `direnv` activation is configured through `.envrc`.
- `devenv shell` exposes Go, GoReleaser, Skopeo, RepoWolf, and RepoWolf Client.
- Both RepoWolf binaries come from this repository's flake package outputs.
- Existing `nix develop` behavior remains available.
- The environment contains no service process, credentials, or mutable installation fallback.
- Lock and ignore behavior is reproducible and leaves generated state untracked.
