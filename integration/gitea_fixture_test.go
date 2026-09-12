//go:build linux && gitea_integration

package integration_test

import (
	"crypto/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/testutil"
)

const giteaImage = "docker.gitea.com/gitea:1.27.2@sha256:d20286ca2b2e170fdf628e7231b8a31a3220ade39ff462b55041d43d1fc757dd"

type restrictedGiteaFixture struct {
	work        string
	address     string
	baseURL     string
	container   string
	certificate testutil.Certificate
	client      *http.Client
	token       string
}

type restrictedGiteaBroker struct {
	binaries    testutil.Binaries
	server      *testutil.Server
	environment []string
}

func newRestrictedGiteaFixture(t *testing.T, address, subnet string) *restrictedGiteaFixture {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("Docker required for gitea_integration: %v", err)
	}
	work := t.TempDir()
	network := "repowolf-gitea-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	dockerOutput(t, "network", "create", "--subnet", subnet, network)
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", network).Run() })
	certificate := testutil.GenerateCertificateForIPs(t, filepath.Join(work, "gitea-cert"), []net.IP{net.ParseIP(address)})
	if err := os.Chmod(filepath.Dir(certificate.CertificateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{certificate.CertificateFile, certificate.KeyFile} {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	container := dockerOutput(t, "run", "--detach", "--rm", "--network", network, "--ip", address,
		"--volume", filepath.Dir(certificate.CertificateFile)+":/certs:ro",
		"--env", "GITEA__database__DB_TYPE=sqlite3",
		"--env", "GITEA__security__INSTALL_LOCK=true",
		"--env", "GITEA__server__PROTOCOL=https",
		"--env", "GITEA__server__HTTP_PORT=443",
		"--env", "GITEA__server__SSL_MIN_VERSION=TLSv1.3",
		"--env", "GITEA__server__SSL_MAX_VERSION=TLSv1.3",
		"--env", "GITEA__server__CERT_FILE=/certs/server.pem",
		"--env", "GITEA__server__KEY_FILE=/certs/server-key.pem",
		"--env", "GITEA__api__MAX_RESPONSE_ITEMS=50",
		giteaImage,
	)
	t.Cleanup(func() {
		command := exec.Command("docker", "rm", "-f", container)
		if output, err := command.CombinedOutput(); err != nil && !strings.Contains(string(output), "No such container") {
			t.Errorf("remove Gitea: %v: %s", err, output)
		}
	})
	baseURL := "https://" + address
	client := giteaHTTPClient(t, certificate.CAFile)
	waitGitea(t, client, baseURL, container)
	dockerOutput(t, "exec", "--user", "git", container, "gitea", "admin", "user", "create", "--admin", "--username", "CanonicalOwner", "--password", "correct-horse-battery-staple", "--email", "owner@example.invalid", "--must-change-password=false")
	var tokenResponse struct {
		SHA1 string `json:"sha1"`
	}
	giteaJSON(t, client, http.MethodPost, baseURL+"/api/v1/users/CanonicalOwner/tokens", "", map[string]any{
		"name":   "repowolf-integration",
		"scopes": []string{"read:repository", "write:repository", "write:issue", "write:user"},
	}, &tokenResponse, "CanonicalOwner", "correct-horse-battery-staple")
	if tokenResponse.SHA1 == "" {
		t.Fatal("Gitea returned empty token")
	}
	giteaJSON(t, client, http.MethodPost, baseURL+"/api/v1/user/repos", tokenResponse.SHA1, map[string]any{
		"name": "CanonicalRepo", "description": "repository view integration", "private": true, "auto_init": true, "default_branch": "main",
	}, nil, "", "")
	return &restrictedGiteaFixture{work: work, address: address, baseURL: baseURL, container: container, certificate: certificate, client: client, token: tokenResponse.SHA1}
}

func (fixture *restrictedGiteaFixture) startBroker(t *testing.T) restrictedGiteaBroker {
	t.Helper()
	agentToken, err := auth.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binaries := testutil.BuildBinaries(t, filepath.Join(fixture.work, "bin"))
	brokerCertificate := testutil.GenerateCertificate(t, filepath.Join(fixture.work, "broker-cert"))
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	server := testutil.StartServer(t, testutil.ServerOptions{
		Binary: binaries.Service, PolicyPath: filepath.Join("testdata", "gitea-policy.yaml"), Certificate: brokerCertificate,
		SSHPath: sshPath, GiteaAPIHost: fixture.address, GiteaCAFile: fixture.certificate.CAFile,
		Environment: []string{"REPOWOLF_TOKEN_AGENT=" + agentToken, "REPOWOLF_TOKEN_GITEA=" + fixture.token},
	})
	environment := testutil.Environment(os.Environ(), "REPOWOLF_ENDPOINT="+server.Endpoint, "REPOWOLF_TOKEN="+agentToken, "REPOWOLF_CA_FILE="+server.Certificate.CAFile, "REPOWOLF_SERVER_NAME="+server.Certificate.ServerName)
	return restrictedGiteaBroker{binaries: binaries, server: server, environment: environment}
}
