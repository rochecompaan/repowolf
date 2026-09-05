package integration_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

func TestBrokerRejectsDuplicateCredentialBeforeReadiness(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	binaries := testutil.BuildBinaries(t, binDir)
	provider := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := testutil.InstallExecutable(t, filepath.Join("testdata", "fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := testutil.GenerateCertificate(t, filepath.Join(root, "tls"))
	failure := testutil.StartServerFailure(t, testutil.ServerOptions{
		Binary:      binaries.Service,
		PolicyPath:  filepath.Join("testdata", "policy.yaml"),
		Certificate: certificate,
		GHPath:      provider,
		SSHPath:     ssh,
		Environment: []string{
			"REPOWOLF_TOKEN_AGENT=" + agentToken,
			"REPOWOLF_TOKEN_GITHUB=" + agentToken,
			"REPOWOLF_TOKEN_GITEA=" + giteaCredential,
		},
	})
	if !strings.Contains(failure, "service failed") {
		t.Fatalf("startup failure = %q", failure)
	}
	if strings.Contains(failure, agentToken) || strings.Contains(failure, giteaCredential) {
		t.Fatalf("startup failure disclosed a credential: %q", failure)
	}
}
