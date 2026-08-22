# RepoWolf Docker example

This example keeps provider credentials and real provider tools on the broker
side. The sandbox gets only `repowolf-client` (as `gh` and
`repowolf-git-ssh`), its RepoWolf token, endpoint, and public CA.

Two run modes share the same sandbox image:

- **Compose broker + sandbox container** — no host RepoWolf install.
- **Host broker + sandbox container** — the production topology.

## Requirements and platform scope

You need Git, Bash, Docker, and the Compose v2 plugin.

| Host | Docker environment | Command environment |
| --- | --- | --- |
| Linux amd64 or arm64 | Docker Engine | Bash |
| macOS | Docker Desktop | Terminal |
| Windows 11 | Docker Desktop with WSL2 integration | WSL2 Bash |

Native PowerShell and cmd are not supported. The native host-broker path later
in this guide requires Linux.

## Quickstart: Compose broker and sandbox

Run these commands from the repository's `examples/docker` directory.

```bash
cp .env.example .env
# Set GH_TOKEN in .env. Leave REPOWOLF_TOKEN_AGENT empty.
export REPOWOLF_REPO=rochecompaan/repowolf
./bootstrap.sh
docker compose build sandbox
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo "$REPOWOLF_REPO"
```

The build downloads `v0.1.0` by default. It makes sure that the selected
archive matches the published checksum file. Use
`--build-arg REPOWOLF_VERSION=<tag>` to select another release. Release
archives have checksum verification. They are not signed.

`bootstrap.sh` writes fixed paths `state/` and `.env`, because Compose reads
these paths. It refuses existing state. The wait is required: `docker compose
up -d` and `depends_on` do not signal TLS readiness.

The runtime image has Git and CA roots. It runs as UID/GID 65532. It contains
no real `gh`, OpenSSH, provider token, key, or agent socket.

## Boundary proof

The sandbox still receives a RepoWolf token and public CA. It does not receive
a GitHub token, SSH key, SSH client, or agent socket.

```sh
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

### Policy-denial demonstration

The sample policy deliberately omits `actions:read`:

```sh
docker compose run --rm sandbox gh run list --repo rochecompaan/repowolf
# gh: GitHub operation failed
docker compose logs repowolf > /tmp/repowolf-broker.log
grep -E '"outcome":[[:space:]]*"denied"' /tmp/repowolf-broker.log
```

Client diagnostics are intentionally identical for policy/provider failures.
The broker audit log is the stable source of the outcome. Add
`- actions:read` to `state/config.yaml` and restart the broker to allow the
operation.

### Enable Git in compose

Every Git read/write requires broker-side authentication and host
verification. Prepare a repository-scoped deploy key and a verified
known-hosts file. Before you trust `ssh-keyscan` output, make sure that it
matches GitHub's published SSH fingerprints:

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

The private key, known-hosts file, and config go only to the broker.
The sandbox still contains no SSH identity or OpenSSH client.

## Native Linux broker and sandbox container

Choose this path when the broker host runs Linux and a service manager controls
RepoWolf. macOS and Windows users use the Compose broker path instead.

Install RepoWolf on the Linux host per the top-level README. Run the setup as a
normal sudo-capable operator. The scripts use `sudo` only for protected
filesystem changes and refuse existing state.

```sh
cd examples/docker
REPOWOLF_REPO=rochecompaan/repowolf ./install-host-broker.sh
./install-host-principal.sh
```

Override `REPOWOLF_REPO` to select the repository. Override `REPOWOLF_LISTEN`
to select the bind address. Override `REPOWOLF_GH_PATH` and
`REPOWOLF_SSH_PATH` to select the broker tools. Override
`REPOWOLF_BROKER_USER` and `REPOWOLF_BROKER_GROUP` to select the broker
identity. Override `REPOWOLF_CONFIG_DIR`, `REPOWOLF_STATE_DIR`, and
`REPOWOLF_RUNTIME_DIR` to select installation directories. Tool and directory
overrides must be absolute paths. `REPOWOLF_LISTEN` defaults to the Docker
bridge gateway on port `9443`. If the bridge is missing, set an explicit listen
override. Neither script starts or restarts the broker.

Both `/run/repowolf/service.env` and `/run/repowolf/example-agent.env` must be
loaded by the broker before restart. For example, use systemd
`EnvironmentFile=/run/repowolf/service.env` and
`EnvironmentFile=/run/repowolf/example-agent.env`. Do not print their
contents.

Run the client against the detected bridge gateway:

```bash
DOCKER_GATEWAY="$(docker network inspect \
  --format '{{(index .IPAM.Config 0).Gateway}}' bridge)"
docker run --rm -it \
  -e REPOWOLF_ENDPOINT="https://$DOCKER_GATEWAY:9443" \
  -e REPOWOLF_SERVER_NAME=repowolf.internal \
  -e REPOWOLF_TOKEN="$(sudo cat /var/lib/repowolf/token)" \
  -e REPOWOLF_CA_FILE=/run/repowolf/ca.crt \
  -v /var/lib/repowolf/tls/ca.crt:/run/repowolf/ca.crt:ro \
  repowolf-sandbox:local gh repo view --repo rochecompaan/repowolf
```

A custom `REPOWOLF_LISTEN` requires the reachable matching host and port in
`REPOWOLF_ENDPOINT`. `REPOWOLF_SERVER_NAME` must match the host certificate's
DNS SAN. Git operations also require the **host broker** to have an SSH
identity or agent and verified known-hosts state. GitHub requires
authentication even for public SSH clones.

## Reset and secret handling

For Compose, `.gitignore` prevents ordinary accidental adds of `.env` and
`state/`, but `git add -f` bypasses it. Treat both paths as secret material.

Deleting Compose `state/` erases the CA private key, server key, principal
token, and optional SSH key. Deleting Compose `.env` can erase the only local
copy of the provider token. Back them up outside the repository before reset:

```sh
docker compose down
backup="${HOME}/.local/state/repowolf-example/$(date +%Y%m%d%H%M%S)"
mkdir -p "$backup"
mv state .env "$backup/"
```

Only use `rm -rf state .env` when that Compose-only destruction is intended.

For the host installers, rerunning an installer requires a backup. Then,
deliberately remove the exact conflicting path: the TLS directory
`$REPOWOLF_STATE_DIR/tls`, policy `$REPOWOLF_CONFIG_DIR/repowolf.yaml`, token
`$REPOWOLF_STATE_DIR/token`, or principal environment
`$REPOWOLF_RUNTIME_DIR/example-agent.env`. Do not use a wildcard or automatic
host reset command.

## Troubleshooting

- **Connection refused after compose start:** Run `./wait-for-broker.sh`. If it
  times out, inspect `docker compose logs repowolf`.
- **Missing Docker bridge:** Set an explicit `REPOWOLF_LISTEN`. The host
  installer cannot infer its default address without the bridge.
- **Invalid host override:** `REPOWOLF_GH_PATH`, `REPOWOLF_SSH_PATH`, and the
  directory overrides must be absolute paths.
- **Existing protected host state:** Back up and deliberately remove only the
  exact conflicting TLS, policy, token, or principal environment path before
  rerunning its installer.
- **Broker does not receive the principal token:** Make sure that both
  `/run/repowolf/service.env` and `/run/repowolf/example-agent.env` load before
  restarting the broker.
- **TLS name mismatch:** Set `REPOWOLF_SERVER_NAME` to the cert DNS SAN.
- **Git host-key/authentication failure:** Supply both a verified known-hosts
  file and a usable broker-side key or agent. Read-only Git is not anonymous.
