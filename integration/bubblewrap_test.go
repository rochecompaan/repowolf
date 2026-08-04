//go:build integration && linux

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
	"golang.org/x/sys/unix"
)

const stdinMarker = "task15-forwarded-stdin-marker"

type bubblewrapConfig struct {
	Bubblewrap  string `json:"bubblewrap"`
	ClientRoot  string `json:"clientRoot"`
	GitRoot     string `json:"gitRoot"`
	Shell       string `json:"shell"`
	ClosureFile string `json:"closureFile"`
}

func openPseudoTerminal(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		master.Close()
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatal(err)
	}
	return master, slave
}

func TestBubblewrapClientClosureSupportsRestrictedGHAndRealGit(t *testing.T) {
	config := loadBubblewrapConfig(t)
	closure := readClosure(t, config)
	fixture := prepareJailFixture(t)
	before := checkoutState(t, fixture, fixture.checkout)

	command := exec.Command(config.Bubblewrap, bubblewrapArguments(t, config, closure, fixture)...)
	terminalMaster, terminalSlave := openPseudoTerminal(t)
	defer terminalMaster.Close()
	command.Stdin = terminalSlave
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		terminalSlave.Close()
		t.Fatalf("start Bubblewrap: %v", err)
	}
	terminalSlave.Close()
	if _, err := terminalMaster.Write([]byte(stdinMarker + "\n")); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("forward Bubblewrap TTY stdin: %v", err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("Bubblewrap jail: %v\nstdout:\n%s\nstderr:\n%s\nserver stderr:\n%s\naudit:\n%s\nprovider stderr:\n%s",
			err, stdout.String(), stderr.String(), readIfPresent(fixture.server.StderrPath),
			readIfPresent(fixture.server.AuditPath), readIfPresent(fixture.providerStderr))
	}
	for _, leaked := range []string{agentToken, providerCredential, environmentMarker, providerStderr, sshStderrMarker} {
		if strings.Contains(stdout.String()+stderr.String(), leaked) {
			t.Fatalf("jail output leaked marker %q", leaked)
		}
	}

	if after := checkoutState(t, fixture, fixture.checkout); after != before {
		t.Fatalf("jail mutated checkout state:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	allowed := strings.TrimSpace(fixture.gitOK(t, fixture.remote, "rev-parse", "refs/heads/feature/task15").stdout)
	candidate := strings.TrimSpace(fixture.gitOK(t, fixture.checkout, "rev-parse", "refs/heads/feature/candidate").stdout)
	if allowed != candidate {
		t.Fatalf("allowed remote ref = %q, want %q", allowed, candidate)
	}
	if input := mustRead(fixture.uploadInput); len(input) == 0 {
		t.Fatal("jailed fetch forwarded no real Git client bytes")
	}
	if input := mustRead(fixture.receiveInput); len(input) != 0 {
		t.Fatalf("denied exact-main update forwarded %d client bytes", len(input))
	}

	fixture.server.Stop(t)
	expected := append(forgeAuditExpectations("github.issue_list"), gitAuditExpectations(
		"refs/heads/feature/task15", "refs/heads/main",
	)...)
	assertAuditInvocations(t, string(mustRead(fixture.server.AuditPath)), expected, auditLeakMarkers())
	for name, environment := range map[string]string{
		"provider": string(mustRead(fixture.providerEnvironment)),
		"ssh":      string(mustRead(fixture.sshEnvironment)),
	} {
		if !strings.Contains(environment, "GH_TOKEN="+providerCredential) ||
			!strings.Contains(environment, "REPOWOLF_TOKEN_AGENT=unset") ||
			!strings.Contains(environment, "REPOWOLF_ENDPOINT=unset") {
			t.Errorf("%s environment contract mismatch: %q", name, environment)
		}
	}
	assertNoFixtureProcess(t, fixture.root)
	assertRepositoryUnchanged(t, fixture.sourceStatus)
}

type jailFixture struct {
	*gitFixture
	checkout, providerEnvironment, providerStderr string
}

func prepareJailFixture(t *testing.T) *jailFixture {
	t.Helper()
	fixture := newGitFixture(t)
	fixture.server.Stop(t)
	providerArgv := filepath.Join(fixture.root, "provider.argv")
	providerInput := filepath.Join(fixture.root, "provider.stdin")
	providerOutput := filepath.Join(fixture.root, "provider.stdout")
	providerEnvironment := filepath.Join(fixture.root, "provider.env")
	providerStderrPath := filepath.Join(fixture.root, "provider.stderr")
	certificate := fixture.server.Certificate
	fixture.server = testutil.StartServer(t, testutil.ServerOptions{
		Binary: fixture.binaries.Service, PolicyPath: filepath.Join("testdata", "policy.yaml"), Certificate: certificate,
		GHPath: filepath.Join(fixture.root, "bin", "fake-provider"), SSHPath: filepath.Join(fixture.root, "bin", "fake-ssh"),
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken, "GH_TOKEN=" + providerCredential,
			"TASK13_ENV_MARKER=" + environmentMarker,
			"FAKE_PROVIDER_ARGV_LOG=" + providerArgv, "FAKE_PROVIDER_STDIN_LOG=" + providerInput,
			"FAKE_PROVIDER_OUTPUT_LOG=" + providerOutput, "FAKE_PROVIDER_ENV_LOG=" + providerEnvironment,
			"FAKE_PROVIDER_STDERR_LOG=" + providerStderrPath, "FAKE_PROVIDER_STDERR=" + providerStderr,
			"FAKE_PROVIDER_ISSUE_BODY=" + issueBodyMarker, "FAKE_PROVIDER_COMMENT=" + commentMarker,
			"FAKE_SSH_REPOSITORY=" + fixture.remote, "FAKE_SSH_ARGV_LOG=" + fixture.sshArgv,
			"FAKE_SSH_ENV_LOG=" + fixture.sshEnvironment, "FAKE_SSH_STDERR=" + sshStderrMarker,
			"FAKE_SSH_UPLOAD_INPUT=" + fixture.uploadInput, "FAKE_SSH_RECEIVE_INPUT=" + fixture.receiveInput,
			"FAKE_GIT_UPLOAD_PACK=" + mustLookPath(t, "git-upload-pack"),
			"FAKE_GIT_RECEIVE_PACK=" + mustLookPath(t, "git-receive-pack"), "FAKE_TEE=" + mustLookPath(t, "tee"),
		},
	})

	checkout := filepath.Join(fixture.root, "checkout")
	fixture.gitOK(t, fixture.root, "clone", fixture.remote, checkout)
	fixture.gitOK(t, checkout, "config", "user.name", "Task 15")
	fixture.gitOK(t, checkout, "config", "user.email", "task15@invalid")
	fixture.gitOK(t, checkout, "switch", "-c", "feature/candidate")
	if err := os.WriteFile(filepath.Join(checkout, "candidate.txt"), []byte("task15 candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.gitOK(t, checkout, "add", "candidate.txt")
	fixture.gitOK(t, checkout, "commit", "-m", "task15 candidate")
	fixture.gitOK(t, checkout, "switch", "main")
	fixture.gitOK(t, checkout, "remote", "set-url", "origin", "ssh://git@github.com:22/alpha/repo.git")
	return &jailFixture{
		gitFixture: fixture, checkout: checkout,
		providerEnvironment: providerEnvironment, providerStderr: providerStderrPath,
	}
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func checkoutState(t *testing.T, fixture *jailFixture, checkout string) string {
	t.Helper()
	status := fixture.gitOK(t, checkout, "status", "--porcelain=v1", "--untracked-files=all").stdout
	refs := fixture.gitOK(t, checkout, "for-each-ref", "--format=%(refname) %(objectname)").stdout
	head := fixture.gitOK(t, checkout, "rev-parse", "HEAD").stdout
	return status + "--refs--\n" + refs + "--head--\n" + head
}

func loadBubblewrapConfig(t *testing.T) bubblewrapConfig {
	t.Helper()
	config := bubblewrapConfig{
		Bubblewrap: os.Getenv("REPOWOLF_TEST_BUBBLEWRAP"), ClientRoot: os.Getenv("REPOWOLF_TEST_CLIENT_ROOT"),
		GitRoot: os.Getenv("REPOWOLF_TEST_GIT_ROOT"), Shell: os.Getenv("REPOWOLF_TEST_SHELL"),
		ClosureFile: os.Getenv("REPOWOLF_TEST_CLOSURE_FILE"),
	}
	if config.complete() {
		return config
	}
	root := testRepositoryRoot(t)
	system := map[string]string{"amd64": "x86_64-linux", "arm64": "aarch64-linux"}[runtime.GOARCH]
	if system == "" || runtime.GOOS != "linux" {
		t.Fatalf("Bubblewrap integration requires Linux amd64 or arm64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	checkAttribute := fmt.Sprintf(".#checks.%s.bubblewrap", system)
	build := exec.Command("nix", "build", "--no-link", "--print-build-logs", checkAttribute)
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Nix Bubblewrap check: %v\n%s", err, output)
	}
	attribute := checkAttribute + ".testConfig"
	command := exec.Command("nix", "eval", "--json", attribute)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("load Nix Bubblewrap test configuration: %v\n%s", err, output)
	}
	if err := json.Unmarshal(output, &config); err != nil {
		t.Fatalf("decode Nix Bubblewrap test configuration: %v", err)
	}
	for _, path := range []string{config.Bubblewrap, config.ClientRoot, config.GitRoot, config.Shell, config.ClosureFile} {
		realise := exec.Command("nix-store", "--realise", path)
		if result, err := realise.CombinedOutput(); err != nil {
			t.Fatalf("realise %s: %v\n%s", path, err, result)
		}
	}
	if !config.complete() {
		t.Fatal("Nix Bubblewrap test configuration is incomplete")
	}
	return config
}

func (config bubblewrapConfig) complete() bool {
	return config.Bubblewrap != "" && config.ClientRoot != "" && config.GitRoot != "" && config.Shell != "" && config.ClosureFile != ""
}

func readClosure(t *testing.T, config bubblewrapConfig) []string {
	t.Helper()
	contents, err := os.ReadFile(config.ClosureFile)
	if err != nil {
		t.Fatal(err)
	}
	paths := strings.Fields(string(contents))
	sort.Strings(paths)
	if !containsPath(paths, config.ClientRoot) || !containsPath(paths, config.GitRoot) {
		t.Fatalf("selected jail closure lacks client or Git root: %v", paths)
	}
	for _, path := range paths {
		for _, forbidden := range []string{"-repowolf-dev", "github-cli", "-openssh-"} {
			if strings.Contains(path, forbidden) {
				t.Fatalf("forbidden service/provider package in jail closure: %s", path)
			}
		}
	}
	return paths
}

func containsPath(paths []string, target string) bool {
	index := sort.SearchStrings(paths, target)
	return index < len(paths) && paths[index] == target
}

func readIfPresent(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<unavailable: %v>", err)
	}
	return string(contents)
}

func bubblewrapArguments(t *testing.T, config bubblewrapConfig, closure []string, fixture *jailFixture) []string {
	t.Helper()
	args := []string{
		"--unshare-all", "--share-net", "--die-with-parent", "--clearenv",
		"--dir", "/nix", "--dir", "/nix/store",
	}
	for _, path := range closure {
		args = append(args, "--ro-bind", path, path)
	}
	args = append(args,
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp",
		"--dir", "/home", "--dir", "/home/jail", "--dir", "/work",
		"--bind", fixture.checkout, "/work/checkout",
		"--dir", "/run", "--dir", "/run/repowolf",
		"--ro-bind", fixture.server.Certificate.CAFile, "/run/repowolf/ca.pem",
		"--ro-bind", filepath.Join(testRepositoryRoot(t), "integration", "testdata", "jail-command.sh"), "/jail-command.sh",
		"--chdir", "/work/checkout",
		"--setenv", "REPOWOLF_ENDPOINT", fixture.server.Endpoint,
		"--setenv", "REPOWOLF_TOKEN", agentToken,
		"--setenv", "REPOWOLF_CA_FILE", "/run/repowolf/ca.pem",
		"--setenv", "GIT_SSH_COMMAND", config.ClientRoot+"/bin/repowolf-git-ssh",
		"--", config.Shell, "/jail-command.sh", config.ClientRoot, config.GitRoot,
	)
	for _, path := range hostPathsThatMustNotEnterJail(fixture) {
		args = append(args, path)
	}
	return args
}

func hostPathsThatMustNotEnterJail(fixture *jailFixture) []string {
	paths := []string{fixture.binaries.Service, os.Getenv("SSH_AUTH_SOCK")}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "gh"), filepath.Join(home, ".ssh"))
	}
	for _, name := range []string{"gh", "ssh"} {
		if path, err := exec.LookPath(name); err == nil {
			if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
				path = resolved
			}
			paths = append(paths, path)
		}
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			result = append(result, path)
		}
	}
	return result
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Bubblewrap integration source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), ".."))
}
