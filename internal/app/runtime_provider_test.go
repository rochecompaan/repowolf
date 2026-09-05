package app_test

import (
	"bytes"
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

func TestNewRuntimeBuildsExplicitGitHubOnlyRuntime(t *testing.T) {
	fixture := newRuntimeProviderFixture(t, `
  github:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITHUB
`, `
  project:
    provider: github
    owner: alpha
    name: project
`)
	fixture.setToken(t, "REPOWOLF_TOKEN_GITHUB")

	runtime := fixture.newRuntime(t)
	assertCompleteRuntime(t, runtime)
	if runtime.GitHub == nil || runtime.Tools.GH != fixture.tool {
		t.Fatal("explicit GitHub runtime did not retain the canonical gh path and executor")
	}
}

func TestNewRuntimeBuildsLegacyGitHubOnlyRuntime(t *testing.T) {
	fixture := newRuntimeProviderFixture(t, `
  github:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
`, `
  project:
    provider: github
    owner: alpha
    name: project
`)
	fixture.setToken(t, "GH_TOKEN")

	runtime := fixture.newRuntime(t)
	assertCompleteRuntime(t, runtime)
	if runtime.GitHub == nil || runtime.Tools.GH != fixture.tool {
		t.Fatal("legacy GitHub runtime did not retain the canonical gh path and executor")
	}
}

func TestNewRuntimeBuildsGiteaOnlyRuntimeWithoutGH(t *testing.T) {
	fixture := newRuntimeProviderFixture(t, `
  gitea:
    kind: gitea
    apiHost: gitea.example
    gitHost: gitea.example
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITEA
`, `
  project:
    provider: gitea
    owner: alpha
    name: project
`)
	fixture.setToken(t, "REPOWOLF_TOKEN_GITEA")
	fixture.gh = filepath.Join(t.TempDir(), "missing-gh")
	fixture.writeConfig(t)

	runtime := fixture.newRuntime(t)
	if runtime.Tools.GH != "" || runtime.GitHub != nil {
		t.Fatal("Gitea-only runtime resolved GitHub dependencies")
	}
	assertCompleteRuntime(t, runtime)
}

func TestNewRuntimeBuildsMixedAndMultipleProviderRuntime(t *testing.T) {
	fixture := newRuntimeProviderFixture(t, `
  github-a:
    kind: github
    apiHost: github-a.example
    gitHost: github-a.example
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITHUB_A
  github-b:
    kind: github
    apiHost: github-b.example
    gitHost: github-b.example
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITHUB_B
  gitea:
    kind: gitea
    apiHost: gitea.example
    gitHost: gitea.example
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITEA
`, `
  project:
    provider: github-a
    owner: alpha
    name: project
`)
	fixture.setToken(t, "REPOWOLF_TOKEN_GITHUB_A")
	fixture.setToken(t, "REPOWOLF_TOKEN_GITHUB_B")
	fixture.setToken(t, "REPOWOLF_TOKEN_GITEA")

	runtime := fixture.newRuntime(t)
	assertCompleteRuntime(t, runtime)
	if runtime.GitHub == nil || runtime.Tools.GH != fixture.tool || runtime.Tools.SSH != fixture.tool {
		t.Fatal("mixed runtime did not retain canonical tools and GitHub executor")
	}
}

func TestNewRuntimeRejectsDuplicatePrincipalAndProviderValue(t *testing.T) {
	fixture := newRuntimeProviderFixture(t, `
  github:
    kind: github
    apiHost: github.com
    gitHost: github.com
    sshUser: git
    tokenEnv: REPOWOLF_TOKEN_GITHUB
`, `
  project:
    provider: github
    owner: alpha
    name: project
`)
	shared := fixture.setToken(t, "REPOWOLF_TOKEN_GITHUB")
	t.Setenv("REPOWOLF_TOKEN_AGENT", shared)

	runtime, err := app.NewRuntime(fixture.configPath, &bytes.Buffer{})
	if err == nil || runtime != nil {
		t.Fatal("NewRuntime accepted duplicate principal and provider credentials")
	}
	if strings.Contains(err.Error(), shared) {
		t.Fatalf("NewRuntime() error disclosed token: %v", err)
	}
}

type runtimeProviderFixture struct {
	configPath string
	cert       string
	key        string
	tool       string
	gh         string
	providers  string
	repos      string
}

func newRuntimeProviderFixture(t *testing.T, providers, repos string) *runtimeProviderFixture {
	t.Helper()
	certificates, err := tlsconfig.Init(tlsconfig.InitOptions{OutputDir: filepath.Join(t.TempDir(), "certs"), DNSNames: []string{"localhost"}, Now: time.Now, Random: rand.Reader})
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(t.TempDir(), "provider-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := &runtimeProviderFixture{
		configPath: filepath.Join(t.TempDir(), "repowolf.yaml"), cert: certificates.ServerCertificate,
		key: certificates.ServerPrivateKey, tool: tool, gh: tool, providers: providers, repos: repos,
	}
	fixture.writeConfig(t)
	return fixture
}

func (fixture *runtimeProviderFixture) setToken(t *testing.T, name string) string {
	t.Helper()
	token, err := auth.Generate(strings.NewReader(strings.Repeat(name, 32)))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(name, token)
	return token
}

func (fixture *runtimeProviderFixture) writeConfig(t *testing.T) {
	t.Helper()
	configuration := `apiVersion: repowolf.dev/v1alpha1
listen: 127.0.0.1:65534
tls:
  certificate: ` + fixture.cert + `
  privateKey: ` + fixture.key + `
tools:
  gh: ` + fixture.gh + `
  ssh: ` + fixture.tool + `
providers:` + fixture.providers + `
repositories:` + fixture.repos + `
    git:
      maxRefUpdates: 16
principals:
  agent:
    tokenEnvs: [REPOWOLF_TOKEN_AGENT]
    grants:
      - repository: project
        capabilities: [repository:read]
`
	if err := os.WriteFile(fixture.configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (fixture *runtimeProviderFixture) newRuntime(t *testing.T) *app.Runtime {
	t.Helper()
	fixture.setToken(t, "REPOWOLF_TOKEN_AGENT")
	runtime, err := app.NewRuntime(fixture.configPath, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func assertCompleteRuntime(t *testing.T, runtime *app.Runtime) {
	t.Helper()
	if runtime.Server == nil || runtime.Git == nil || runtime.Tokens == nil || runtime.TLSConfig == nil || runtime.Policy == nil || runtime.Tools.SSH == "" {
		t.Fatal("runtime did not assemble all required dependencies")
	}
}
