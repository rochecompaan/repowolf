package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

const (
	pinnedGiteaProviderMarker  = "pinned-gitea-provider-token-marker"
	pinnedGiteaPrincipalMarker = "rw1_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
)

func TestPinnedGiteaGitInteroperability(t *testing.T) {
	if os.Getenv("REPOWOLF_GITEA_INTEGRATION") != "1" {
		t.Skip("set REPOWOLF_GITEA_INTEGRATION=1 to run pinned Gitea interoperability")
	}
	gitea := testutil.StartGitea(t)
	seedCommit := gitea.Seed(t, "seed.txt", "seeded through operator setup\n")
	access := gitea.StartSSHAccess(t)
	root := t.TempDir()
	binaries := testutil.BuildBinaries(t, filepath.Join(root, "bin"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	teePath, err := exec.LookPath("tee")
	if err != nil {
		t.Fatal(err)
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
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
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
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
	checkout := filepath.Join(root, "checkout")
	remote := "ssh://" + gitea.SSHUser + "@" + gitea.SSHHost + ":" + strconv.Itoa(gitea.SSHPort) + "/team_name/repo.one.git"
	clone := runPinnedGit(gitPath, root, gitEnv, "clone", remote, checkout)
	if clone.err != nil {
		t.Fatalf("pinned Gitea clone: %v; stdoutBytes=%d stderrBytes=%d captureBytes=%d auditBytes=%d brokerStderrBytes=%d", clone.err, len(clone.stdout), len(clone.stderr), fileSize(capture), fileSize(server.AuditPath), fileSize(server.StderrPath))
	}
	if got := strings.TrimSpace(runPinnedGitOK(t, gitPath, checkout, gitEnv, "rev-parse", "HEAD").stdout); got != seedCommit {
		t.Fatalf("cloned HEAD = %q, want %q", got, seedCommit)
	}
	updatedCommit := gitea.Update(t, "fetched.txt", "fetched through operator setup\n")
	fetch := runPinnedGitOK(t, gitPath, checkout, gitEnv, "fetch", "origin")
	if got := strings.TrimSpace(runPinnedGitOK(t, gitPath, checkout, gitEnv, "rev-parse", "origin/main").stdout); got != updatedCommit {
		t.Fatalf("fetched origin/main = %q, want %q", got, updatedCommit)
	}
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "config", "user.name", "RepoWolf Agent")
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "config", "user.email", "agent@example.invalid")
	if err := os.WriteFile(filepath.Join(checkout, "allowed.txt"), []byte("allowed pinned Gitea push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "add", "allowed.txt")
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "commit", "-m", "allowed pinned push")
	allowedCommit := strings.TrimSpace(runPinnedGitOK(t, gitPath, checkout, gitEnv, "rev-parse", "HEAD").stdout)
	allowed := runPinnedGitOK(t, gitPath, checkout, gitEnv, "push", "origin", "HEAD:refs/heads/feature/allowed")
	if got, found := gitea.Ref(t, "refs/heads/feature/allowed"); !found || got != allowedCommit {
		t.Fatalf("allowed provider ref = %q, %t; want %q, true", got, found, allowedCommit)
	}
	if fileSize(receiveCapture) <= 0 {
		t.Fatal("allowed push forwarded no provider input")
	}
	if err := os.WriteFile(filepath.Join(checkout, "denied.txt"), []byte("denied pinned Gitea push\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "add", "denied.txt")
	runPinnedGitOK(t, gitPath, checkout, gitEnv, "commit", "-m", "denied pinned push")
	if _, found := gitea.Ref(t, "refs/heads/denied"); found {
		t.Fatal("denied provider ref unexpectedly exists before push")
	}
	denied := runPinnedGit(gitPath, checkout, gitEnv, "push", "origin", "HEAD:refs/heads/denied")
	if denied.err == nil || !strings.Contains(denied.stderr, "repowolf git transport failed") {
		t.Fatalf("denied push = %v stderr=%q", denied.err, denied.stderr)
	}
	if _, found := gitea.Ref(t, "refs/heads/denied"); found || fileSize(receiveCapture) != 0 {
		t.Fatalf("denied push changed provider state: found=%t captureBytes=%d", found, fileSize(receiveCapture))
	}
	if err := os.WriteFile(blockReceive, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	pushContext, cancelPush := context.WithCancel(context.Background())
	cancelledCommand := exec.CommandContext(pushContext, gitPath, "push", "origin", "HEAD:refs/heads/cancelled")
	cancelledCommand.Dir, cancelledCommand.Env = checkout, gitEnv
	cancelledCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cancelledCommand.Cancel = func() error {
		return syscall.Kill(-cancelledCommand.Process.Pid, syscall.SIGKILL)
	}
	cancelledCommand.WaitDelay = 100 * time.Millisecond
	var cancelledOutput bytes.Buffer
	cancelledCommand.Stdout, cancelledCommand.Stderr = &cancelledOutput, &cancelledOutput
	if err := cancelledCommand.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fileSize(receivePID) <= 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fileSize(receivePID) <= 0 {
		cancelPush()
		t.Fatal("cancelled receive-pack did not start")
	}
	pidValue, err := strconv.Atoi(strings.TrimSpace(string(mustRead(receivePID))))
	if err != nil {
		cancelPush()
		t.Fatal(err)
	}
	cancelledDone := make(chan error, 1)
	go func() { cancelledDone <- cancelledCommand.Wait() }()
	cancelPush()
	select {
	case <-cancelledDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled Git push did not return promptly")
	}
	if err := os.Remove(blockReceive); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for syscall.Kill(pidValue, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(pidValue, 0); err == nil {
		t.Fatalf("receive-pack wrapper PID %d survived cancellation", pidValue)
	}
	if _, found := gitea.Ref(t, "refs/heads/cancelled"); found {
		t.Fatal("cancelled provider ref exists")
	}
	server.Stop(t)
	auditLog := string(mustRead(server.AuditPath))
	captureLog := string(mustRead(capture))
	checkoutContents := string(mustRead(filepath.Join(checkout, "seed.txt")))
	for channel, contents := range map[string]string{
		"clone stdout": clone.stdout, "clone stderr": clone.stderr,
		"fetch stdout": fetch.stdout, "fetch stderr": fetch.stderr,
		"allowed push stdout": allowed.stdout, "allowed push stderr": allowed.stderr,
		"denied push stdout": denied.stdout, "denied push stderr": denied.stderr,
		"cancelled push": cancelledOutput.String(), "receive capture": string(mustRead(receiveCapture)),
		"broker stderr": string(mustRead(server.StderrPath)), "audit": auditLog,
		"checkout": checkoutContents, "ssh environment": captureLog,
	} {
		for _, marker := range []string{pinnedGiteaProviderMarker, pinnedGiteaPrincipalMarker} {
			if strings.Contains(contents, marker) {
				t.Errorf("%s leaked provider credential marker", channel)
			}
		}
	}
	providerEvents := strings.Count(auditLog, `"provider":"gitea"`)
	repositoryEvents := strings.Count(auditLog, `"repository":"pinned-gitea"`)
	if providerEvents != 10 || repositoryEvents != 10 {
		t.Fatalf("missing Gitea Git audit pairs: providerEvents=%d repositoryEvents=%d auditBytes=%d", providerEvents, repositoryEvents, len(auditLog))
	}
	records, err := parseAuditRecords([]byte(auditLog), []string{pinnedGiteaProviderMarker, pinnedGiteaPrincipalMarker})
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []string
	for _, record := range records {
		if record.event.Provider == "gitea" {
			lifecycle = append(lifecycle, record.event.Operation+":"+string(record.event.Outcome))
		}
	}
	wantLifecycle := []string{
		"git.upload-pack:accepted", "git.upload-pack:completed",
		"git.upload-pack:accepted", "git.upload-pack:completed",
		"git.receive-pack:accepted", "git.receive-pack:completed",
		"git.receive-pack:accepted", "git.receive-pack:denied",
		"git.receive-pack:accepted", "git.receive-pack:cancelled",
	}
	if strings.Join(lifecycle, "\n") != strings.Join(wantLifecycle, "\n") {
		t.Fatalf("Gitea audit lifecycle = %v, want %v", lifecycle, wantLifecycle)
	}
	var receiveTerminals []auditRecord
	for _, record := range records {
		if record.event.Provider == "gitea" && record.event.Operation == "git.receive-pack" && string(record.event.Outcome) != "accepted" {
			receiveTerminals = append(receiveTerminals, record)
		}
	}
	if len(receiveTerminals) != 3 {
		t.Fatalf("receive-pack terminal audit count = %d, want 3", len(receiveTerminals))
	}
	allowedTerminal, deniedTerminal, cancelledTerminal := receiveTerminals[0], receiveTerminals[1], receiveTerminals[2]
	if string(allowedTerminal.event.Outcome) != "completed" || allowedTerminal.event.Reason != "GIT_TERMINAL_CATEGORY_COMPLETED" ||
		!reflect.DeepEqual(allowedTerminal.event.Refs, []string{"refs/heads/feature/allowed"}) || allowedTerminal.event.UpdateCount != 1 ||
		allowedTerminal.event.InputBytes <= 0 || allowedTerminal.event.OutputBytes <= 0 || !allowedTerminal.fields["input_bytes"] || !allowedTerminal.fields["output_bytes"] {
		t.Fatalf("unsafe or incomplete allowed receive-pack terminal audit: %#v fields=%v", allowedTerminal.event, allowedTerminal.fields)
	}
	if string(deniedTerminal.event.Outcome) != "denied" || deniedTerminal.event.Reason != "GIT_TERMINAL_CATEGORY_INVALID_REQUEST" ||
		!reflect.DeepEqual(deniedTerminal.event.Refs, []string{"refs/heads/denied"}) || deniedTerminal.event.UpdateCount != 1 ||
		deniedTerminal.event.InputBytes != 0 || deniedTerminal.fields["input_bytes"] || deniedTerminal.event.OutputBytes <= 0 || !deniedTerminal.fields["output_bytes"] {
		t.Fatalf("unsafe or incomplete denied receive-pack terminal audit: %#v fields=%v", deniedTerminal.event, deniedTerminal.fields)
	}
	if string(cancelledTerminal.event.Outcome) != "cancelled" || cancelledTerminal.event.Reason != "GIT_TERMINAL_CATEGORY_UNAVAILABLE" ||
		len(cancelledTerminal.event.Refs) != 0 || cancelledTerminal.event.UpdateCount != 0 || cancelledTerminal.event.InputBytes != 0 || cancelledTerminal.fields["input_bytes"] {
		t.Fatalf("unsafe or incomplete cancelled receive-pack terminal audit: %#v fields=%v", cancelledTerminal.event, cancelledTerminal.fields)
	}
	invocations := strings.Count(captureLog, "BEGIN\n")
	giteaTokenUnset := strings.Contains(captureLog, "REPOWOLF_TOKEN_GITEA=unset")
	principalTokenUnset := strings.Contains(captureLog, "REPOWOLF_TOKEN_AGENT=unset")
	if invocations != 5 || !giteaTokenUnset || !principalTokenUnset {
		t.Fatalf("unexpected SSH environment capture: invocations=%d giteaTokenUnset=%t principalTokenUnset=%t captureBytes=%d", invocations, giteaTokenUnset, principalTokenUnset, len(captureLog))
	}
	trustedPrefix := "-T\n-p\n" + strconv.Itoa(gitea.SSHPort) + "\n--\n" + gitea.SSHUser + "@" + gitea.SSHHost + "\n"
	trustedUploads := strings.Count(captureLog, trustedPrefix+"git-upload-pack '"+gitea.Owner+"/"+gitea.Repository+".git'")
	trustedReceives := strings.Count(captureLog, trustedPrefix+"git-receive-pack '"+gitea.Owner+"/"+gitea.Repository+".git'")
	if trustedUploads != 2 || trustedReceives != 3 {
		t.Fatalf("SSH wrapper did not preserve trusted argv: uploads=%d receives=%d captureBytes=%d", trustedUploads, trustedReceives, len(captureLog))
	}
}

type pinnedGitResult struct {
	stdout, stderr string
	err            error
}

func runPinnedGit(gitPath, directory string, environment []string, args ...string) pinnedGitResult {
	command := exec.Command(gitPath, args...)
	command.Dir, command.Env = directory, environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return pinnedGitResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func runPinnedGitOK(t *testing.T, gitPath, directory string, environment []string, args ...string) pinnedGitResult {
	t.Helper()
	result := runPinnedGit(gitPath, directory, environment, args...)
	if result.err != nil {
		t.Fatalf("git %v: %v; stdoutBytes=%d stderrBytes=%d", args, result.err, len(result.stdout), len(result.stderr))
	}
	return result
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
