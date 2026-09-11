package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/server"
	"github.com/rochecompaan/repowolf/internal/testutil"
	"github.com/rochecompaan/repowolf/internal/tlsconfig"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type integrationGiteaExecutor struct {
	calls      int
	repository policy.ResolvedRepository
	request    *repowolfv1.GiteaRequest
}

func (f *integrationGiteaExecutor) Execute(_ context.Context, r policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	f.calls++
	f.repository = r
	f.request = request
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: &repowolfv1.GiteaRepositoryRecord{FullName: "CanonicalOwner/CanonicalRepo", Description: "safe", DefaultBranch: "main", Url: "https://gitea.test/CanonicalOwner/CanonicalRepo", SshUrl: "git@gitea.test:CanonicalOwner/CanonicalRepo.git", CloneUrl: "https://gitea.test/CanonicalOwner/CanonicalRepo.git", Topics: []string{"safe"}, Created: timestamppb.New(now), Updated: timestamppb.New(now)}}}}, nil
}
func TestRestrictedTeaRepositoryViewThroughFakeAdapter(t *testing.T) {
	work := t.TempDir()
	certificate := testutil.GenerateCertificate(t, work)
	tlsConfig, err := tlsconfig.LoadServer(certificate.CertificateFile, certificate.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Generate(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.NewIndex(map[string][]string{"agent": {token}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.New(config.Config{Providers: map[string]config.Provider{"gitea": {Kind: config.ProviderGitea, APIHost: "gitea.test", GitHost: "gitea.test", SSHPort: 22}}, Repositories: map[string]config.Repository{"project": {Provider: "gitea", Owner: "CanonicalOwner", Name: "CanonicalRepo"}}, Principals: map[string]config.Principal{"agent": {Grants: []config.Grant{{Repository: "project", Capabilities: []config.Capability{config.RepositoryRead}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	var auditOutput bytes.Buffer
	fake := &integrationGiteaExecutor{}
	broker, err := server.New(server.Options{TLSConfig: tlsConfig, Tokens: tokens, AuditWriter: audit.NewWriter(&auditOutput), MaxConcurrentRequests: 4, MaxConcurrentRequestsPerPrincipal: 2, OperationTimeout: time.Minute, GracePeriod: time.Second, Policy: snapshot, Gitea: fake})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	broker.MarkReady()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- broker.Serve(ctx, listener); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	binaries := testutil.BuildBinaries(t, t.TempDir())
	command := exec.Command(binaries.Tea, "repos", "CanonicalOwner/CanonicalRepo", "--repo", "canonicalowner/canonicalrepo", "--output", "json")
	command.Env = testutil.Environment(os.Environ(), "REPOWOLF_ENDPOINT=https://"+listener.Addr().String(), "REPOWOLF_TOKEN="+token, "REPOWOLF_CA_FILE="+certificate.CAFile, "REPOWOLF_SERVER_NAME="+certificate.ServerName)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("tea: %v: %s", err, output)
	}
	if !strings.Contains(string(output), `"full_name":"CanonicalOwner/CanonicalRepo"`) {
		t.Fatalf("output=%s", output)
	}
	if fake.calls != 1 || fake.repository.ID != "project" || fake.request.GetContext().GetRepository().GetHost() != "" {
		t.Fatalf("fake=%#v", fake)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(auditOutput.String(), `"operation":"gitea.repository_view"`) || !strings.Contains(auditOutput.String(), `"outcome":"completed"`) {
		t.Fatalf("audit=%s", auditOutput.String())
	}
}
