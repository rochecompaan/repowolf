//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRestrictedTeaRepositoryViewAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.9.2", "172.29.9.0/24")
	giteaJSON(t, fixture.client, http.MethodPut, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/topics", fixture.token, map[string]any{"topics": []string{"repowolf", "integration"}}, nil, "", "")
	service := fixture.startBroker(t)
	binaries, broker, clientEnvironment := service.binaries, service.server, service.environment
	tokenResponse := struct {
		SHA1 string
	}{SHA1: fixture.token}
	baseURL, container := fixture.baseURL, fixture.container
	jsonOutput, jsonDiagnostic, err := runTea(binaries.Tea, clientEnvironment, "CanonicalOwner/CanonicalRepo", "json")
	if err != nil {
		t.Fatalf("tea JSON: %v: %s; broker=%s", err, jsonDiagnostic, mustRead(broker.StderrPath))
	}
	assertGiteaRepositoryJSON(t, jsonOutput)

	simpleOutput, simpleDiagnostic, err := runTea(binaries.Tea, clientEnvironment, "CanonicalOwner/CanonicalRepo", "simple")
	if err != nil {
		t.Fatalf("tea simple: %v: %s; broker=%s", err, simpleDiagnostic, mustRead(broker.StderrPath))
	}
	assertGiteaRepositorySimple(t, simpleOutput)

	deniedOutput, deniedDiagnostic, err := runTea(binaries.Tea, clientEnvironment, "CanonicalOwner/OtherRepo", "json")
	if err == nil || len(deniedOutput) != 0 || string(deniedDiagnostic) != "tea: Gitea operation failed\n" {
		t.Fatalf("denied tea = output %q, diagnostic %q, error %v", deniedOutput, deniedDiagnostic, err)
	}

	// Pausing Gitea after two successful calls forces the real SDK operation to
	// remain in flight. The client signal must cancel that operation rather than
	// waiting for the provider or the broker timeout.
	dockerOutput(t, "pause", container)
	paused := true
	defer func() {
		if paused {
			_ = exec.Command("docker", "unpause", container).Run()
		}
	}()
	cancelled := exec.Command(binaries.Tea, "repos", "CanonicalOwner/CanonicalRepo", "--repo", "canonicalowner/canonicalrepo", "--output", "json")
	cancelled.Env = clientEnvironment
	var cancelledOutput, cancelledDiagnostic bytes.Buffer
	cancelled.Stdout, cancelled.Stderr = &cancelledOutput, &cancelledDiagnostic
	if err := cancelled.Start(); err != nil {
		t.Fatal(err)
	}
	waitForAcceptedGiteaAudit(t, broker.AuditPath, 3)
	if err := cancelled.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	cancelErr := cancelled.Wait()
	dockerOutput(t, "unpause", container)
	paused = false
	if exit, ok := cancelErr.(*exec.ExitError); !ok || exit.ExitCode() != 128+int(syscall.SIGTERM) {
		t.Fatalf("cancelled tea error = %v", cancelErr)
	}
	if cancelledOutput.Len() != 0 || cancelledDiagnostic.String() != "tea: interrupted\n" {
		t.Fatalf("cancelled tea = output %q, diagnostic %q", cancelledOutput.String(), cancelledDiagnostic.String())
	}

	broker.Stop(t)
	auditBytes := mustRead(broker.AuditPath)
	acceptedFields := []string{"timestamp", "request_id", "principal", "provider", "repository", "operation", "outcome"}
	terminalFields := []string{"timestamp", "request_id", "principal", "provider", "repository", "operation", "outcome", "reason", "input_bytes", "output_bytes"}
	assertAuditInvocations(t, string(auditBytes), [][]auditExpectation{
		{
			{operation: "gitea.repository_view", outcome: "accepted", principal: "agent", provider: "gitea", repository: "project", required: acceptedFields},
			{operation: "gitea.repository_view", outcome: "completed", principal: "agent", provider: "gitea", repository: "project", reason: "OK", inputPositive: true, outputPositive: true, required: terminalFields, optional: []string{"duration_ms"}},
		},
		{
			{operation: "gitea.repository_view", outcome: "accepted", principal: "agent", provider: "gitea", repository: "project", required: acceptedFields},
			{operation: "gitea.repository_view", outcome: "completed", principal: "agent", provider: "gitea", repository: "project", reason: "OK", inputPositive: true, outputPositive: true, required: terminalFields, optional: []string{"duration_ms"}},
		},
		{{operation: "gitea.repository_view", outcome: "denied", principal: "agent", reason: "PermissionDenied", inputPositive: true, required: []string{"timestamp", "request_id", "principal", "operation", "outcome", "reason", "input_bytes"}, optional: []string{"duration_ms"}}},
		{
			{operation: "gitea.repository_view", outcome: "accepted", principal: "agent", provider: "gitea", repository: "project", required: acceptedFields},
			{operation: "gitea.repository_view", outcome: "cancelled", principal: "agent", provider: "gitea", repository: "project", reason: "Canceled", inputPositive: true, required: []string{"timestamp", "request_id", "principal", "provider", "repository", "operation", "outcome", "reason", "input_bytes"}, optional: []string{"duration_ms"}},
		},
	}, []string{tokenResponse.SHA1, "repository view integration", "repowolf, integration", baseURL + "/CanonicalOwner/CanonicalRepo"})
	for channel, contents := range map[string][]byte{
		"JSON output": jsonOutput, "simple output": simpleOutput, "denied diagnostic": deniedDiagnostic,
		"cancelled diagnostic": cancelledDiagnostic.Bytes(), "broker stderr": mustRead(broker.StderrPath),
		"Gitea logs": []byte(dockerOutput(t, "logs", container)),
	} {
		if bytes.Contains(contents, []byte(tokenResponse.SHA1)) {
			t.Fatalf("%s contains the Gitea credential", channel)
		}
	}
	for _, argument := range cancelled.Args {
		if strings.Contains(argument, tokenResponse.SHA1) {
			t.Fatal("client process argument contains the Gitea credential")
		}
	}
	for _, variable := range clientEnvironment {
		if strings.Contains(variable, tokenResponse.SHA1) {
			t.Fatal("client environment contains the Gitea credential")
		}
	}
}

func runTea(binary string, environment []string, repository, format string) ([]byte, []byte, error) {
	command := exec.Command(binary, "repos", repository, "--repo", strings.ToLower(repository), "--output", format)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func assertGiteaRepositoryJSON(t *testing.T, output []byte) {
	t.Helper()
	var repository map[string]any
	if err := json.Unmarshal(output, &repository); err != nil {
		t.Fatalf("decode tea JSON: %v: %s", err, output)
	}
	types := map[string]string{
		"full_name": "string", "description": "string", "default_branch": "string", "url": "string", "ssh_url": "string", "clone_url": "string",
		"private": "bool", "archived": "bool", "fork": "bool", "mirror": "bool", "empty": "bool",
		"stars": "number", "forks": "number", "open_issues": "number", "size": "number", "topics": "array", "created": "string", "updated": "string",
	}
	if len(repository) != len(types) {
		t.Fatalf("JSON field count = %d, want %d: %s", len(repository), len(types), output)
	}
	for field, wantType := range types {
		value, ok := repository[field]
		if !ok {
			t.Fatalf("JSON missing %q: %s", field, output)
		}
		gotType := ""
		switch value.(type) {
		case string:
			gotType = "string"
		case bool:
			gotType = "bool"
		case float64:
			gotType = "number"
		case []any:
			gotType = "array"
		}
		if gotType != wantType {
			t.Fatalf("JSON %q type = %s, want %s", field, gotType, wantType)
		}
	}
	if repository["full_name"] != "CanonicalOwner/CanonicalRepo" || repository["description"] != "repository view integration" || repository["default_branch"] != "main" || repository["private"] != true {
		t.Fatalf("unexpected canonical repository identity: %s", output)
	}
	for _, field := range []string{"url", "ssh_url", "clone_url"} {
		if repository[field] == "" {
			t.Fatalf("JSON %q is empty", field)
		}
	}
	for _, field := range []string{"created", "updated"} {
		if _, err := time.Parse(time.RFC3339, repository[field].(string)); err != nil {
			t.Fatalf("JSON %q is not UTC RFC3339: %v", field, err)
		}
	}
	topics := repository["topics"].([]any)
	if len(topics) != 2 || topics[0] != "integration" || topics[1] != "repowolf" {
		t.Fatalf("unexpected topics: %#v", topics)
	}
}

func assertGiteaRepositorySimple(t *testing.T, output []byte) {
	t.Helper()
	headers := []string{"full_name", "description", "default_branch", "url", "ssh_url", "clone_url", "private", "archived", "fork", "mirror", "empty", "stars", "forks", "open_issues", "size", "topics", "created", "updated"}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != len(headers) {
		t.Fatalf("simple line count = %d, want %d: %s", len(lines), len(headers), output)
	}
	for index, header := range headers {
		if !strings.HasPrefix(lines[index], header+": ") {
			t.Fatalf("simple line %d = %q, want %q prefix", index, lines[index], header+": ")
		}
	}
	if lines[0] != "full_name: CanonicalOwner/CanonicalRepo" || lines[1] != "description: repository view integration" || !strings.Contains(lines[15], "integration") || !strings.Contains(lines[15], "repowolf") {
		t.Fatalf("unexpected simple output: %s", output)
	}
}

func waitForAcceptedGiteaAudit(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil && strings.Count(string(contents), `"operation":"gitea.repository_view","outcome":"accepted"`) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d accepted Gitea audit events: %s", want, mustRead(path))
}

func dockerOutput(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
func giteaHTTPClient(t *testing.T, caFile string) *http.Client {
	t.Helper()
	pem, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		t.Fatal("load Gitea CA")
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}, Timeout: 5 * time.Second}
}
func waitGitea(t *testing.T, client *http.Client, baseURL, container string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/api/v1/version")
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	logs := dockerOutput(t, "logs", container)
	t.Fatalf("Gitea readiness timeout: %s", logs)
}
func giteaJSON(t *testing.T, client *http.Client, method, url, token string, input any, output any, user, password string) {
	t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, url, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "token "+token)
	}
	if user != "" {
		request.SetBasicAuth(user, password)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode/100 != 2 {
		t.Fatalf("Gitea %s %s: %s: %s", method, url, response.Status, body)
	}
	if output != nil {
		if err := json.Unmarshal(body, output); err != nil {
			t.Fatalf("decode Gitea response: %v: %s", err, body)
		}
	}
}
