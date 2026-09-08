package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// PinnedGiteaImage is the interoperability image certified by the Git test.
const PinnedGiteaImage = "docker.gitea.com/gitea:1.27.2@sha256:d20286ca2b2e170fdf628e7231b8a31a3220ade39ff462b55041d43d1fc757dd"

// Gitea exposes only the non-secret coordinates needed by integration tests.
type Gitea struct {
	SSHHost    string
	SSHPort    int
	SSHUser    string
	Owner      string
	Repository string

	containerID string
	httpURL     string
	apiToken    string
	privateKey  string
}

// SSHAccess contains broker-side SSH state without provider credentials.
type SSHAccess struct {
	Home        string
	AgentSocket string
}

// StartGitea starts and provisions the digest-pinned Gitea fixture.
func StartGitea(t testing.TB) *Gitea {
	t.Helper()
	requireCommands(t, "docker", "git", "ssh", "ssh-agent", "ssh-add", "ssh-keygen", "ssh-keyscan")
	command := exec.Command("docker", "run", "-d",
		"-p", "127.0.0.1::3000", "-p", "127.0.0.1::22",
		"-e", "GITEA__database__DB_TYPE=sqlite3",
		"-e", "GITEA__security__INSTALL_LOCK=true",
		"-e", "GITEA__server__ROOT_URL=http://localhost:3000/",
		"-e", "GITEA__server__SSH_DOMAIN=localhost",
		PinnedGiteaImage,
	)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("start pinned Gitea container: %v", err)
	}
	fixture := &Gitea{containerID: strings.TrimSpace(string(output)), SSHHost: "localhost", SSHUser: "git", Owner: "Team_Name", Repository: "Repo.One"}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", fixture.containerID).Run() })

	httpPort := dockerPort(t, fixture.containerID, "3000/tcp")
	fixture.SSHPort = dockerPort(t, fixture.containerID, "22/tcp")
	fixture.httpURL = "http://127.0.0.1:" + strconv.Itoa(httpPort)
	waitHTTP(t, fixture.httpURL+"/api/healthz", 30*time.Second)

	password := "repowolf-fixture-password"
	runQuiet(t, "docker", "exec", "--user", "git", fixture.containerID, "gitea", "admin", "user", "create",
		"--username", fixture.Owner, "--password", password, "--email", "repowolf-gitea@example.invalid", "--must-change-password=false")
	token, err := exec.Command("docker", "exec", "--user", "git", fixture.containerID, "gitea", "admin", "user", "generate-access-token",
		"--username", fixture.Owner, "--token-name", "repowolf-integration", "--scopes", "write:repository,write:user", "--raw").Output()
	if err != nil {
		t.Fatalf("generate Gitea fixture token: %v", err)
	}
	fixture.apiToken = strings.TrimSpace(string(token))
	keyDir := t.TempDir()
	fixture.privateKey = filepath.Join(keyDir, "id_ed25519")
	runQuiet(t, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", fixture.privateKey)
	publicKey, err := os.ReadFile(fixture.privateKey + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	fixture.api(t, http.MethodPost, "/api/v1/user/keys", map[string]string{"title": "repowolf-integration", "key": strings.TrimSpace(string(publicKey))})
	fixture.api(t, http.MethodPost, "/api/v1/user/repos", map[string]any{"name": fixture.Repository, "private": true, "default_branch": "main"})
	waitSSH(t, fixture.SSHHost, fixture.SSHPort, 30*time.Second)
	return fixture
}

// Seed creates main with one file and returns the commit ID.
func (fixture *Gitea) Seed(t testing.TB, filename, contents string) string {
	t.Helper()
	work := t.TempDir()
	runGit(t, work, fixture.gitEnvironment(), "init", ".")
	runGit(t, work, fixture.gitEnvironment(), "config", "user.name", "RepoWolf Gitea fixture")
	runGit(t, work, fixture.gitEnvironment(), "config", "user.email", "repowolf-gitea@example.invalid")
	if err := os.WriteFile(filepath.Join(work, filename), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, fixture.gitEnvironment(), "add", filename)
	runGit(t, work, fixture.gitEnvironment(), "commit", "-m", "seed fixture")
	runGit(t, work, fixture.gitEnvironment(), "branch", "-M", "main")
	runGit(t, work, fixture.gitEnvironment(), "remote", "add", "origin", fixture.remoteURL())
	runGit(t, work, fixture.gitEnvironment(), "push", "-u", "origin", "main")
	return strings.TrimSpace(runGit(t, work, fixture.gitEnvironment(), "rev-parse", "HEAD"))
}

// Update commits one file on main and returns the commit ID.
func (fixture *Gitea) Update(t testing.TB, filename, contents string) string {
	t.Helper()
	work := filepath.Join(t.TempDir(), "update")
	runGit(t, ".", fixture.gitEnvironment(), "clone", fixture.remoteURL(), work)
	runGit(t, work, fixture.gitEnvironment(), "config", "user.name", "RepoWolf Gitea fixture")
	runGit(t, work, fixture.gitEnvironment(), "config", "user.email", "repowolf-gitea@example.invalid")
	if err := os.WriteFile(filepath.Join(work, filename), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, fixture.gitEnvironment(), "add", filename)
	runGit(t, work, fixture.gitEnvironment(), "commit", "-m", "update fixture")
	runGit(t, work, fixture.gitEnvironment(), "push", "origin", "main")
	return strings.TrimSpace(runGit(t, work, fixture.gitEnvironment(), "rev-parse", "HEAD"))
}

// StartSSHAccess creates verified known-host state and a private ssh-agent.
func (fixture *Gitea) StartSSHAccess(t testing.TB) SSHAccess {
	t.Helper()
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(sshDir, "known_hosts")
	identity := filepath.Join(sshDir, "id_ed25519")
	privateKey, err := os.ReadFile(fixture.privateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identity, privateKey, 0o600); err != nil {
		t.Fatal(err)
	}
	scan, err := exec.Command("ssh-keyscan", "-4", "-p", strconv.Itoa(fixture.SSHPort), fixture.SSHHost).Output()
	if err != nil || len(scan) == 0 {
		t.Fatalf("scan Gitea SSH host key: %v", err)
	}
	if err := os.WriteFile(knownHosts, scan, 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(t.TempDir(), "agent.sock")
	output, err := exec.Command("ssh-agent", "-a", socket, "-s").Output()
	if err != nil {
		t.Fatalf("start ssh-agent: %v", err)
	}
	pid := parseAgentPID(string(output))
	add := exec.Command("ssh-add", fixture.privateKey)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+socket)
	if output, err := add.CombinedOutput(); err != nil {
		t.Fatalf("add Gitea SSH key: %v: %s", err, output)
	}
	t.Cleanup(func() {
		if pid > 0 {
			_ = exec.Command("kill", strconv.Itoa(pid)).Run()
		}
	})
	return SSHAccess{Home: home, AgentSocket: socket}
}

func (fixture *Gitea) remoteURL() string {
	return fmt.Sprintf("ssh://%s@%s:%d/%s/%s.git", fixture.SSHUser, fixture.SSHHost, fixture.SSHPort, fixture.Owner, fixture.Repository)
}

func (fixture *Gitea) gitEnvironment() []string {
	ssh := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", fixture.privateKey)
	return append(os.Environ(), "GIT_SSH_COMMAND="+ssh, "GIT_TERMINAL_PROMPT=0")
}

func (fixture *Gitea) api(t testing.TB, method, path string, body any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, fixture.httpURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "token "+fixture.apiToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Gitea fixture API request: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("Gitea fixture API status: %s", response.Status)
	}
}

func requireCommands(t testing.TB, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("required command %s: %v", name, err)
		}
	}
}
func runQuiet(t testing.TB, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	if err := command.Run(); err != nil {
		t.Fatalf("%s failed: %v", name, err)
	}
}
func runGit(t testing.TB, directory string, environment []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
func dockerPort(t testing.TB, container, port string) int {
	t.Helper()
	output, err := exec.Command("docker", "port", container, port).Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	value := strings.TrimSpace(string(output))
	_, raw, ok := strings.Cut(value, ":")
	if !ok {
		t.Fatalf("unexpected docker port %q", value)
	}
	number, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return number
}
func waitHTTP(t testing.TB, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Gitea HTTP readiness timeout")
}
func waitSSH(t testing.TB, host string, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		command := exec.Command("ssh-keyscan", "-4", "-p", strconv.Itoa(port), host)
		if output, err := command.Output(); err == nil && len(output) > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Gitea SSH readiness timeout")
}
func parseAgentPID(output string) int {
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, "SSH_AGENT_PID=") {
			value := strings.TrimSuffix(strings.TrimPrefix(field, "SSH_AGENT_PID="), ";")
			pid, _ := strconv.Atoi(value)
			return pid
		}
	}
	return 0
}
