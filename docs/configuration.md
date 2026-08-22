# Configuration

RepoWolf loads one strict YAML configuration at startup. A configuration
change requires a service restart.

## Policy model

A provider defines the GitHub API and Git hosts. A repository maps one policy
name to an owner, repository name, provider, and Git limits. A principal names
one or more token environment variables. A grant gives that principal an
explicit capability set for one repository.

## Tokens and TLS

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

After configuration, [select a deployment](deployment.md).
