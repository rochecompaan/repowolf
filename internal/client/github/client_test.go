package github

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestRenderNativeIssueListAndSelectedJSON(t *testing.T) {
	response := issueListFixture()
	native, err := render(command{kind: operationIssueList}, response)
	if err != nil {
		t.Fatal(err)
	}
	if want := "1\tFirst issue\tOPEN\tbug, help wanted\t2025-01-02T03:04:05Z\n"; string(native) != want {
		t.Fatalf("native output = %q, want %q", native, want)
	}
	jsonOutput, err := render(command{kind: operationIssueList, fields: []string{"number", "title"}}, response)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"number":1,"title":"First issue"}]` + "\n"; string(jsonOutput) != want {
		t.Fatalf("JSON output = %q, want %q", jsonOutput, want)
	}
}

func TestRenderSanitizesUnicodeControlsOnlyInNativeOutput(t *testing.T) {
	hostile := "start\x00\b\t\n\r\x1b\x7f\u0085\u009bend"
	response := &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueView{IssueView: &repowolfv1.GitHubIssueViewResult{Issue: &repowolfv1.GitHubIssueRecord{
		Number: 7, Title: hostile, State: "OPEN", Author: "octocat", Body: &hostile,
	}}}}
	native, err := render(command{kind: operationIssueView}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantCell := "start" + strings.Repeat(" ", 9) + "end"
	wantNative := "title:\t" + wantCell + "\nstate:\tOPEN\nauthor:\toctocat\nlabels:\t\nassignees:\t\nnumber:\t7\nurl:\t\n--\n" + wantCell + "\n"
	if string(native) != wantNative {
		t.Fatalf("native output = %q, want %q", native, wantNative)
	}

	jsonOutput, err := render(command{kind: operationIssueView, fields: []string{"title", "body"}}, response)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(jsonOutput) {
		t.Fatalf("invalid JSON output %q", jsonOutput)
	}
	var decoded map[string]string
	if err := json.Unmarshal(jsonOutput, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != hostile || decoded["body"] != hostile {
		t.Fatalf("JSON controls were altered: %#v", decoded)
	}
}

func TestExecuteUsesTypedRPCAndRendersResponse(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	service := &recordingGitHubService{response: issueListFixture()}
	repowolfv1.RegisterGitHubServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	ctx := context.Background()
	connection, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	parsed, err := parseArgs([]string{"issue", "list", "--repo", "owner/repo"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := executeCommand(ctx, repowolfv1.NewGitHubServiceClient(connection), parsed, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "1\tFirst issue\tOPEN\tbug, help wanted\t2025-01-02T03:04:05Z\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if service.request.GetIssueList() == nil || service.request.GetContext().GetRepository().GetOwner() != "owner" {
		t.Fatalf("request = %#v", service.request)
	}
}

func TestRunUsesRealTLSBearerAuthenticationAndRendering(t *testing.T) {
	serverName := "repowolf.test"
	certificate, caPEM := newTLSCertificate(t, serverName)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	service := &recordingGitHubService{response: issueListFixture(), token: testClientToken}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})))
	repowolfv1.RegisterGitHubServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	setRunEnv(t, "https://"+listener.Addr().String(), testClientToken, caFile, serverName)
	var stdout, stderr bytes.Buffer
	statusCode := Run(context.Background(), []string{"issue", "list", "--json", "number,title", "--repo", "owner/repo"}, &stdout, &stderr)
	if statusCode != 0 || stdout.String() != `[{"number":1,"title":"First issue"}]`+"\n" || stderr.Len() != 0 {
		t.Fatalf("Run() = %d, stdout=%q stderr=%q", statusCode, stdout.String(), stderr.String())
	}
	if !service.authenticated() {
		t.Fatal("server did not observe bearer authentication")
	}

	setRunEnv(t, "https://"+listener.Addr().String(), testClientToken, caFile, "wrong.example")
	stdout.Reset()
	stderr.Reset()
	wrongNameContext, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if statusCode := Run(wrongNameContext, []string{"repo", "view", "--repo", "owner/repo"}, &stdout, &stderr); statusCode != 1 || stdout.Len() != 0 || stderr.String() != "gh: connection failed\n" {
		t.Fatalf("wrong-name Run() = %d, stdout=%q stderr=%q", statusCode, stdout.String(), stderr.String())
	}

	setRunEnv(t, "https://"+listener.Addr().String(), testClientToken, caFile, serverName)
	stdout.Reset()
	stderr.Reset()
	statusCode = Run(context.Background(), []string{"issue", "list", "--repo", "denied/repo"}, &stdout, &stderr)
	if statusCode != 1 || stdout.Len() != 0 || stderr.String() != "gh: GitHub operation failed\n" {
		t.Fatalf("denied Run() = %d, stdout=%q stderr=%q", statusCode, stdout.String(), stderr.String())
	}
}

func TestRunFailsAtSafeBoundaries(t *testing.T) {
	t.Run("unsupported command before configuration", func(t *testing.T) {
		setRunEnv(t, "", "", "", "")
		var stderr bytes.Buffer
		if statusCode := Run(context.Background(), []string{"api", "/user"}, &bytes.Buffer{}, &stderr); statusCode != 2 || stderr.String() != "gh: unsupported or invalid command\n" {
			t.Fatalf("Run() = %d, stderr=%q", statusCode, stderr.String())
		}
	})
	t.Run("missing token", func(t *testing.T) {
		setRunEnv(t, "https://127.0.0.1:1", "", "", "")
		var stderr bytes.Buffer
		if statusCode := Run(context.Background(), []string{"repo", "view", "--repo", "owner/repo"}, &bytes.Buffer{}, &stderr); statusCode != 1 || stderr.String() != "gh: client configuration failed\n" {
			t.Fatalf("Run() = %d, stderr=%q", statusCode, stderr.String())
		}
	})
	t.Run("invalid CA", func(t *testing.T) {
		caFile := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(caFile, []byte("invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
		setRunEnv(t, "https://127.0.0.1:1", testClientToken, caFile, "")
		var stderr bytes.Buffer
		if statusCode := Run(context.Background(), []string{"repo", "view", "--repo", "owner/repo"}, &bytes.Buffer{}, &stderr); statusCode != 1 || stderr.String() != "gh: connection failed\n" {
			t.Fatalf("Run() = %d, stderr=%q", statusCode, stderr.String())
		}
	})
}

const testClientToken = "rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func setRunEnv(t *testing.T, endpoint, token, caFile, serverName string) {
	t.Helper()
	t.Setenv("REPOWOLF_ENDPOINT", endpoint)
	t.Setenv("REPOWOLF_TOKEN", token)
	t.Setenv("REPOWOLF_CA_FILE", caFile)
	t.Setenv("REPOWOLF_SERVER_NAME", serverName)
}

func issueListFixture() *repowolfv1.GitHubResponse {
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueList{IssueList: &repowolfv1.GitHubIssueListResult{Issues: []*repowolfv1.GitHubIssueRecord{{
		Number: 1, Title: "First issue", State: "OPEN", Author: "octocat", Labels: []string{"bug", "help wanted"}, Url: "https://github.example/owner/repo/issues/1", UpdatedAt: "2025-01-02T03:04:05Z",
	}}}}}
}

type recordingGitHubService struct {
	repowolfv1.UnimplementedGitHubServiceServer
	mu       sync.Mutex
	request  *repowolfv1.GitHubRequest
	response *repowolfv1.GitHubResponse
	token    string
	sawAuth  bool
}

func (service *recordingGitHubService) Execute(ctx context.Context, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.request = request
	if request.GetContext().GetRepository().GetOwner() == "denied" {
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
	if service.token != "" {
		values := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(values) != 1 || values[0] != "Bearer "+service.token {
			return nil, status.Error(codes.Unauthenticated, "authentication required")
		}
		service.sawAuth = true
	}
	return service.response, nil
}

func (service *recordingGitHubService) authenticated() bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.sawAuth
}

func newTLSCertificate(t *testing.T, serverName string) (tls.Certificate, []byte) {
	t.Helper()
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RepoWolf test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	_, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCertificate, serverKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}
