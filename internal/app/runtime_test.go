package app_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/app"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/tlsconfig"
)

func TestNewRuntimeBuildsImmutableDependenciesAndSafeProviderEnvironment(t *testing.T) {
	configPath, token := runtimeFixture(t)
	t.Setenv("REPOWOLF_TOKEN_AGENT", token)
	t.Setenv("REPOWOLF_INTERNAL_CONTROL", "remove-me")
	t.Setenv("GH_TOKEN", "preserve=a=b")
	var auditOutput bytes.Buffer
	runtime, err := app.NewRuntime(configPath, &auditOutput)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Server == nil || runtime.GitHub == nil || runtime.Tokens == nil || runtime.TLSConfig == nil || runtime.Policy == nil || runtime.Tools.GH == "" || runtime.Tools.SSH == "" {
		t.Fatalf("incomplete runtime: %#v", runtime)
	}
	environment := strings.Join(runtime.ProviderEnvironment, "\n")
	if strings.Contains(environment, "REPOWOLF_") || !strings.Contains(environment, "GH_TOKEN=preserve=a=b") {
		t.Fatalf("provider environment = %q", environment)
	}
}

func TestServePerformsRuntimeValidationBeforeBinding(t *testing.T) {
	configPath, _ := runtimeFixture(t)
	err := app.Serve(context.Background(), configPath, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "token environment") {
		t.Fatalf("Serve() error = %v, want token startup validation", err)
	}
}

func runtimeFixture(t *testing.T) (string, string) {
	t.Helper()
	certificates, err := tlsconfig.Init(tlsconfig.InitOptions{OutputDir: filepath.Join(t.TempDir(), "certs"), DNSNames: []string{"localhost"}, Now: time.Now, Random: rand.Reader})
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "provider-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	token, err := auth.Generate(strings.NewReader(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	configuration := `apiVersion: repowolf.dev/v1alpha1
listen: 127.0.0.1:65534
tls:
  certificate: ` + certificates.ServerCertificate + `
  privateKey: ` + certificates.ServerPrivateKey + `
tools:
  gh: ` + tool + `
  ssh: ` + tool + `
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
	path := filepath.Join(t.TempDir(), "repowolf.yaml")
	if err := os.WriteFile(path, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, token
}
