# RepoWolf

RepoWolf is a repository-scoped access broker for Git and forge tooling. The sandbox protects the host from the agent; RepoWolf protects the forge from the sandbox. RepoWolf does not create, inspect, register, or attest sandboxes.

The service artifact provides `repowolf`. The credential-free client artifact is installed under both `gh` and `repowolf-git-ssh` inside a sandbox.

See the [approved MVP design](docs/specs/2026-08-01-repowolf-mvp-design.md) for the product boundary and interface plan.
