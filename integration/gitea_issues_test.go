//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/testutil"
)

func TestRestrictedTeaReadOperationsAgainstGitea(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("Docker required: %v", err)
	}
	work := t.TempDir()
	address := "172.29.10.2"
	network := "repowolf-gitea-issues"
	dockerOutput(t, "network", "create", "--subnet", "172.29.10.0/24", network)
	t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", network).Run() })
	cert := testutil.GenerateCertificateForIPs(t, filepath.Join(work, "gitea-cert"), []net.IP{net.ParseIP(address)})
	if err := os.Chmod(filepath.Dir(cert.CertificateFile), 0755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{cert.CertificateFile, cert.KeyFile} {
		if err := os.Chmod(p, 0644); err != nil {
			t.Fatal(err)
		}
	}
	container := dockerOutput(t, "run", "--detach", "--rm", "--network", network, "--ip", address, "--volume", filepath.Dir(cert.CertificateFile)+":/certs:ro", "--env", "GITEA__database__DB_TYPE=sqlite3", "--env", "GITEA__security__INSTALL_LOCK=true", "--env", "GITEA__server__PROTOCOL=https", "--env", "GITEA__server__HTTP_PORT=443", "--env", "GITEA__server__SSL_MIN_VERSION=TLSv1.3", "--env", "GITEA__server__SSL_MAX_VERSION=TLSv1.3", "--env", "GITEA__server__CERT_FILE=/certs/server.pem", "--env", "GITEA__server__KEY_FILE=/certs/server-key.pem", giteaImage)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	baseURL := "https://" + address
	httpClient := giteaHTTPClient(t, cert.CAFile)
	waitGitea(t, httpClient, baseURL, container)
	dockerOutput(t, "exec", "--user", "git", container, "gitea", "admin", "user", "create", "--admin", "--username", "CanonicalOwner", "--password", "correct-horse-battery-staple", "--email", "owner@example.invalid", "--must-change-password=false")
	var token struct {
		SHA1 string `json:"sha1"`
	}
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/users/CanonicalOwner/tokens", "", map[string]any{"name": "issues", "scopes": []string{"write:repository", "write:issue"}}, &token, "CanonicalOwner", "correct-horse-battery-staple")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/user/repos", token.SHA1, map[string]any{"name": "CanonicalRepo", "private": true, "auto_init": true}, nil, "", "")
	type createdIssue struct {
		Index int64 `json:"number"`
	}
	var open, closed createdIssue
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "open issue", "body": "private issue body"}, &open, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "closed issue", "body": "closed body"}, &closed, "", "")
	giteaJSON(t, httpClient, http.MethodPatch, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(closed.Index, 10), token.SHA1, map[string]any{"state": "closed"}, nil, "", "")
	for i := 1; i <= 51; i++ {
		giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(open.Index, 10)+"/comments", token.SHA1, map[string]any{"body": "comment " + strconv.Itoa(i)}, nil, "", "")
	}
	agentToken, err := auth.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	binaries := testutil.BuildBinaries(t, filepath.Join(work, "bin"))
	brokerCert := testutil.GenerateCertificate(t, filepath.Join(work, "broker-cert"))
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	broker := testutil.StartServer(t, testutil.ServerOptions{Binary: binaries.Service, PolicyPath: filepath.Join("testdata", "gitea-policy.yaml"), Certificate: brokerCert, SSHPath: sshPath, GiteaAPIHost: address, GiteaCAFile: cert.CAFile, Environment: []string{"REPOWOLF_TOKEN_AGENT=" + agentToken, "REPOWOLF_TOKEN_GITEA=" + token.SHA1}})
	env := testutil.Environment(os.Environ(), "REPOWOLF_ENDPOINT="+broker.Endpoint, "REPOWOLF_TOKEN="+agentToken, "REPOWOLF_CA_FILE="+broker.Certificate.CAFile, "REPOWOLF_SERVER_NAME="+broker.Certificate.ServerName)
	list := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "all", "--page", "1", "--limit", "2", "--fields", "index,title,state,comments", "--output", "json")
	var rows []map[string]any
	if err := json.Unmarshal(list, &rows); err != nil || len(rows) != 2 {
		t.Fatalf("list=%s err=%v", list, err)
	}
	for _, row := range rows {
		if len(row) != 4 || row["index"] == nil || row["title"] == nil || row["state"] == nil || row["comments"] == nil {
			t.Fatalf("row=%#v", row)
		}
	}
	view := runTeaArgs(t, binaries.Tea, env, "issues", strconv.FormatInt(open.Index, 10), "--repo", "CanonicalOwner/CanonicalRepo", "--comments", "--output", "json")
	var detail struct {
		Index    int64 `json:"index"`
		Comments []struct {
			ID int64 `json:"id"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(view, &detail); err != nil || detail.Index != open.Index || len(detail.Comments) != 51 {
		t.Fatalf("view=%s err=%v", view, err)
	}
	for i, c := range detail.Comments {
		if i > 0 && c.ID <= detail.Comments[i-1].ID {
			t.Fatal("comments out of order")
		}
	}
	broker.Stop(t)
	audit := string(mustRead(broker.AuditPath))
	if !strings.Contains(audit, `"operation":"gitea.issue_list"`) || !strings.Contains(audit, `"operation":"gitea.issue_view"`) || strings.Contains(audit, "private issue body") {
		t.Fatalf("unexpected audit: %s", audit)
	}
}
func runTeaArgs(t *testing.T, binary string, env []string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("tea %v: %v: %s", args, err, stderr.String())
	}
	return stdout.Bytes()
}
