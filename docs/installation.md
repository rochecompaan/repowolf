# Installation

RepoWolf publishes native Linux packages and an OCI image. Use the
[setup table](../README.md#choose-your-setup) to select Docker or native Linux.

Native archives, Nix packages, and OCI images support Linux amd64 and arm64.
macOS and Windows users run the OCI image through Docker Desktop.

## Native release archives

Each GitHub release has `repowolf_linux_amd64.tar.gz`, `repowolf_linux_arm64.tar.gz`, and `checksums.txt`. An archive contains both binaries. Verify and install the archive matching `go env GOARCH`:

```sh
sha256sum -c checksums.txt --ignore-missing
tar -xzf "repowolf_linux_$(go env GOARCH).tar.gz"
install -m 0755 repowolf /usr/local/bin/repowolf
install -m 0755 repowolf-client /usr/local/bin/repowolf-client
ln -s repowolf-client /usr/local/bin/repowolf-git-ssh
```

Inside a sandbox, install only `repowolf-client` and link both restricted entry points:

```sh
ln -s repowolf-client /sandbox/bin/gh
ln -s repowolf-client /sandbox/bin/repowolf-git-ssh
```

Do not place the host's real `gh`, OpenSSH client, provider tokens, SSH keys, or `SSH_AUTH_SOCK` in the sandbox.

## Nix

Install the service/admin package or the separate client package from the flake:

```sh
nix profile install github:rochecompaan/repowolf#repowolf
nix profile install github:rochecompaan/repowolf#repowolf-client
```

The client package supplies `gh` and `repowolf-git-ssh` links. It does not contain the service, real provider tools, configuration, or credentials. The OCI archive is available as `.#ociImage` for image publishing; OCI consumers do not need Nix.

## OCI image

Tagged releases publish `ghcr.io/rochecompaan/repowolf:<version-tag>`. The image runs as UID/GID `65532:65532`, listens on the configured address, and contains the service-side `gh`, OpenSSH, and CA roots. It contains no policy, tokens, provider credentials, SSH keys, or TLS keys.

```sh
docker pull ghcr.io/rochecompaan/repowolf:v0.1.0
```

For a complete sandbox-image example with host-broker and compose
walkthroughs, see [examples/docker](../examples/docker/README.md).

Next, [configure RepoWolf](configuration.md).
