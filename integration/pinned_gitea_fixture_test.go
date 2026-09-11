package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

type pinnedGiteaGitFixture struct {
	gitea                    *testutil.Gitea
	server                   *testutil.Server
	root, gitPath, checkout  string
	capture, receiveCapture  string
	blockReceive, receivePID string
	seedCommit               string
	gitEnv                   []string
}

func newPinnedGiteaGitFixture(t *testing.T) *pinnedGiteaGitFixture {
	t.Helper()
	gitea := testutil.StartGitea(t)
	seedCommit := gitea.Seed(t, "seed.txt", "seeded through operator setup\n")
	access := gitea.StartSSHAccess(t)
	root := t.TempDir()
	binaries := testutil.BuildBinaries(t, filepath.Join(root, "bin"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	sshPath := mustExecutable(t, "ssh")
	teePath := mustExecutable(t, "tee")
	sleepPath := mustExecutable(t, "sleep")
	capture := filepath.Join(root, "ssh.capture")
	receiveCapture := filepath.Join(root, "receive.capture")
	blockReceive := filepath.Join(root, "block-receive")
	receivePID := filepath.Join(root, "receive.pid")
	wrapper := filepath.Join(root, "ssh-wrapper")
	wrapperScript := fmt.Sprintf(`#!/bin/sh
set -eu
{
  printf 'BEGIN\n'
  printf 'HOME=%%s\n' "${HOME-unset}"
  printf 'SSH_AUTH_SOCK=%%s\n' "${SSH_AUTH_SOCK-unset}"
  printf 'REPOWOLF_TOKEN_AGENT=%%s\n' "${REPOWOLF_TOKEN_AGENT-unset}"
  printf 'REPOWOLF_TOKEN_GITEA=%%s\n' "${REPOWOLF_TOKEN_GITEA-unset}"
  printf 'GH_TOKEN=%%s\n' "${GH_TOKEN-unset}"
  printf 'GITHUB_TOKEN=%%s\n' "${GITHUB_TOKEN-unset}"
  printf '%%s\n' "$@"
  printf 'END\n'
} >> "${SSH_CAPTURE_PATH:?}"
remote=''
for argument in "$@"; do remote=$argument; done
case "$remote" in
  "git-receive-pack "*)
    : > "${SSH_RECEIVE_CAPTURE:?}"
    if [ -e "${SSH_BLOCK_RECEIVE:?}" ]; then
      printf '%%s\n' "$$" > "${SSH_RECEIVE_PID:?}"
      exec %q 30
    fi
    %q "${SSH_RECEIVE_CAPTURE:?}" | %q -i %q -o IdentitiesOnly=yes -o UserKnownHostsFile=%q -o StrictHostKeyChecking=yes "$@"
    ;;
  *) exec %q -i %q -o IdentitiesOnly=yes -o UserKnownHostsFile=%q -o StrictHostKeyChecking=yes "$@" ;;
esac
`, sleepPath, teePath, sshPath, filepath.Join(access.Home, ".ssh", "id_ed25519"), filepath.Join(access.Home, ".ssh", "known_hosts"), sshPath, filepath.Join(access.Home, ".ssh", "id_ed25519"), filepath.Join(access.Home, ".ssh", "known_hosts"))
	if err := os.WriteFile(wrapper, []byte(wrapperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.yaml")
	policy := fmt.Sprintf(`apiVersion: repowolf.dev/v1alpha1
listen: __LISTEN__
tls:
  certificate: __CERTIFICATE__
  privateKey: __PRIVATE_KEY__
tools:
  ssh: __SSH__
providers:
  gitea:
    kind: gitea
    apiHost: gitea.example.invalid
    gitHost: %s
    sshUser: %s
    sshPort: %d
    tokenEnv: REPOWOLF_TOKEN_GITEA
repositories:
  pinned-gitea:
    provider: gitea
    owner: %s
    name: %s
    git:
      denyRefs:
        - refs/heads/denied
      denyDeletes: true
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs:
      - REPOWOLF_TOKEN_AGENT
    grants:
      - repository: pinned-gitea
        capabilities:
          - git:read
          - git:write
limits:
  maxConcurrentRequests: 16
  maxConcurrentRequestsPerPrincipal: 8
  maxMessageBytes: 1048576
  maxStreamChunkBytes: 65536
  maxPushPrefixBytes: 1048576
  maxGitBytesPerDirection: 1073741824
  initialStreamTimeout: 5s
  operationTimeout: 30s
  idleStreamTimeout: 5s
`, gitea.SSHHost, gitea.SSHUser, gitea.SSHPort, gitea.Owner, gitea.Repository)
	if err := os.WriteFile(policyPath, []byte(policy), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testutil.StartServer(t, testutil.ServerOptions{
		Binary: binaries.Service, PolicyPath: policyPath, Certificate: certificate, SSHPath: wrapper,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + pinnedGiteaPrincipalMarker,
			"REPOWOLF_TOKEN_GITEA=" + pinnedGiteaProviderMarker,
			"HOME=" + access.Home, "SSH_AUTH_SOCK=" + access.AgentSocket,
			"SSH_CAPTURE_PATH=" + capture, "SSH_RECEIVE_CAPTURE=" + receiveCapture,
			"SSH_BLOCK_RECEIVE=" + blockReceive, "SSH_RECEIVE_PID=" + receivePID, "GIT_PROTOCOL=version=2",
		},
	})
	gitPath := mustExecutable(t, "git")
	execPath, err := exec.Command(gitPath, "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	gitEnv := isolatedGitEnvironment(root, gitPath, strings.TrimSpace(string(execPath)))
	gitEnv = testutil.Environment(gitEnv,
		"GIT_SSH_COMMAND="+binaries.GitSSH,
		"REPOWOLF_ENDPOINT="+server.Endpoint, "REPOWOLF_CA_FILE="+server.Certificate.CAFile,
		"REPOWOLF_SERVER_NAME="+server.Certificate.ServerName, "REPOWOLF_TOKEN="+pinnedGiteaPrincipalMarker,
	)
	return &pinnedGiteaGitFixture{
		gitea: gitea, server: server, root: root, gitPath: gitPath, checkout: filepath.Join(root, "checkout"),
		capture: capture, receiveCapture: receiveCapture, blockReceive: blockReceive, receivePID: receivePID,
		seedCommit: seedCommit, gitEnv: gitEnv,
	}
}

func mustExecutable(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func (fixture *pinnedGiteaGitFixture) cloneAndFetch(t *testing.T) (pinnedGitResult, pinnedGitResult) {
	t.Helper()
	remote := "ssh://" + fixture.gitea.SSHUser + "@" + fixture.gitea.SSHHost + ":" + strconv.Itoa(fixture.gitea.SSHPort) + "/team_name/repo.one.git"
	clone := runPinnedGit(fixture.gitPath, fixture.root, fixture.gitEnv, "clone", remote, fixture.checkout)
	if clone.err != nil {
		t.Fatalf("pinned Gitea clone: %v; stdoutBytes=%d stderrBytes=%d captureBytes=%d auditBytes=%d brokerStderrBytes=%d", clone.err, len(clone.stdout), len(clone.stderr), fileSize(fixture.capture), fileSize(fixture.server.AuditPath), fileSize(fixture.server.StderrPath))
	}
	if got := strings.TrimSpace(runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "rev-parse", "HEAD").stdout); got != fixture.seedCommit {
		t.Fatalf("cloned HEAD = %q, want %q", got, fixture.seedCommit)
	}
	updatedCommit := fixture.gitea.Update(t, "fetched.txt", "fetched through operator setup\n")
	fetch := runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "fetch", "origin")
	if got := strings.TrimSpace(runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "rev-parse", "origin/main").stdout); got != updatedCommit {
		t.Fatalf("fetched origin/main = %q, want %q", got, updatedCommit)
	}
	return clone, fetch
}

func (fixture *pinnedGiteaGitFixture) pushAllowedAndDenied(t *testing.T) (pinnedGitResult, pinnedGitResult) {
	t.Helper()
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "config", "user.name", "RepoWolf Agent")
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "config", "user.email", "agent@example.invalid")
	if err := os.WriteFile(filepath.Join(fixture.checkout, "allowed.txt"), []byte("allowed pinned Gitea push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "add", "allowed.txt")
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "commit", "-m", "allowed pinned push")
	allowedCommit := strings.TrimSpace(runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "rev-parse", "HEAD").stdout)
	allowed := runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "push", "origin", "HEAD:refs/heads/feature/allowed")
	if got, found := fixture.gitea.Ref(t, "refs/heads/feature/allowed"); !found || got != allowedCommit {
		t.Fatalf("allowed provider ref = %q, %t; want %q, true", got, found, allowedCommit)
	}
	if fileSize(fixture.receiveCapture) <= 0 {
		t.Fatal("allowed push forwarded no provider input")
	}
	if err := os.WriteFile(filepath.Join(fixture.checkout, "denied.txt"), []byte("denied pinned Gitea push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "add", "denied.txt")
	runPinnedGitOK(t, fixture.gitPath, fixture.checkout, fixture.gitEnv, "commit", "-m", "denied pinned push")
	if _, found := fixture.gitea.Ref(t, "refs/heads/denied"); found {
		t.Fatal("denied provider ref unexpectedly exists before push")
	}
	denied := runPinnedGit(fixture.gitPath, fixture.checkout, fixture.gitEnv, "push", "origin", "HEAD:refs/heads/denied")
	if denied.err == nil || !strings.Contains(denied.stderr, "repowolf git transport failed") {
		t.Fatalf("denied push = %v stderr=%q", denied.err, denied.stderr)
	}
	if _, found := fixture.gitea.Ref(t, "refs/heads/denied"); found || fileSize(fixture.receiveCapture) != 0 {
		t.Fatalf("denied push changed provider state: found=%t captureBytes=%d", found, fileSize(fixture.receiveCapture))
	}
	return allowed, denied
}

func (fixture *pinnedGiteaGitFixture) cancelActivePush(t *testing.T) string {
	t.Helper()
	if err := os.WriteFile(fixture.blockReceive, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pushContext, cancelPush := context.WithCancel(context.Background())
	cancelledCommand := exec.CommandContext(pushContext, fixture.gitPath, "push", "origin", "HEAD:refs/heads/cancelled")
	cancelledCommand.Dir, cancelledCommand.Env = fixture.checkout, fixture.gitEnv
	cancelledCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cancelledCommand.Cancel = func() error { return syscall.Kill(-cancelledCommand.Process.Pid, syscall.SIGKILL) }
	cancelledCommand.WaitDelay = 100 * time.Millisecond
	var output bytes.Buffer
	cancelledCommand.Stdout, cancelledCommand.Stderr = &output, &output
	if err := cancelledCommand.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fileSize(fixture.receivePID) <= 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fileSize(fixture.receivePID) <= 0 {
		cancelPush()
		t.Fatal("cancelled receive-pack did not start")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(mustRead(fixture.receivePID))))
	if err != nil {
		cancelPush()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cancelledCommand.Wait() }()
	cancelPush()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled Git push did not return promptly")
	}
	if err := os.Remove(fixture.blockReceive); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("receive-pack wrapper PID %d survived cancellation", pid)
	}
	if _, found := fixture.gitea.Ref(t, "refs/heads/cancelled"); found {
		t.Fatal("cancelled provider ref exists")
	}
	return output.String()
}
