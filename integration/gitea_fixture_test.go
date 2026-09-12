//go:build linux && gitea_integration

package integration_test

import (
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	work         string
	network      string
	address      string
	proxyAddress string
	baseURL      string
	container    string
	certificate  testutil.Certificate
	client       *http.Client
	token        string
	binaries     testutil.Binaries
}

type restrictedGiteaBroker struct {
	binaries    testutil.Binaries
	server      *testutil.Server
	environment []string
	agentToken  string
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
	proxyAddress := nextIPv4Address(t, address)
	certificate := testutil.GenerateCertificateForIPs(t, filepath.Join(work, "gitea-cert"), []net.IP{net.ParseIP(address), net.ParseIP(proxyAddress)})
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
	return &restrictedGiteaFixture{work: work, network: network, address: address, proxyAddress: proxyAddress, baseURL: baseURL, container: container, certificate: certificate, client: client, token: tokenResponse.SHA1}
}

func nextIPv4Address(t *testing.T, address string) string {
	t.Helper()
	value := net.ParseIP(address).To4()
	if value == nil || value[3] == 255 {
		t.Fatalf("cannot derive proxy address from %q", address)
	}
	value[3]++
	return value.String()
}

func (fixture *restrictedGiteaFixture) requestCount(t *testing.T, method, requestPath string) int {
	t.Helper()
	logs := dockerOutput(t, "logs", fixture.container)
	needle := method + " " + requestPath
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func (fixture *restrictedGiteaFixture) startBroker(t *testing.T) restrictedGiteaBroker {
	t.Helper()
	return fixture.startBrokerAt(t, fixture.address)
}

func (fixture *restrictedGiteaFixture) startBrokerAt(t *testing.T, apiHost string) restrictedGiteaBroker {
	t.Helper()
	agentToken, err := auth.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.binaries.Service == "" {
		fixture.binaries = testutil.BuildBinaries(t, filepath.Join(fixture.work, "bin"))
	}
	binaries := fixture.binaries
	brokerCertificate := testutil.GenerateCertificate(t, t.TempDir())
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	server := testutil.StartServer(t, testutil.ServerOptions{
		Binary: binaries.Service, PolicyPath: filepath.Join("testdata", "gitea-policy.yaml"), Certificate: brokerCertificate,
		SSHPath: sshPath, GiteaAPIHost: apiHost, GiteaCAFile: fixture.certificate.CAFile,
		Environment: []string{"REPOWOLF_TOKEN_AGENT=" + agentToken, "REPOWOLF_TOKEN_GITEA=" + fixture.token},
	})
	environment := testutil.Environment(os.Environ(), "REPOWOLF_ENDPOINT="+server.Endpoint, "REPOWOLF_TOKEN="+agentToken, "REPOWOLF_CA_FILE="+server.Certificate.CAFile, "REPOWOLF_SERVER_NAME="+server.Certificate.ServerName)
	return restrictedGiteaBroker{binaries: binaries, server: server, environment: environment, agentToken: agentToken}
}

func (fixture *restrictedGiteaFixture) startCorruptingWriteProxy(t *testing.T) string {
	t.Helper()
	helper := filepath.Join(fixture.work, "gitea-failure-proxy.test")
	command := exec.Command("go", "test", "-c", "-tags", "gitea_integration", "-o", helper, ".")
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Gitea failure proxy: %v: %s", err, output)
	}
	certificateDirectory := filepath.Dir(fixture.certificate.CertificateFile)
	container := dockerOutput(t, "run", "--detach", "--rm", "--network", fixture.network, "--ip", fixture.proxyAddress,
		"--volume", helper+":/helper:ro",
		"--volume", certificateDirectory+":/certs:ro",
		"--env", "REPOWOLF_GITEA_FAILURE_PROXY=1",
		"--env", "REPOWOLF_GITEA_FAILURE_PROXY_TARGET="+fixture.baseURL,
		"--env", "REPOWOLF_GITEA_FAILURE_PROXY_CERT=/certs/"+filepath.Base(fixture.certificate.CertificateFile),
		"--env", "REPOWOLF_GITEA_FAILURE_PROXY_KEY=/certs/"+filepath.Base(fixture.certificate.KeyFile),
		"--env", "REPOWOLF_GITEA_FAILURE_PROXY_CA=/certs/"+filepath.Base(fixture.certificate.CAFile),
		"--entrypoint", "/helper", giteaImage, "-test.run=^TestGiteaFailureProxyProcess$",
	)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	proxyURL := "https://" + fixture.proxyAddress
	waitGitea(t, fixture.client, proxyURL, container)
	return fixture.proxyAddress
}

func TestGiteaFailureProxyProcess(t *testing.T) {
	if os.Getenv("REPOWOLF_GITEA_FAILURE_PROXY") != "1" {
		t.Skip("helper process")
	}
	target, err := url.Parse(os.Getenv("REPOWOLF_GITEA_FAILURE_PROXY_TARGET"))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		t.Fatalf("invalid proxy target")
	}
	roots := x509.NewCertPool()
	ca, err := os.ReadFile(os.Getenv("REPOWOLF_GITEA_FAILURE_PROXY_CA"))
	if err != nil || !roots.AppendCertsFromPEM(ca) {
		t.Fatalf("load proxy CA: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method != http.MethodPost || response.Request.URL.Path != "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues" {
			return nil
		}
		if response.Body != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		response.Body = io.NopCloser(strings.NewReader("{"))
		response.ContentLength = 1
		response.Header.Set("Content-Length", "1")
		return nil
	}
	server := &http.Server{Addr: ":443", Handler: proxy, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	if err := server.ListenAndServeTLS(os.Getenv("REPOWOLF_GITEA_FAILURE_PROXY_CERT"), os.Getenv("REPOWOLF_GITEA_FAILURE_PROXY_KEY")); err != nil {
		t.Fatal(fmt.Errorf("serve Gitea failure proxy: %w", err))
	}
}
