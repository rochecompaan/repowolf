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
	packMarker           = "task13-pack-payload-marker"
	sshStderrMarker      = "task13-ssh-stderr-marker"
	allowedUpdateMarker  = "task13-allowed-update-marker"
	allowedContentMarker = "task13-allowed-content-marker"
	deniedUpdateMarker   = "task13-denied-update-marker"
	deniedContentMarker  = "task13-denied-content-marker"
)

func TestGitFixtureIgnoresHostConfigurationInjection(t *testing.T) {
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "core.sshCommand")
	t.Setenv("GIT_CONFIG_VALUE_0", "/bin/false")
	t.Setenv("GIT_CONFIG_KEY_1", "user.name")
	t.Setenv("GIT_CONFIG_VALUE_1", "injected-count")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'user.email=injected-parameters'")

	fixture := newGitFixture(t)
	for _, key := range []string{"user.name", "user.email"} {
		result := fixture.git(t, fixture.root, "config", "--get", key)
		if result.err == nil || result.stdout != "" {
			t.Fatalf("inherited %s injection: err=%v stdout=%q stderr=%q", key, result.err, result.stdout, result.stderr)
		}
	}
	checkout := filepath.Join(fixture.root, "contamination-checkout")
	if result := fixture.git(t, fixture.root, "clone", "ssh://git@github.com:22/alpha/repo.git", checkout); result.err != nil {
		t.Fatalf("isolated clone: %v; stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
	}
	fixture.server.Stop(t)
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

func TestRealGitDefaultPortStreamsOfflineAndDeniesDefaultMainBeforeProviderInput(t *testing.T) {
	fixture := newGitFixture(t)
	checkout := filepath.Join(fixture.root, "checkout")
	clone := fixture.git(t, fixture.root, "clone", "git@github.com:alpha/repo.git", checkout)
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
	assertSSHEnvironment(t, string(mustRead(fixture.sshEnvironment)))
	assertAuditInvocations(t, string(mustRead(fixture.server.AuditPath)), gitAuditExpectations(
		"refs/heads/feature/task13", "refs/heads/main",
	), auditLeakMarkers())
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

func TestRealGitGiteaCloneFetchAndControlledPushes(t *testing.T) {
	fixture := newGitFixture(t)
	checkout := filepath.Join(fixture.root, "gitea-checkout")
	clone := fixture.git(t, fixture.root, "clone", "ssh://forge_user@gitea.example.invalid:2222/team_name/repo.one.git", checkout)
	if clone.err != nil {
		t.Fatalf("Gitea clone: %v; stdout=%q stderr=%q", clone.err, clone.stdout, clone.stderr)
	}
	if got := string(mustRead(filepath.Join(checkout, "gitea.txt"))); got != "gitea seed\n" {
		t.Fatalf("Gitea checkout = %q", got)
	}
	if err := os.WriteFile(filepath.Join(fixture.giteaSeed, "fetched.txt"), []byte("fetched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.gitOK(t, fixture.giteaSeed, "add", "fetched.txt")
	fixture.gitOK(t, fixture.giteaSeed, "commit", "-m", "update Gitea fixture")
	fixture.gitOK(t, fixture.giteaSeed, "push", fixture.giteaRemote, "main")
	want := strings.TrimSpace(fixture.gitOK(t, fixture.giteaSeed, "rev-parse", "HEAD").stdout)
	fixture.gitOK(t, checkout, "fetch", "origin")
	if got := strings.TrimSpace(fixture.gitOK(t, checkout, "rev-parse", "origin/main").stdout); got != want {
		t.Fatalf("fetched origin/main = %q, want %q", got, want)
	}
	auditBefore := string(mustRead(fixture.server.AuditPath))
	command := exec.Command(fixture.binaries.GitSSH, "-o", "ProxyCommand=sh", "forge_user@gitea.example.invalid", "git-upload-pack 'Team_Name/Repo.One.git'")
	command.Env = isolatedGitEnvironment(fixture.root, fixture.gitPath, fixture.gitExecPath)
	if err := command.Run(); err == nil {
		t.Fatal("malformed shim argv accepted")
	}
	if got := string(mustRead(fixture.server.AuditPath)); got != auditBefore {
		t.Fatal("malformed request reached the broker audit path")
	}
	if denied := fixture.git(t, fixture.root, "ls-remote", "ssh://forge_user@gitea.example.invalid:2222/team_name/missing.git"); denied.err == nil {
		t.Fatal("ungranted Gitea repository accepted")
	}
	fixture.gitOK(t, checkout, "config", "user.name", "Gitea Agent")
	fixture.gitOK(t, checkout, "config", "user.email", "agent@invalid")
	if err := os.WriteFile(filepath.Join(checkout, "allowed.txt"), []byte("allowed-gitea-content-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.gitOK(t, checkout, "add", "allowed.txt")
	fixture.gitOK(t, checkout, "commit", "-m", "allowed Gitea push")
	allowedCommit := strings.TrimSpace(fixture.gitOK(t, checkout, "rev-parse", "HEAD").stdout)
	allowed := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/feature/allowed")
	if allowed.err != nil {
		t.Fatalf("allowed Gitea push: %v; stderr=%q", allowed.err, allowed.stderr)
	}
	if got := strings.TrimSpace(fixture.gitOK(t, fixture.giteaRemote, "rev-parse", "refs/heads/feature/allowed").stdout); got != allowedCommit {
		t.Fatalf("provider ref = %q, want %q", got, allowedCommit)
	}
	if len(mustRead(fixture.giteaReceiveInput)) == 0 {
		t.Fatal("allowed Gitea push forwarded no input")
	}
	if err := os.WriteFile(filepath.Join(checkout, "denied.txt"), []byte("denied-gitea-content-marker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.gitOK(t, checkout, "add", "denied.txt")
	fixture.gitOK(t, checkout, "commit", "-m", "denied Gitea push")
	denied := fixture.git(t, checkout, "push", "origin", "HEAD:refs/heads/denied")
	if denied.err == nil || !strings.Contains(denied.stderr, "repowolf git transport failed") {
		t.Fatalf("denied Gitea push = %v stderr=%q", denied.err, denied.stderr)
	}
	if len(mustRead(fixture.giteaReceiveInput)) != 0 {
		t.Fatal("denied Gitea push forwarded input")
	}
	if output := fixture.git(t, fixture.giteaRemote, "rev-parse", "--verify", "refs/heads/denied"); output.err == nil {
		t.Fatalf("denied provider ref exists: %s", output.stdout)
	}
	fixture.server.Stop(t)
	argv := string(mustRead(fixture.sshArgv))
	if !strings.Contains(argv, "forge_user@gitea.example.invalid\ngit-upload-pack 'Team_Name/Repo.One.git'") {
		t.Fatalf("missing trusted Gitea argv in %q", argv)
	}
	assertSSHEnvironment(t, string(mustRead(fixture.sshEnvironment)))
	auditLog := string(mustRead(fixture.server.AuditPath))
	assertAuditInvocations(t, auditLog, giteaGitAuditExpectations("refs/heads/feature/allowed", "refs/heads/denied"), auditLeakMarkers())
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

func assertSSHEnvironment(t *testing.T, environment string) {
	t.Helper()
	for _, expected := range []string{
		"GH_TOKEN=unset",
		"GITHUB_TOKEN=unset",
		"REPOWOLF_TOKEN_AGENT=unset",
		"REPOWOLF_TOKEN_GITHUB=unset",
		"REPOWOLF_TOKEN_GITEA=unset",
		"SSH_AUTH_SOCK=/run/test-agent.sock",
		"GIT_PROTOCOL=version=2",
	} {
		if !strings.Contains(environment, expected) {
			t.Errorf("SSH environment missing %q in %q", expected, environment)
		}
	}
}

type gitFixture struct {
	root, remote, giteaRemote, giteaSeed, gitPath, gitExecPath                              string
	uploadInput, receiveInput, giteaUploadInput, giteaReceiveInput, sshArgv, sshEnvironment string
	sourceStatus                                                                            string
	binaries                                                                                testutil.Binaries
	server                                                                                  *testutil.Server
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
	tee, err := exec.LookPath("tee")
	if err != nil {
		t.Fatal(err)
	}
	execPath, err := exec.Command(gitPath, "--exec-path").Output()
	if err != nil {
		t.Fatal(err)
	}
	fixture := &gitFixture{
		root: root, sourceStatus: sourceStatus, remote: filepath.Join(root, "remote.git"),
		giteaRemote: filepath.Join(root, "gitea-remote.git"), giteaSeed: filepath.Join(root, "gitea-seed"), gitPath: gitPath,
		gitExecPath: strings.TrimSpace(string(execPath)), uploadInput: filepath.Join(root, "upload.input"),
		receiveInput: filepath.Join(root, "receive.input"), giteaUploadInput: filepath.Join(root, "gitea-upload.input"),
		giteaReceiveInput: filepath.Join(root, "gitea-receive.input"),
		sshArgv:           filepath.Join(root, "ssh.argv"), sshEnvironment: filepath.Join(root, "ssh.environment"),
	}
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

	fixture.gitOK(t, root, "init", "--bare", fixture.giteaRemote)
	fixture.gitOK(t, root, "init", fixture.giteaSeed)
	fixture.gitOK(t, fixture.giteaSeed, "config", "user.name", "Gitea Operator")
	fixture.gitOK(t, fixture.giteaSeed, "config", "user.email", "gitea@invalid")
	if err := os.WriteFile(filepath.Join(fixture.giteaSeed, "gitea.txt"), []byte("gitea seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.gitOK(t, fixture.giteaSeed, "add", "gitea.txt")
	fixture.gitOK(t, fixture.giteaSeed, "commit", "-m", "seed Gitea fixture")
	fixture.gitOK(t, fixture.giteaSeed, "branch", "-M", "main")
	fixture.gitOK(t, fixture.giteaSeed, "push", fixture.giteaRemote, "main")
	fixture.gitOK(t, fixture.giteaRemote, "symbolic-ref", "HEAD", "refs/heads/main")

	binDir := filepath.Join(root, "bin")
	fixture.binaries = testutil.BuildBinaries(t, binDir)
	provider := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	fixture.server = testutil.StartServer(t, testutil.ServerOptions{
		Binary: fixture.binaries.Service, PolicyPath: filepath.Join("testdata", "policy.yaml"), Certificate: certificate, GHPath: provider, SSHPath: ssh,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken, "REPOWOLF_TOKEN_GITHUB=" + providerCredential,
			"REPOWOLF_TOKEN_GITEA=" + giteaCredential, "GH_TOKEN=" + ambientGHCredential,
			"GITHUB_TOKEN=" + ambientGitHubCredential, "SSH_AUTH_SOCK=/run/test-agent.sock",
			"GIT_PROTOCOL=version=2",
			"FAKE_SSH_GITHUB_REPOSITORY=" + fixture.remote, "FAKE_SSH_GITEA_REPOSITORY=" + fixture.giteaRemote,
			"FAKE_SSH_ARGV_LOG=" + fixture.sshArgv, "FAKE_SSH_ENV_LOG=" + fixture.sshEnvironment,
			"FAKE_SSH_STDERR=" + sshStderrMarker, "FAKE_SSH_GITHUB_UPLOAD_INPUT=" + fixture.uploadInput,
			"FAKE_SSH_GITHUB_RECEIVE_INPUT=" + fixture.receiveInput, "FAKE_SSH_GITEA_UPLOAD_INPUT=" + fixture.giteaUploadInput,
			"FAKE_SSH_GITEA_RECEIVE_INPUT=" + fixture.giteaReceiveInput,
			"FAKE_GIT_UPLOAD_PACK=" + upload, "FAKE_GIT_RECEIVE_PACK=" + receive, "FAKE_TEE=" + tee,
		},
	})
	return fixture
}

func (fixture *gitFixture) git(t *testing.T, directory string, args ...string) commandResult {
	t.Helper()
	command := exec.Command(fixture.gitPath, args...)
	command.Dir = directory
	command.Env = isolatedGitEnvironment(fixture.root, fixture.gitPath, fixture.gitExecPath)
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

func isolatedGitEnvironment(root, gitPath, gitExecPath string) []string {
	return []string{
		"HOME=" + filepath.Join(root, "empty-home"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "empty-xdg"),
		"PATH=" + filepath.Dir(gitPath),
		"GIT_EXEC_PATH=" + gitExecPath,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=0",
		"GIT_CONFIG_PARAMETERS=",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
		"TZ=UTC",
		"HTTP_PROXY=http://127.0.0.1:1",
		"HTTPS_PROXY=http://127.0.0.1:1",
		"ALL_PROXY=http://127.0.0.1:1",
		"http_proxy=http://127.0.0.1:1",
		"https_proxy=http://127.0.0.1:1",
		"all_proxy=http://127.0.0.1:1",
		"NO_PROXY=",
		"no_proxy=",
	}
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
