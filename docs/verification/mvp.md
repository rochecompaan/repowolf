# RepoWolf MVP release-candidate evidence

## Scope and evidence boundary

Local evidence was collected on 2026-08-04 on Linux amd64. The initial Task 15 evidence came from the staged tree over `b99695c64c5ea0b8d54cb2b9eb59b6a02c495b71`; it is not archive evidence for a later commit. The Release archives section records the exact clean source revision and tree that produced its archives. GoReleaser snapshot archives embed the source short commit in their version, so a later documentation-only commit is not represented as the producer of those archive checksums.

This document does not invent external evidence. PR #1 on `rochecompaan/repowolf` has successful GitHub CI evidence on head `66ad76def6e7acf0fd5c34a77b13e0baf7d45e91` at `https://github.com/rochecompaan/repowolf/actions/runs/30907343278`; no release tag or GHCR publication was attempted and PR #1 includes successful native amd64 and arm64 release-smoke jobs recorded below.

## Real Bubblewrap jail

### TDD RED

The first focused run failed because the Nix check attribute did not exist:

```sh
go test -tags=integration ./integration -run TestBubblewrap -count=1
```

Result: exit 1. `TestBubblewrapClientClosureSupportsRestrictedGHAndRealGit` reported that `checks.x86_64-linux.bubblewrap.testConfig` was absent. This established the missing Nix/Bubblewrap wiring before `nix/bubblewrap-check.nix` and the `nix/checks.nix` check were added.

A second focused RED cycle tightened the stdin requirement to a real TTY. With a plain reader still connected, the jail exited 31 with `stdin is not a TTY`. The launcher was then changed to open a pseudoterminal, explicitly attach its slave to the asynchronously started Bubblewrap process, and write the marker through the master.

### GREEN and behavior proof

```sh
go test -tags=integration ./integration -run TestBubblewrap -count=1
go test -tags=integration ./integration -count=1
```

Results: exit 0; focused package time 2.776s on the first recorded GREEN rerun, and full tagged integration package time 12.055s. After the TTY tightening, the focused test again passed (48.248s including a fresh Nix derivation rebuild).

The test starts a real TLS RepoWolf service outside the jail and launches real Bubblewrap with:

- `--unshare-all --share-net --die-with-parent`;
- an empty synthetic `/home/jail` and `--clearenv`;
- only `REPOWOLF_ENDPOINT`, `REPOWOLF_TOKEN`, `REPOWOLF_CA_FILE`, and `GIT_SSH_COMMAND` passed by Bubblewrap;
- selected Nix store paths mounted read-only;
- `/proc`, `/dev`, isolated tmpfs `/tmp`, and the shared host network namespace;
- a writable disposable checkout and a read-only generated CA;
- an explicitly forwarded pseudoterminal stdin descriptor on the asynchronous process.

Inside the jail, `jail-command.sh` proves stdin remains a TTY and receives a unique marker. It runs restricted `gh issue list`, a real `git fetch --no-write-fetch-head`, an allowed non-main push, and an offline denied exact-main push. The outer assertions prove:

- the fetched upload-pack channel received real Git client bytes;
- the allowed remote feature ref equals the candidate commit;
- exact-main denial left the fake SSH receive input at zero bytes;
- local checkout status, refs, and HEAD are byte-identical before and after;
- audit records have the expected accepted/completed/denied sequence and leak-safe schema;
- client output does not contain token, provider credential/environment, or provider stderr markers;
- provider auth remains service-side while RepoWolf controls are removed before provider execution;
- the service, Bubblewrap, provider, and client processes are fully reaped.

The shell also rejects any unexpected inherited environment, nonempty home, visible host `gh`/`ssh`, provider configuration, SSH keys, `SSH_AUTH_SOCK`, server binary, service token environment, or host path supplied by the test.

## Nix closure and flake evidence

```sh
nix flake check --accept-flake-config --print-build-logs
```

Result: exit 0, `all checks passed!`. Ten x86_64-linux checks ran or were confirmed from the store, including `checks.x86_64-linux.bubblewrap`, split packages, client closure, and OCI image. Nix reported aarch64-linux as an incompatible omitted system on this amd64 host.

The Bubblewrap check is a `buildGoModule` integration check. Its inner jail mounts the runtime closure rooted only in `repowolf-client`, `gitMinimal`, and Bash; Bubblewrap, Go, procps, the test service, fake provider tools, and the source tree remain outside that closure. No RepoWolf service package, GitHub CLI package, or OpenSSH package appeared in the selected closure.

Inspection command:

```sh
config="$(nix eval --json .#checks.x86_64-linux.bubblewrap.testConfig)"
closure_file="$(printf '%s' "$config" | jq -r .closureFile)"
client="$(printf '%s' "$config" | jq -r .clientRoot)"
cat "$closure_file"
! grep -E -- '(-repowolf-dev|github-cli|-openssh-)' "$closure_file"
ls -l "$client/bin"
nix-store -qR "$client"
```

Result: exit 0. Client links were `gh -> repowolf-client` and `repowolf-git-ssh -> repowolf-client`. The client runtime closure contained only the RepoWolf client package and its `mailcap`, `tzdata`, and `iana-etc` runtime data; it contained no service, `gh`, OpenSSH, policy, or private-key package. Nix store hashes intentionally are not recorded because they change with the staged source; the package identities and absence checks are authoritative. Existing `client-closure` also scans for service policy and private-key filenames.

## Go, Protobuf, and generated output

```sh
go tool buf lint
scripts/check-generated.sh
gofmt -l .
go vet ./...
go test ./... -count=1
go test -race ./... -count=1
go test -tags=integration ./integration -count=1
```

Results: all exited 0. `gofmt -l .` produced no output. The ordinary full suite and race suite passed across every package; generated Protobuf output was reproducible. The race suite's slowest package was `internal/runner` at 17.009s. The tagged integration suite separately exercised the real Bubblewrap test.

## Release archives

```sh
scripts/check-release.sh
(cd dist && sha256sum -c checksums.txt)
for archive in dist/repowolf_linux_amd64.tar.gz dist/repowolf_linux_arm64.tar.gz; do
  tar -tzf "$archive"
done
```

Result: `scripts/check-release.sh` exited 0. GoReleaser built service and client binaries for Linux amd64 and arm64, created both paired archives, executed native amd64 `repowolf --version`, and observed the expected usage exit from native `repowolf-client`. Both checksum validations returned `OK`. Each archive contains `README.md`, `repowolf`, and `repowolf-client`.

The earlier staged-tree checksum values are superseded. The following snapshot archives were rebuilt from a clean checkout at source revision `c053a5f41b207ee3ca107fc4e8a61d631fcb4d0a` and source tree `50ad1512a723557e5e502a2c2acdaffb159ac1f3` (before this evidence update was committed):

```text
0861517c490b12a1db6896b70f9e2bfb075e9ab611f9882766d9dd8b4dd77e5e  repowolf_linux_amd64.tar.gz
35e8c247d003f849c302f7b3f1d957fd791df3d7ecbda593864204f22536ed83  repowolf_linux_arm64.tar.gz
```

`scripts/check-release.sh` invoked `goreleaser release --snapshot --clean`, whose generated `dist/metadata.json` identifies commit `c053a5f41b207ee3ca107fc4e8a61d631fcb4d0a` and version `0.0.0-SNAPSHOT-c053a5f`. `(cd dist && sha256sum -c checksums.txt)` reported both archive names `OK`. This evidence update necessarily creates a later documentation revision; it does **not** claim that these checksums belong to that later `HEAD`. Rebuilding from that later revision would use a different snapshot short commit and must record its own resulting checksums.

Binary-format inspection used `file` from pinned nixpkgs because the host shell had no `file` executable:

```sh
nix shell --inputs-from . nixpkgs#file -c file <extracted binaries>
```

Result: both amd64 binaries were statically linked x86-64 ELF and both arm64 binaries were statically linked ARM aarch64 ELF. Arm64 was not executed on this amd64 host.

## OCI smoke

```sh
image="$(nix build .#ociImage --no-link --print-out-paths)"
docker load -i "$image"
docker run --rm repowolf:mvp --version
```

Result: exit 0. Nix produced the OCI archive and Docker loaded `repowolf:mvp`; the container printed `dev`. Task 14's layer inspection remains applicable: the service image intentionally contains service-side `gh`, OpenSSH, and CA roots, but no policy, token, provider credential/configuration, SSH key, or TLS private key.

## Migration and source-anchor verification

The later migration handoff is `docs/migration/roche-pi.md`. It records the trusted-parent certificate contract, standalone parity/leak gates, staged Home Manager and jail cutover, exact rollback, retained embedded broker, and separate later removal.

External source reads were limited to the immutable baseline:

```sh
ROCHE_PI_SOURCE=/home/roche/projects/pi/roche-pi
PIN=5b8425c8663a4c7bc7c79a7188743ec464eaba02
for path in \
  nix/lib/mk-jailed-pi.nix \
  nix/lib/jailed-github-broker-lifecycle.nix \
  nix/packages/jailed-github-broker.nix \
  modules/checks/jailed-github-broker-real-jail.nix
do
  git -C "$ROCHE_PI_SOURCE" cat-file -e "$PIN:$path"
  git -C "$ROCHE_PI_SOURCE" show "$PIN:$path" >/dev/null
done
```

Result: exit 0 for all four paths. No external repository was modified. The handoff explicitly treats clubhouse `devenv.nix` as an untracked operator-supplied input whose location and content must be supplied to the later plan, never as recoverable Git history.

Documentation and static Nix received direct verification rather than tests that restate prose/configuration:

```sh
bash -n integration/testdata/jail-command.sh
git diff --check
git diff --cached --check
nix eval .#checks.x86_64-linux.bubblewrap.name --raw
```

Results: exit 0; Nix printed `repowolf-bubblewrap-check-dev` and the diff checks reported no whitespace errors.

## External residuals and readiness

Task 14 residuals remain accurate:

- The local host is Linux amd64. Arm64 archives were built, checksummed, structurally inspected, and identified as AArch64 ELF, but not executed natively.
- `nix flake check` omitted incompatible aarch64-linux outputs on this x86_64 host.
- There is no GitHub release tag or GHCR publication.
- PR-level run evidence is available for `docs/mvp-design`: `https://github.com/rochecompaan/repowolf/actions/runs/30907343278` with head `66ad76def6e7acf0fd5c34a77b13e0baf7d45e91`, conclusion `success`; `test` job (`https://github.com/rochecompaan/repowolf/actions/runs/30907343278/job/91985423960`, conclusion `success`) includes lint/generated/format/vet, race tests, Bubblewrap user-namespace bootstrap, full Nix flake, and OCI smoke. Native `release-smoke (ubuntu-24.04-arm, arm64)` success is at `https://github.com/rochecompaan/repowolf/actions/runs/30907343278/job/91986197596` and native `release-smoke (ubuntu-24.04, amd64)` success is at `https://github.com/rochecompaan/repowolf/actions/runs/30907343278/job/91986197616`.
- Local host is Linux amd64; native aarch64 archive execution remains unavailable and aarch64 Linux checks are omitted by local `nix flake check` as incompatible.
- A fresh adversarial review against `docs/specs/2026-08-01-repowolf-mvp-design.md` is still required before declaring migration/publication readiness.
- The current embedded broker was read only at its pinned source baseline and remains untouched and available for rollback.
