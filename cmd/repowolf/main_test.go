package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || stdout.String() != "dev\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunUsageForUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.String() != "usage: repowolf <serve|config|token|cert>\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTokenGenerate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"token", "generate"}, &stdout, &stderr); code != 0 || stdout.Len() != 48 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout-bytes=%d stderr=%q", code, stdout.Len(), stderr.String())
	}
}

func TestRunCertInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certificates")
	var stdout, stderr bytes.Buffer
	code := run([]string{"cert", "init", "--output", dir, "--dns", "repo.internal"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, path := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated path %q: %v", path, err)
		}
	}
}

func TestRunCertInitUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"cert", "init", "--output", t.TempDir()}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "invalid cert init arguments\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunConfigValidateDoesNotRequireRuntimeSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repowolf.yaml")
	if err := os.WriteFile(path, []byte(validConfigYAML()), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate", "--config", path}, &stdout, &stderr)
	if code != 0 || stdout.String() != "configuration valid\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunConfigValidateRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "invalid config validate arguments\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
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
