//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/clientconfig"
	"github.com/rochecompaan/repowolf/internal/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

func TestRestrictedTeaIssueWritesAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.11.2", "172.29.11.0/24")
	type resource struct {
		ID int64 `json:"id"`
	}
	var label resource
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/labels", fixture.token, map[string]any{"name": "bug", "color": "ee0701"}, &label, "", "")
	service := fixture.startBroker(t)
	defer service.server.Stop(t)
	issuePath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues"
	createBefore := fixture.requestCount(t, http.MethodPost, issuePath)
	created := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "write integration", "-d", "private write body", "-a", "CanonicalOwner", "-L", "bug", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPost, issuePath) - createBefore; got != 1 {
		t.Fatalf("create request count=%d", got)
	}
	var issue struct {
		Index int64  `json:"index"`
		State string `json:"state"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(created, &issue); err != nil || issue.Index <= 0 || issue.State != "open" || issue.Title != "write integration" {
		t.Fatalf("create=%s err=%v", created, err)
	}
	index := strconv.FormatInt(issue.Index, 10)
	commentPath := issuePath + "/" + index + "/comments"
	commentBefore := fixture.requestCount(t, http.MethodPost, commentPath)
	comment := runTeaArgs(t, service.binaries.Tea, service.environment, "comments", "add", index, "private comment body", "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPost, commentPath) - commentBefore; got != 1 {
		t.Fatalf("comment request count=%d", got)
	}
	var commentValue struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(comment, &commentValue); err != nil || commentValue.ID <= 0 || commentValue.Body != "private comment body" {
		t.Fatalf("comment=%s err=%v", comment, err)
	}
	statePath := issuePath + "/" + index
	stateBefore := fixture.requestCount(t, http.MethodPatch, statePath)
	closed := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if err := json.Unmarshal(closed, &issue); err != nil || issue.State != "closed" {
		t.Fatalf("close=%s err=%v", closed, err)
	}
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 1 {
		t.Fatalf("first close PATCH count=%d", got)
	}
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 1 {
		t.Fatalf("no-op close changed PATCH count to %d", got)
	}
	reopened := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "reopen", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 2 {
		t.Fatalf("reopen total PATCH count=%d", got)
	}
	if err := json.Unmarshal(reopened, &issue); err != nil || issue.State != "open" {
		t.Fatalf("reopen=%s err=%v", reopened, err)
	}
	var independent struct {
		State    string `json:"state"`
		Body     string `json:"body"`
		Comments int    `json:"comments"`
		Labels   []struct {
			Name string `json:"name"`
		} `json:"labels"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &independent, "", "")
	if independent.State != "open" || independent.Body != "private write body" || independent.Comments != 1 || len(independent.Labels) != 1 || independent.Labels[0].Name != "bug" || len(independent.Assignees) != 1 || independent.Assignees[0].Login != "CanonicalOwner" {
		t.Fatalf("independent=%#v", independent)
	}
	var independentComments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+commentPath, fixture.token, nil, &independentComments, "", "")
	if len(independentComments) != 1 || independentComments[0].ID != commentValue.ID || independentComments[0].Body != "private comment body" {
		t.Fatalf("independent comments=%#v", independentComments)
	}

	auditContents, err := os.ReadFile(service.server.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := parseAuditRecords(auditContents, []string{"private write body", "private comment body", "bug", "CanonicalOwner", fixture.token, `"index"`})
	if err != nil {
		t.Fatal(err)
	}
	operations := []string{"gitea.issue_create", "gitea.issue_comment", "gitea.issue_close", "gitea.issue_close", "gitea.issue_reopen"}
	if len(records) != len(operations)*2 {
		t.Fatalf("audit record count=%d", len(records))
	}
	wantTransitions := []*bool{nil, nil, boolPointer(true), boolPointer(false), boolPointer(true)}
	for invocation, operation := range operations {
		accepted, terminal := records[invocation*2], records[invocation*2+1]
		if accepted.event.RequestID == "" || accepted.event.RequestID != terminal.event.RequestID || accepted.event.Operation != operation || terminal.event.Operation != operation || accepted.event.Outcome != "accepted" || terminal.event.Outcome != "completed" || terminal.event.Reason != "OK" {
			t.Fatalf("audit invocation %d: accepted=%#v terminal=%#v", invocation, accepted.event, terminal.event)
		}
		want := wantTransitions[invocation]
		if (want == nil) != (terminal.event.Transitioned == nil) || want != nil && *want != *terminal.event.Transitioned {
			t.Fatalf("audit invocation %d transitioned=%v want=%v", invocation, terminal.event.Transitioned, want)
		}
	}
}

func TestGiteaWriteOutcomeUnknownAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.12.2", "172.29.12.0/24")
	issuePath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues"

	t.Run("provider response corruption", func(t *testing.T) {
		proxyAddress := fixture.startCorruptingWriteProxy(t)
		service := fixture.startBrokerAt(t, proxyAddress)
		defer service.server.Stop(t)
		before := fixture.requestCount(t, http.MethodPost, issuePath)
		stdout, stderr := runTeaWriteFailure(t, service.binaries.Tea, service.environment,
			"issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "provider response corruption", "-o", "json")
		if len(stdout) != 0 || stderr != "tea: write outcome unknown; inspect repository state before retrying\n" {
			t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
		}
		if got := fixture.requestCount(t, http.MethodPost, issuePath) - before; got != 1 {
			t.Fatalf("provider create count=%d, want 1", got)
		}
		assertPersistedIssue(t, fixture, "provider response corruption")
		assertIssueCreateAuditOutcome(t, service.server.AuditPath, "unknown")
	})

	t.Run("post completion grpc response loss", func(t *testing.T) {
		service := fixture.startBroker(t)
		defer service.server.Stop(t)
		environment, calls := startGiteaResponseLossProxy(t, service)
		before := fixture.requestCount(t, http.MethodPost, issuePath)
		stdout, stderr := runTeaWriteFailure(t, service.binaries.Tea, environment,
			"issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "grpc response loss", "-o", "json")
		if len(stdout) != 0 || stderr != "tea: write outcome unknown; inspect repository state before retrying\n" {
			t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("gRPC forwarding handler count=%d, want 1", got)
		}
		if got := fixture.requestCount(t, http.MethodPost, issuePath) - before; got != 1 {
			t.Fatalf("provider create count=%d, want 1", got)
		}
		assertPersistedIssue(t, fixture, "grpc response loss")
		assertIssueCreateAuditOutcome(t, service.server.AuditPath, "completed")
	})
}

type giteaResponseLossService struct {
	repowolfv1.UnimplementedGiteaServiceServer
	upstream repowolfv1.GiteaServiceClient
	calls    *atomic.Int32
}

func (service *giteaResponseLossService) Execute(ctx context.Context, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	service.calls.Add(1)
	if _, err := service.upstream.Execute(ctx, request); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unavailable, "simulated post-completion response loss")
}

func startGiteaResponseLossProxy(t *testing.T, broker restrictedGiteaBroker) ([]string, *atomic.Int32) {
	t.Helper()
	connection, err := clientconfig.Dial(context.Background(), clientconfig.Config{
		Endpoint: broker.server.Endpoint, Token: broker.agentToken,
		CAFile: broker.server.Certificate.CAFile, ServerName: broker.server.Certificate.ServerName,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	certificate := testutil.GenerateCertificate(t, t.TempDir())
	keyPair, err := tls.LoadX509KeyPair(certificate.CertificateFile, certificate.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{keyPair}})))
	calls := &atomic.Int32{}
	repowolfv1.RegisterGiteaServiceServer(server, &giteaResponseLossService{upstream: repowolfv1.NewGiteaServiceClient(connection), calls: calls})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	environment := testutil.Environment(broker.environment,
		"REPOWOLF_ENDPOINT=https://"+listener.Addr().String(),
		"REPOWOLF_CA_FILE="+certificate.CAFile,
		"REPOWOLF_SERVER_NAME="+certificate.ServerName,
	)
	return environment, calls
}

func runTeaWriteFailure(t *testing.T, binary string, environment []string, args ...string) ([]byte, string) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitError, ok := err.(*exec.ExitError)
	if !ok || exitError.ExitCode() != 1 {
		t.Fatalf("tea %v exit error=%v, want exit 1", args, err)
	}
	return stdout.Bytes(), stderr.String()
}

func assertPersistedIssue(t *testing.T, fixture *restrictedGiteaFixture, title string) {
	t.Helper()
	var issues []struct {
		Index int64  `json:"number"`
		Title string `json:"title"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues?state=all&limit=50", fixture.token, nil, &issues, "", "")
	for _, issue := range issues {
		if issue.Title == title && issue.Index > 0 {
			return
		}
	}
	t.Fatalf("persisted issue %q not found: %#v", title, issues)
}

func assertIssueCreateAuditOutcome(t *testing.T, path, outcome string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := parseAuditRecords(contents, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].event.Operation != "gitea.issue_create" || records[0].event.Outcome != "accepted" || records[1].event.Operation != "gitea.issue_create" || string(records[1].event.Outcome) != outcome {
		t.Fatalf("audit records=%#v, want accepted/%s", records, outcome)
	}
}

func boolPointer(value bool) *bool { return &value }
