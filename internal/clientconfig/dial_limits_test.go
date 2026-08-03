package clientconfig

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
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
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestDialEnforcesExactOneMiBMessageLimits(t *testing.T) {
	service := &limitGitHubService{response: sizedResponse(t, 64)}
	endpoint, caFile := startLimitTLSServer(t, service)
	connection, err := Dial(context.Background(), Config{Endpoint: endpoint, Token: testToken, CAFile: caFile, ServerName: "repowolf.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	client := repowolfv1.NewGitHubServiceClient(connection)

	t.Run("send exact limit succeeds", func(t *testing.T) {
		if _, err := client.Execute(context.Background(), sizedRequest(t, messageLimitBytes)); err != nil {
			t.Fatalf("exact-limit send failed: %v", err)
		}
	})
	t.Run("send over limit is rejected", func(t *testing.T) {
		if _, err := client.Execute(context.Background(), sizedRequest(t, messageLimitBytes+1)); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("over-limit send error = %v", err)
		}
	})
	t.Run("receive exact limit succeeds", func(t *testing.T) {
		service.setResponse(sizedResponse(t, messageLimitBytes))
		if _, err := client.Execute(context.Background(), sizedRequest(t, 64)); err != nil {
			t.Fatalf("exact-limit receive failed: %v", err)
		}
	})
	t.Run("receive over limit is rejected", func(t *testing.T) {
		service.setResponse(sizedResponse(t, messageLimitBytes+1))
		if _, err := client.Execute(context.Background(), sizedRequest(t, 64)); status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("over-limit receive error = %v", err)
		}
	})
}

func sizedRequest(t *testing.T, size int) *repowolfv1.GitHubRequest {
	t.Helper()
	request := &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{}}}
	sizedString(t, size, func(value string) { request.GetIssueComment().Body = value }, func() int { return proto.Size(request) })
	return request
}

func sizedResponse(t *testing.T, size int) *repowolfv1.GitHubResponse {
	t.Helper()
	response := &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentResult{Comment: &repowolfv1.GitHubCommentRecord{}}}}
	sizedString(t, size, func(value string) { response.GetIssueComment().Comment.Body = value }, func() int { return proto.Size(response) })
	return response
}

func sizedString(t *testing.T, target int, set func(string), size func() int) {
	t.Helper()
	value := strings.Repeat("x", target)
	for attempts := 0; attempts < 8; attempts++ {
		set(value)
		current := size()
		if current == target {
			return
		}
		next := len(value) + target - current
		if next < 0 {
			t.Fatalf("cannot build message of size %d", target)
		}
		value = strings.Repeat("x", next)
	}
	t.Fatalf("could not build exact message size %d (got %d)", target, size())
}

type limitGitHubService struct {
	repowolfv1.UnimplementedGitHubServiceServer
	mu       sync.Mutex
	response *repowolfv1.GitHubResponse
}

func (service *limitGitHubService) Execute(context.Context, *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.response, nil
}

func (service *limitGitHubService) setResponse(response *repowolfv1.GitHubResponse) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.response = response
}

func startLimitTLSServer(t *testing.T, service repowolfv1.GitHubServiceServer) (string, string) {
	t.Helper()
	certificate, caPEM := limitTLSCertificate(t, "repowolf.test")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS13})))
	repowolfv1.RegisterGitHubServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return "https://" + listener.Addr().String(), caFile
}

func limitTLSCertificate(t *testing.T, serverName string) (tls.Certificate, []byte) {
	t.Helper()
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(201), Subject: pkix.Name{CommonName: "limit test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
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
	serverTemplate := &x509.Certificate{SerialNumber: big.NewInt(202), DNSNames: []string{serverName}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCertificate, serverKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{serverDER}, PrivateKey: serverKey}, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
}
