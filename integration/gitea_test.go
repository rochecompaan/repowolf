package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

const (
	pinnedGiteaProviderMarker  = "pinned-gitea-provider-token-marker"
	pinnedGiteaPrincipalMarker = "rw1_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
)

func TestPinnedGiteaCloneFetch(t *testing.T) {
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
	capture := filepath.Join(root, "ssh.capture")
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
exec %q -i %q -o IdentitiesOnly=yes -o UserKnownHostsFile=%q -o StrictHostKeyChecking=yes "$@"
`, sshPath, filepath.Join(access.Home, ".ssh", "id_ed25519"), filepath.Join(access.Home, ".ssh", "known_hosts"))
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
			"SSH_CAPTURE_PATH=" + capture, "GIT_PROTOCOL=version=2",
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
		captureContents, _ := os.ReadFile(capture)
		auditContents, _ := os.ReadFile(server.AuditPath)
		serverContents, _ := os.ReadFile(server.StderrPath)
		t.Fatalf("pinned Gitea clone: %v; stdout=%q stderr=%q capture=%q audit=%q broker=%q", clone.err, clone.stdout, clone.stderr, captureContents, auditContents, serverContents)
	}
	if got := strings.TrimSpace(runPinnedGitOK(t, gitPath, checkout, gitEnv, "rev-parse", "HEAD").stdout); got != seedCommit {
		t.Fatalf("cloned HEAD = %q, want %q", got, seedCommit)
	}
	updatedCommit := gitea.Update(t, "fetched.txt", "fetched through operator setup\n")
	fetch := runPinnedGitOK(t, gitPath, checkout, gitEnv, "fetch", "origin")
	if got := strings.TrimSpace(runPinnedGitOK(t, gitPath, checkout, gitEnv, "rev-parse", "origin/main").stdout); got != updatedCommit {
		t.Fatalf("fetched origin/main = %q, want %q", got, updatedCommit)
	}
	server.Stop(t)
	auditLog := string(mustRead(server.AuditPath))
	captureLog := string(mustRead(capture))
	checkoutContents := string(mustRead(filepath.Join(checkout, "seed.txt")))
	for channel, contents := range map[string]string{
		"clone stdout": clone.stdout, "clone stderr": clone.stderr,
		"fetch stdout": fetch.stdout, "fetch stderr": fetch.stderr,
		"broker stderr": string(mustRead(server.StderrPath)), "audit": auditLog,
		"checkout": checkoutContents, "ssh environment": captureLog,
	} {
		for _, marker := range []string{pinnedGiteaProviderMarker, pinnedGiteaPrincipalMarker} {
			if strings.Contains(contents, marker) {
				t.Errorf("%s leaked provider credential marker", channel)
			}
		}
	}
	if strings.Count(auditLog, `"provider":"gitea"`) != 4 || strings.Count(auditLog, `"repository":"pinned-gitea"`) != 4 {
		t.Fatalf("missing Gitea upload-pack audit pairs: %s", auditLog)
	}
	if strings.Count(captureLog, "BEGIN\n") != 2 || !strings.Contains(captureLog, "REPOWOLF_TOKEN_GITEA=unset") || !strings.Contains(captureLog, "REPOWOLF_TOKEN_AGENT=unset") {
		t.Fatalf("unexpected SSH environment capture: %s", captureLog)
	}
	trustedArgs := "-T\n-p\n" + strconv.Itoa(gitea.SSHPort) + "\n--\n" + gitea.SSHUser + "@" + gitea.SSHHost + "\ngit-upload-pack '" + gitea.Owner + "/" + gitea.Repository + ".git'"
	if strings.Count(captureLog, trustedArgs) != 2 {
		t.Fatalf("SSH wrapper did not preserve trusted argv: %s", captureLog)
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
		t.Fatalf("git %v: %v; stdout=%q stderr=%q", args, result.err, result.stdout, result.stderr)
	}
	return result
}
