package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

const (
	packMarker      = "task13-pack-payload-marker"
	sshStderrMarker = "task13-ssh-stderr-marker"
)

func TestRealGitStreamsOfflineAndDeniesDefaultMainBeforeProviderInput(t *testing.T) {
	fixture := newGitFixture(t)
	checkout := filepath.Join(fixture.root, "checkout")
	clone := fixture.git(t, fixture.root, "clone", "ssh://git@github.com:22/alpha/repo.git", checkout)
	if clone.err != nil {
		t.Fatalf("git clone: %v; stdout=%q stderr=%q", clone.err, clone.stdout, clone.stderr)
	}
	if contents := string(mustRead(filepath.Join(checkout, "pack.txt"))); contents != packMarker+"\n" {
		t.Fatalf("checked-out pack payload = %q", contents)
	}
	if contents := mustRead(fixture.uploadInput); len(contents) == 0 {
		t.Fatal("upload-pack received no real Git client bytes")
	}
	fixture.gitOK(t, checkout, "config", "user.name", "Task 13")
	fixture.gitOK(t, checkout, "config", "user.email", "task13@invalid")

	fixture.gitOK(t, checkout, "switch", "-c", "feature/task13")
	os.WriteFile(filepath.Join(checkout, "feature.txt"), []byte("allowed\n"), 0o600)
	fixture.gitOK(t, checkout, "add", "feature.txt")
	fixture.gitOK(t, checkout, "commit", "-m", "allowed feature update")
	allowedCommit := strings.TrimSpace(fixture.gitOK(t, checkout, "rev-parse", "HEAD").stdout)
	allowed := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/feature/task13")
	if allowed.err != nil {
		t.Fatalf("allowed push: %v; stdout=%q stderr=%q", allowed.err, allowed.stdout, allowed.stderr)
	}
	if got := strings.TrimSpace(fixture.gitOK(t, fixture.remote, "rev-parse", "refs/heads/feature/task13").stdout); got != allowedCommit {
		t.Fatalf("allowed remote ref = %q, want %q", got, allowedCommit)
	}
	if contents := mustRead(fixture.receiveInput); len(contents) == 0 {
		t.Fatal("allowed receive-pack forwarded no real Git client bytes")
	}

	mainBefore := strings.TrimSpace(fixture.gitOK(t, fixture.remote, "rev-parse", "refs/heads/main").stdout)
	os.WriteFile(filepath.Join(checkout, "denied.txt"), []byte("denied\n"), 0o600)
	fixture.gitOK(t, checkout, "add", "denied.txt")
	fixture.gitOK(t, checkout, "commit", "-m", "denied main update")
	denied := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/main")
	if denied.err == nil || !strings.Contains(denied.stderr, "repowolf git transport failed") {
		t.Fatalf("denied push = %v; stdout=%q stderr=%q", denied.err, denied.stdout, denied.stderr)
	}
	if got := strings.TrimSpace(fixture.gitOK(t, fixture.remote, "rev-parse", "refs/heads/main").stdout); got != mainBefore {
		t.Fatalf("denied main mutated from %q to %q", mainBefore, got)
	}
	if contents := mustRead(fixture.receiveInput); len(contents) != 0 {
		t.Fatalf("denied update forwarded %d bytes to fake SSH", len(contents))
	}

	fixture.server.Stop(t)
	argv := string(mustRead(fixture.sshArgv))
	for _, expected := range []string{
		"-T\n-p\n22\n--\ngit@github.com\ngit-upload-pack 'alpha/repo.git'",
		"-T\n-p\n22\n--\ngit@github.com\ngit-receive-pack 'alpha/repo.git'",
	} {
		if !strings.Contains(argv, expected) {
			t.Errorf("fake SSH argv missing %q in %q", expected, argv)
		}
	}
	if audit := string(mustRead(fixture.server.AuditPath)); strings.Contains(audit, packMarker) || strings.Contains(audit, sshStderrMarker) {
		t.Errorf("Git audit leaked payload/stderr marker: %s", audit)
	}
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

type gitFixture struct {
	root, remote, gitPath, uploadInput, receiveInput, sshArgv string
	sourceStatus                                              string
	binaries                                                  testutil.Binaries
	server                                                    *testutil.Server
}

type commandResult struct {
	stdout, stderr string
	err            error
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	root := t.TempDir()
	sourceStatus := repositoryStatus(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	upload, err := exec.LookPath("git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	receive, err := exec.LookPath("git-receive-pack")
	if err != nil {
		t.Fatal(err)
	}
	fixture := &gitFixture{root: root, sourceStatus: sourceStatus, remote: filepath.Join(root, "remote.git"), gitPath: gitPath, uploadInput: filepath.Join(root, "upload.input"), receiveInput: filepath.Join(root, "receive.input"), sshArgv: filepath.Join(root, "ssh.argv")}
	fixture.gitOK(t, root, "init", "--bare", fixture.remote)
	seed := filepath.Join(root, "seed")
	fixture.gitOK(t, root, "init", seed)
	fixture.gitOK(t, seed, "config", "user.name", "Task 13")
	fixture.gitOK(t, seed, "config", "user.email", "task13@invalid")
	os.WriteFile(filepath.Join(seed, "pack.txt"), []byte(packMarker+"\n"), 0o600)
	fixture.gitOK(t, seed, "add", "pack.txt")
	fixture.gitOK(t, seed, "commit", "-m", "seed pack marker")
	fixture.gitOK(t, seed, "branch", "-M", "main")
	fixture.gitOK(t, seed, "push", fixture.remote, "main")
	fixture.gitOK(t, fixture.remote, "symbolic-ref", "HEAD", "refs/heads/main")

	binDir := filepath.Join(root, "bin")
	fixture.binaries = testutil.BuildBinaries(t, binDir)
	provider := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	fixture.server = testutil.StartServer(t, testutil.ServerOptions{
		Binary: fixture.binaries.Service, PolicyPath: filepath.Join("testdata", "policy.yaml"), Certificate: certificate, GHPath: provider, SSHPath: ssh,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken, "GH_TOKEN=" + providerCredential,
			"FAKE_SSH_REPOSITORY=" + fixture.remote, "FAKE_SSH_ARGV_LOG=" + fixture.sshArgv,
			"FAKE_SSH_STDERR=" + sshStderrMarker, "FAKE_SSH_UPLOAD_INPUT=" + fixture.uploadInput,
			"FAKE_SSH_RECEIVE_INPUT=" + fixture.receiveInput, "FAKE_GIT_UPLOAD_PACK=" + upload, "FAKE_GIT_RECEIVE_PACK=" + receive,
		},
	})
	return fixture
}

func (fixture *gitFixture) git(t *testing.T, directory string, args ...string) commandResult {
	t.Helper()
	command := exec.Command(fixture.gitPath, args...)
	command.Dir = directory
	command.Env = testutil.Environment(os.Environ(),
		"HOME="+filepath.Join(fixture.root, "empty-home"), "XDG_CONFIG_HOME="+filepath.Join(fixture.root, "empty-xdg"),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	)
	if fixture.server != nil {
		command.Env = testutil.Environment(command.Env,
			"GIT_SSH_COMMAND="+fixture.binaries.GitSSH,
			"REPOWOLF_ENDPOINT="+fixture.server.Endpoint, "REPOWOLF_CA_FILE="+fixture.server.Certificate.CAFile,
			"REPOWOLF_SERVER_NAME="+fixture.server.Certificate.ServerName, "REPOWOLF_TOKEN="+agentToken,
		)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return commandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func (fixture *gitFixture) gitOK(t *testing.T, directory string, args ...string) commandResult {
	t.Helper()
	result := fixture.git(t, directory, args...)
	if result.err != nil {
		t.Fatalf("git %v: %v; stdout=%q stderr=%q", args, result.err, result.stdout, result.stderr)
	}
	return result
}

func assertNoFixtureProcess(t *testing.T, root string) {
	t.Helper()
	output, err := exec.Command("ps", "-eo", "args=").Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, root) {
			t.Errorf("fixture process survived cleanup: %s", line)
		}
	}
}
