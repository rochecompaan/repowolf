package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rochecompaan/repowolf/internal/cli"
)

func TestRunConfigValidatePerformsOnlyStructuralValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repowolf.yaml")
	if err := os.WriteFile(path, []byte(validConfigYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cli.RunConfigValidate([]string{"--config", path}); err != nil {
		t.Fatalf("RunConfigValidate() error = %v", err)
	}
}

func TestRunConfigValidateRejectsInvalidArguments(t *testing.T) {
	if err := cli.RunConfigValidate(nil); err != cli.ErrInvalidConfigArguments {
		t.Fatalf("RunConfigValidate() error = %v", err)
	}
}

func validConfigYAML() string {
	return `apiVersion: repowolf.dev/v1alpha1
listen: 127.0.0.1:8443
tls:
  certificate: /missing/server.crt
  privateKey: /missing/server.key
providers:
  github:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
repositories:
  project:
    provider: github
    owner: alpha
    name: project
    git:
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs: [REPOWOLF_TOKEN_AGENT]
    grants:
      - repository: project
        capabilities: [repository:read]
`
}
