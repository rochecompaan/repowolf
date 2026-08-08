# RepoWolf Docker example

This example keeps provider credentials and real provider tools on the broker
side. The sandbox gets only `repowolf-client` (as `gh` and
`repowolf-git-ssh`), its RepoWolf token, endpoint, and public CA.

Two run modes share the same sandbox image:

- **Host broker + sandbox container** — the production topology.
- **Compose broker + sandbox container** — no host RepoWolf install.

## Requirements and platform scope

- Docker with the Compose v2 plugin.
- Bash (the scripts are Bash 3.2-compatible).
- Linux is tested. The sandbox release build supports `linux/amd64` and
  `linux/arm64`. On macOS run from a Unix shell. On Windows run from WSL2;
  native PowerShell/cmd is not supported by this example.

## Build the sandbox

```sh
docker build -t repowolf-sandbox:local examples/docker/sandbox
```

The build downloads `v0.1.0` by default and verifies the selected archive
against the published checksum file. Override with
`--build-arg REPOWOLF_VERSION=<tag>` when upgrading. Release archives are
checksum-verified; they are not signed.

The runtime image has git and CA roots, runs as UID/GID 65532, and contains
no real `gh`, OpenSSH, provider token, key, or agent socket.

## Path A: host broker + sandbox

Install RepoWolf on the Linux host per the top-level README. The following
creates the matching certificate state and complete service configuration. It
binds only the Docker bridge gateway (usually `172.17.0.1`), not `0.0.0.0`,
and resolves the service-side tools to absolute paths:

```sh
GH_PATH="$(command -v gh)"
SSH_PATH="$(command -v ssh)"
case "$GH_PATH:$SSH_PATH" in
  /*:/*) ;;
  *) echo "gh and ssh must resolve to absolute paths" >&2; exit 1 ;;
esac

sudo install -d -o root -g repowolf -m 0750 /var/lib/repowolf /etc/repowolf
sudo repowolf cert init --output /var/lib/repowolf/tls \
  --dns repowolf.internal --ip 127.0.0.1
# The broker identity must traverse these directories and read the certificate
# and server key. The private key remains restricted to root:repowolf.
sudo chown root:repowolf /var/lib/repowolf /var/lib/repowolf/tls \
  /var/lib/repowolf/tls/tls.crt /var/lib/repowolf/tls/tls.key
sudo chmod 0750 /var/lib/repowolf /var/lib/repowolf/tls
sudo chmod 0640 /var/lib/repowolf/tls/tls.crt /var/lib/repowolf/tls/tls.key
sudo chown root:root /var/lib/repowolf/tls/ca.key
sudo chmod 0600 /var/lib/repowolf/tls/ca.key

sudo tee /etc/repowolf/repowolf.yaml >/dev/null <<EOF
apiVersion: repowolf.dev/v1alpha1
listen: "172.17.0.1:9443"

tls:
  certificate: /var/lib/repowolf/tls/tls.crt
  privateKey: /var/lib/repowolf/tls/tls.key

tools:
  gh: $GH_PATH
  ssh: $SSH_PATH

providers:
  github-public:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git

repositories:
  example:
    provider: github-public
    owner: rochecompaan
    name: repowolf
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
EOF
sudo chown root:repowolf /etc/repowolf/repowolf.yaml
sudo chmod 0640 /etc/repowolf/repowolf.yaml
sudo -u repowolf repowolf config validate --config /etc/repowolf/repowolf.yaml
```

This assumes the broker runs as `repowolf` (or an identity in group
`repowolf`). Do not loosen the private-key mode or give the sandbox the key.

The example policy principal is `example-agent`, so the broker must load
`REPOWOLF_TOKEN_EXAMPLE_AGENT` from a dedicated principal environment file,
without overwriting provider settings in `/run/repowolf/service.env`. Generate and
store it once in a protected file:

```sh
umask 077
mkdir -p /run/repowolf
chmod 0700 /run/repowolf
BROKER_TOKEN="$(repowolf token generate)"
printf '%s\n' "$BROKER_TOKEN" > /var/lib/repowolf/token
printf 'REPOWOLF_TOKEN_EXAMPLE_AGENT=%s\n' "$BROKER_TOKEN" > /run/repowolf/example-agent.env
chmod 0600 /var/lib/repowolf/token /run/repowolf/example-agent.env
```

Both `/run/repowolf/service.env` and `/run/repowolf/example-agent.env` must be
loaded by the broker before restart (for example with systemd
`EnvironmentFile=/run/repowolf/service.env` and
`EnvironmentFile=/run/repowolf/example-agent.env`). Do not print its contents.

The absolute paths avoid startup failures from ambiguous/empty PATH entries.
Restart the broker, then:

```sh
docker run --rm -it \
  -e REPOWOLF_ENDPOINT=https://172.17.0.1:9443 \
  -e REPOWOLF_SERVER_NAME=repowolf.internal \
  -e REPOWOLF_TOKEN="$(cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

`REPOWOLF_SERVER_NAME` must match the host certificate's DNS SAN. Use
`--add-host=host.docker.internal:host-gateway` and endpoint
`https://host.docker.internal:9443` as an alternative to the numeric gateway.

Git operations additionally require the **host broker** to have an SSH
identity/agent and verified known-hosts state. GitHub requires authentication
even for public SSH clones.

## Path B: compose broker + sandbox

```sh
cd examples/docker
cp .env.example .env
# Set GH_TOKEN in .env (obtain on the host with: gh auth token)
REPOWOLF_REPO=rochecompaan/repowolf ./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo rochecompaan/repowolf
```

`bootstrap.sh` writes fixed paths `state/` and `.env`, because those are the
paths Compose reads. It refuses existing state. The wait is required:
`docker compose up -d` and `depends_on` do not signal TLS readiness.

### Policy-denial demonstration

The sample policy deliberately omits `actions:read`:

```sh
docker compose run --rm sandbox gh run list --repo rochecompaan/repowolf
# gh: GitHub operation failed
docker compose logs repowolf > /tmp/repowolf-broker.log
grep -E '"outcome":[[:space:]]*"denied"' /tmp/repowolf-broker.log
```

Client diagnostics are intentionally identical for policy/provider failures;
the broker audit log is the stable source of the outcome. Add
`- actions:read` to `state/config.yaml` and restart the broker to allow the
operation.

### Enable Git in compose

Every Git read/write requires broker-side authentication and host
verification. Prepare a repository-scoped deploy key and a verified
known-hosts file. Verify GitHub's published SSH fingerprints before trusting
`ssh-keyscan` output:

<https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints>

Bootstrap from a fresh state directory:

```sh
ssh-keyscan -t ed25519 github.com > /tmp/github-known-hosts
REPOWOLF_REPO=rochecompaan/repowolf \
REPOWOLF_SSH_KEY=/secure/path/to/deploy-key \
REPOWOLF_KNOWN_HOSTS=/tmp/github-known-hosts \
./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox \
  git ls-remote git@github.com:rochecompaan/repowolf.git
```

The private key/known-hosts/config are mounted only into the broker. The
sandbox still contains no SSH identity or OpenSSH client.

## Boundary proof

```sh
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

## Reset and secret handling

`.gitignore` prevents ordinary accidental adds of `.env` and `state/`, but
`git add -f` bypasses it. Treat both paths as secret material.

Deleting `state/` destroys the CA private key, server key, principal token,
and optional SSH key; deleting `.env` may destroy the only local copy of the
provider token. Back them up outside the repository before reset:

```sh
docker compose down
backup="${HOME}/.local/state/repowolf-example/$(date +%Y%m%d%H%M%S)"
mkdir -p "$backup"
mv state .env "$backup/"
```

Only use `rm -rf state .env` when that destruction is intended.

## Troubleshooting

- **Connection refused after compose start:** run `./wait-for-broker.sh`; if it
  times out, inspect `docker compose logs repowolf`.
- **TLS name mismatch:** set `REPOWOLF_SERVER_NAME` to the cert DNS SAN.
- **Host broker cannot be reached:** it is still bound to `127.0.0.1`; bind
  the Docker bridge gateway instead.
- **Broker `service failed`:** check TLS ownership/modes and pin absolute
  `tools.gh`/`tools.ssh` paths.
- **Git host-key/authentication failure:** supply both a verified known-hosts
  file and a usable broker-side key/agent. Read-only Git is not anonymous.
