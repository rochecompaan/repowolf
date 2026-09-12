//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
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
	container := dockerOutput(t, "run", "--detach", "--rm", "--network", network, "--ip", address, "--volume", filepath.Dir(cert.CertificateFile)+":/certs:ro", "--env", "GITEA__database__DB_TYPE=sqlite3", "--env", "GITEA__security__INSTALL_LOCK=true", "--env", "GITEA__server__PROTOCOL=https", "--env", "GITEA__server__HTTP_PORT=443", "--env", "GITEA__server__SSL_MIN_VERSION=TLSv1.3", "--env", "GITEA__server__SSL_MAX_VERSION=TLSv1.3", "--env", "GITEA__server__CERT_FILE=/certs/server.pem", "--env", "GITEA__server__KEY_FILE=/certs/server-key.pem", "--env", "GITEA__api__MAX_RESPONSE_ITEMS=50", giteaImage)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	baseURL := "https://" + address
	httpClient := giteaHTTPClient(t, cert.CAFile)
	waitGitea(t, httpClient, baseURL, container)
	dockerOutput(t, "exec", "--user", "git", container, "gitea", "admin", "user", "create", "--admin", "--username", "CanonicalOwner", "--password", "correct-horse-battery-staple", "--email", "owner@example.invalid", "--must-change-password=false")
	var token struct {
		SHA1 string `json:"sha1"`
	}
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/users/CanonicalOwner/tokens", "", map[string]any{"name": "issues", "scopes": []string{"write:repository", "write:issue", "write:user"}}, &token, "CanonicalOwner", "correct-horse-battery-staple")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/user/repos", token.SHA1, map[string]any{"name": "CanonicalRepo", "private": true, "auto_init": true}, nil, "", "")
	type createdIssue struct {
		Index int64 `json:"number"`
	}
	var open, closed createdIssue
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "open issue", "body": "private issue body"}, &open, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "closed issue", "body": "closed body"}, &closed, "", "")
	giteaJSON(t, httpClient, http.MethodPatch, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(closed.Index, 10), token.SHA1, map[string]any{"state": "closed"}, nil, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/branches", token.SHA1, map[string]any{"new_branch_name": "pull-fixture", "old_branch_name": "main"}, nil, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/contents/pull-fixture.txt", token.SHA1, map[string]any{"branch": "pull-fixture", "message": "seed pull request", "content": base64.StdEncoding.EncodeToString([]byte("pull request fixture\n"))}, nil, "", "")
	var pull createdIssue
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/pulls", token.SHA1, map[string]any{"base": "main", "head": "pull-fixture", "title": "pull request"}, &pull, "", "")
	for i := 1; i <= 51; i++ {
		giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(open.Index, 10)+"/comments", token.SHA1, map[string]any{"body": "comment " + strconv.Itoa(i)}, nil, "", "")
	}
	commentURL := baseURL + "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(open.Index, 10) + "/timeline"
	type commentPageRecord struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	var firstCommentPage, secondCommentPage []commentPageRecord
	giteaJSON(t, httpClient, http.MethodGet, commentURL+"?page=1&limit=50", token.SHA1, nil, &firstCommentPage, "", "")
	giteaJSON(t, httpClient, http.MethodGet, commentURL+"?page=2&limit=50", token.SHA1, nil, &secondCommentPage, "", "")
	if len(firstCommentPage) != 50 || len(secondCommentPage) != 1 || secondCommentPage[0].ID <= firstCommentPage[49].ID {
		t.Fatalf("Gitea comment pagination returned page sizes %d and %d", len(firstCommentPage), len(secondCommentPage))
	}
	for _, comment := range append(firstCommentPage, secondCommentPage...) {
		if comment.Type != "comment" {
			t.Fatalf("Gitea timeline returned unexpected entry type %q", comment.Type)
		}
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
	wantRows := []struct {
		index    int64
		title    string
		state    string
		comments float64
	}{
		{index: closed.Index, title: "closed issue", state: "closed", comments: 0},
		{index: open.Index, title: "open issue", state: "open", comments: 51},
	}
	for i, row := range rows {
		if len(row) != 4 || row["index"] != float64(wantRows[i].index) || row["title"] != wantRows[i].title || row["state"] != wantRows[i].state || row["comments"] != wantRows[i].comments {
			t.Fatalf("row %d = %#v, want %#v", i, row, wantRows[i])
		}
	}
	openList := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "open", "--page", "1", "--limit", "50", "--fields", "index,state", "--output", "json")
	var openRows []map[string]any
	if err := json.Unmarshal(openList, &openRows); err != nil || len(openRows) != 1 || openRows[0]["index"] != float64(open.Index) || openRows[0]["state"] != "open" {
		t.Fatalf("open list=%s err=%v", openList, err)
	}
	emptyList := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "all", "--page", "2", "--limit", "50", "--fields", "index", "--output", "json")
	if string(emptyList) != "[]\n" {
		t.Fatalf("empty page = %q", emptyList)
	}
	viewCommand := exec.Command(binaries.Tea, "issues", strconv.FormatInt(open.Index, 10), "--repo", "CanonicalOwner/CanonicalRepo", "--comments", "--output", "json")
	viewCommand.Env = env
	var viewOutput, viewDiagnostic bytes.Buffer
	viewCommand.Stdout, viewCommand.Stderr = &viewOutput, &viewDiagnostic
	if err := viewCommand.Run(); err != nil {
		t.Fatalf("view: %v: %s; broker=%s; audit=%s", err, viewDiagnostic.String(), mustRead(broker.StderrPath), mustRead(broker.AuditPath))
	}
	view := viewOutput.Bytes()
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
	deniedOutput, deniedDiagnostic := runTeaFailure(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/OtherRepo", "--output", "json")
	if len(deniedOutput) != 0 || deniedDiagnostic != "tea: Gitea operation failed\n" {
		t.Fatalf("denied output=%q diagnostic=%q", deniedOutput, deniedDiagnostic)
	}
	pullOutput, pullDiagnostic := runTeaFailure(t, binaries.Tea, env, "issues", strconv.FormatInt(pull.Index, 10), "--repo", "CanonicalOwner/CanonicalRepo", "--comments", "--output", "json")
	if len(pullOutput) != 0 || pullDiagnostic != "tea: index is a pull request; use tea pulls\n" {
		t.Fatalf("pull output=%q diagnostic=%q broker=%s audit=%s", pullOutput, pullDiagnostic, mustRead(broker.StderrPath), mustRead(broker.AuditPath))
	}
	broker.Stop(t)
	audit := string(mustRead(broker.AuditPath))
	if !strings.Contains(audit, `"operation":"gitea.issue_list"`) || !strings.Contains(audit, `"operation":"gitea.issue_view"`) || strings.Contains(audit, "private issue body") {
		t.Fatalf("unexpected audit: %s", audit)
	}
	logs := dockerOutput(t, "logs", container)
	commentPath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(open.Index, 10) + "/timeline"
	if !giteaLogContainsBoundedGET(logs, commentPath, 1, 50) || !giteaLogContainsBoundedGET(logs, commentPath, 2, 50) {
		t.Fatalf("Gitea log does not contain explicit comment pages for %s", commentPath)
	}
	pullCommentPath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(pull.Index, 10) + "/timeline"
	if strings.Contains(logs, "GET "+pullCommentPath) || strings.Contains(logs, "/CanonicalOwner/OtherRepo/issues") {
		t.Fatalf("unexpected provider request in Gitea logs")
	}
}

func giteaLogContainsBoundedGET(logs, path string, page, limit int) bool {
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "GET "+path) && strings.Contains(line, "page="+strconv.Itoa(page)) && strings.Contains(line, "limit="+strconv.Itoa(limit)) {
			return true
		}
	}
	return false
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

func runTeaFailure(t *testing.T, binary string, env []string, args ...string) ([]byte, string) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatalf("tea %v unexpectedly succeeded: %s", args, stdout.String())
	}
	return stdout.Bytes(), stderr.String()
}
