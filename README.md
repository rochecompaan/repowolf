<p align="center">
  <picture>
    <img alt="RepoWolf logo" src="docs/assets/repowolf-logo.png">
  </picture>
</p>

RepoWolf is a repository-scoped access broker for Git and forge tooling. The sandbox protects the host from the agent; RepoWolf protects the forge from the sandbox. RepoWolf does not create, inspect, register, or attest sandboxes.

The service/admin artifact provides `repowolf`. The credential-free client artifact provides the same multicall binary as `repowolf-client`, `gh`, and `repowolf-git-ssh`. Give sandboxes only the client artifact; provider CLIs, SSH, service configuration, and credentials stay with the service.

See the [approved MVP design](docs/specs/2026-08-01-repowolf-mvp-design.md) for the product boundary and interface plan.

## Installation

RepoWolf supports Linux on amd64 and arm64.

### Release archives

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

### Nix

Install the service/admin package or the separate client package from the flake:

```sh
nix profile install github:rochecompaan/repowolf#repowolf
nix profile install github:rochecompaan/repowolf#repowolf-client
```

The client package supplies `gh` and `repowolf-git-ssh` links. It does not contain the service, real provider tools, configuration, or credentials. The OCI archive is available as `.#ociImage` for image publishing; OCI consumers do not need Nix.

### OCI

Tagged releases publish `ghcr.io/rochecompaan/repowolf:<version-tag>`. The image runs as UID/GID `65532:65532`, listens on the configured address, and contains the service-side `gh`, OpenSSH, and CA roots. It contains no policy, tokens, provider credentials, SSH keys, or TLS keys.

```sh
docker pull ghcr.io/rochecompaan/repowolf:v0.1.0
docker run --rm -p 8443:8443 \
  --env-file /run/repowolf/service.env \
  --mount type=bind,src=/etc/repowolf/repowolf.yaml,dst=/etc/repowolf/repowolf.yaml,readonly \
  --mount type=bind,src=/run/repowolf/tls,dst=/run/repowolf/tls,readonly \
  ghcr.io/rochecompaan/repowolf:v0.1.0 \
  serve --config /etc/repowolf/repowolf.yaml
```

The mounted paths must be readable by UID 65532. Mount provider authentication only into the service container, never into an agent container.

## Bootstrap tokens and TLS

Generate a distinct token for each sandbox or project role:

```sh
repowolf token generate
```

The command prints the token once. Store it in a secret manager. Configuration names the environment variable that will hold the token; it never contains the token value or a digest.

For a local deployment, create a private CA and server certificate beneath an existing operator-controlled directory:

```sh
install -d -m 0700 /var/lib/repowolf
repowolf cert init --output /var/lib/repowolf/tls --dns repowolf.internal --ip 127.0.0.1
```

The output path must not already exist. Distribute only the generated CA certificate to clients. Keep CA and server private keys service-side. Production Kubernetes deployments can use cert-manager or another operator-controlled issuer instead.

## Service configuration

RepoWolf loads strict YAML once at startup. Unknown or duplicate fields, unsupported schema versions, invalid references, unknown capabilities, and unsafe limits are rejected. Changes to configuration, token environments, certificates, or tool paths require a restart.

```yaml
apiVersion: repowolf.dev/v1alpha1
listen: "0.0.0.0:8443"

tls:
  certificate: /run/repowolf/tls/tls.crt
  privateKey: /run/repowolf/tls/tls.key

tools:
  gh: null
  ssh: null

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git

repositories:
  example:
    provider: github-public
    owner: example
    name: repository
    git:
      denyRefs:
        - refs/heads/main
      denyDeletes: true
      maxRefUpdates: 16

principals:
  example-agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_EXAMPLE_AGENT
    grants:
      - repository: example
        capabilities:
          - repository:read
          - issues:read
          - issues:write
          - pull_requests:read
          - pull_requests:write
          - actions:read
          - statuses:read
          - git:read
          - git:write
```

Validate policy without loading token values, TLS files, or provider executables:

```sh
repowolf config validate --config /etc/repowolf/repowolf.yaml
```

Set every environment variable named by `tokenEnvs` before starting the service. For the example, place `REPOWOLF_TOKEN_EXAMPLE_AGENT=<generated-value>` in the service's protected environment. Provider authentication such as `GH_TOKEN` and SSH configuration also belongs only in the service environment and filesystem. A `null` tool path resolves `gh` or `ssh` once from service startup `PATH`; an absolute YAML path can pin either executable.

Start the service with:

```sh
repowolf serve --config /etc/repowolf/repowolf.yaml
```

Audit JSON Lines are emitted on standard output for collection by the supervisor.

## Client configuration

Each sandbox receives only these values:

- `REPOWOLF_ENDPOINT`: the service's `https://` URL, for example `https://repowolf.internal:8443`;
- `REPOWOLF_TOKEN`: that sandbox role's generated bearer token;
- `REPOWOLF_CA_FILE`: a readable PEM public CA certificate, unless the CA is already in the platform trust store;
- optional `REPOWOLF_SERVER_NAME`: an explicit TLS server name when it must differ from the endpoint host.

Put the restricted `gh` first in sandbox `PATH` and set `GIT_SSH_COMMAND=repowolf-git-ssh`. Do not put token values in Git remotes, URLs, arguments, repository files, or logs.

## Supervision

Run the service under systemd, Home Manager, Kubernetes, or another supervisor that restarts it after startup-only inputs change.

A system service can use a root-owned environment file:

```ini
[Unit]
Description=RepoWolf access broker
After=network-online.target
Wants=network-online.target

[Service]
User=repowolf
Group=repowolf
EnvironmentFile=/run/repowolf/service.env
ExecStart=/usr/local/bin/repowolf serve --config /etc/repowolf/repowolf.yaml
Restart=on-failure
PrivateTmp=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

Protect `service.env`, provider credentials, and TLS keys from untrusted users. A Home Manager deployment can define the equivalent user unit with a Nix store path:

```nix
systemd.user.services.repowolf = {
  Unit.Description = "RepoWolf access broker";
  Service = {
    EnvironmentFile = "%t/repowolf/service.env";
    ExecStart = "${inputs.repowolf.packages.${pkgs.system}.repowolf}/bin/repowolf serve --config %h/.config/repowolf/config.yaml";
    Restart = "on-failure";
  };
  Install.WantedBy = [ "default.target" ];
};
```

Keep the environment file outside the Nix store because it contains secret values.

## Kubernetes

Mount strict policy YAML from a ConfigMap, and mount the server certificate and key from a Secret. Expose each service token using a Secret-backed environment variable whose name exactly matches the policy's `tokenEnvs` entry. Provider tokens, provider configuration, and SSH keys are separate service-only Secrets.

Run the published image with `runAsUser: 65532`, `runAsGroup: 65532`, a writable temporary volume at `/tmp`, and `repowolf serve --config /etc/repowolf/repowolf.yaml`. A Service exposes port 8443. Agent pods receive only the client binary, their role's `REPOWOLF_TOKEN`, `REPOWOLF_ENDPOINT`, and a public CA mounted from a ConfigMap or Secret; they never mount service policy, provider credentials, or TLS private keys.
