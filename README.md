<p align="center">
  <picture>
    <img alt="RepoWolf logo" src="docs/assets/repowolf-logo.png" width="500">
  </picture>
</p>

[![CI](https://github.com/rochecompaan/repowolf/actions/workflows/ci.yml/badge.svg)](https://github.com/rochecompaan/repowolf/actions/workflows/ci.yml)

# RepoWolf

RepoWolf lets an AI coding agent use GitHub without putting GitHub credentials
or SSH keys in the agent sandbox. A broker that you control holds the provider
credentials and enforces repository policy. The sandbox receives only the
RepoWolf client, a scoped RepoWolf token, and the public CA certificate.

RepoWolf does not create, inspect, register, or attest sandboxes.

## How it works

```text
Agent sandbox                  RepoWolf broker                    GitHub
--------------                 ---------------                    ------
gh / repowolf-git-ssh --TLS--> repository policy --provider auth--> API / Git
RepoWolf token + CA            audit JSON Lines
no GitHub token or SSH key
```

- RepoWolf enforces explicit capabilities for each principal and repository.
- The `gh` compatibility client restricts GitHub operations.
- `repowolf-git-ssh` provides Git over SSH.
- RepoWolf protects refs and deletes. It limits ref updates.
- RepoWolf loads strict configuration at startup.
- It writes JSON Lines audit output.

## Choose your setup

| Host | Recommended setup | Notes |
| --- | --- | --- |
| Linux | Docker for the quickest start. Native for a long-running broker. | Native packages support amd64 and arm64 |
| macOS | Docker Desktop from Terminal | No native RepoWolf package |
| Windows | Docker Desktop through WSL2 | Run commands inside WSL2; PowerShell and cmd are not supported |

Docker Compose is the recommended first setup because it needs no host RepoWolf installation.

## Docker Compose quickstart

You need Git, Bash, Docker with Compose v2, and a GitHub token. Use a GitHub
token that can read the repository. Replace `rochecompaan/repowolf` with a
repository that this token can read.

```bash
git clone https://github.com/rochecompaan/repowolf.git
cd repowolf/examples/docker
docker compose build sandbox
cp .env.example .env
# Edit .env. Set GH_TOKEN and leave REPOWOLF_TOKEN_AGENT empty.
export REPOWOLF_REPO=rochecompaan/repowolf
./bootstrap.sh
docker compose up -d repowolf
./wait-for-broker.sh 127.0.0.1 8443 30
docker compose run --rm sandbox gh repo view --repo "$REPOWOLF_REPO"
```

The last command is your first brokered GitHub request.

Run this command to prove the sandbox boundary:

```sh
docker compose run --rm --entrypoint sh sandbox -c '
  test -z "${GH_TOKEN+x}"
  test "$(readlink /usr/local/bin/gh)" = "repowolf-client"
  test "$(readlink /usr/local/bin/repowolf-git-ssh)" = "repowolf-client"
  ! command -v ssh
'
```

The sandbox contains a RepoWolf token and the public CA. It does not contain
the GitHub token, an SSH key, OpenSSH, or an SSH agent socket.

Read the [Docker guide](examples/docker/README.md) for policy denials and
troubleshooting.

## Enable Git access

GitHub SSH needs a broker-side key and verified host fingerprints. Keep Git SSH
as a later step after the GitHub request succeeds.

[Enable Git in the Docker guide](examples/docker/README.md#enable-git-in-compose)

## Native Linux

If the broker runs for a long time and a service manager controls it, use
native Linux. This path has less Docker overhead. Native packages support amd64
and arm64.

- [Install RepoWolf](docs/installation.md)
- [Configure policy, tokens, and TLS](docs/configuration.md)
- [Deploy and supervise the broker](docs/deployment.md)

## Learn more

- [Complete Docker walkthrough](examples/docker/README.md)
- [Configuration reference](docs/configuration.md)
- [Deployment options](docs/deployment.md)
- [Approved MVP design](docs/specs/2026-08-01-repowolf-mvp-design.md)
