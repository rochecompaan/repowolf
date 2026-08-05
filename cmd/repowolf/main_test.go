package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--version"}, &stdout, &stderr); code != 0 || stdout.String() != "dev\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunUsageForUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"unknown"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.String() != "usage: repowolf <serve|config|token|cert>\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTokenGenerate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"token", "generate"}, &stdout, &stderr); code != 0 || stdout.Len() != 48 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout-bytes=%d stderr=%q", code, stdout.Len(), stderr.String())
	}
}

func TestRunCertInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certificates")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"cert", "init", "--output", dir, "--dns", "repo.internal"}, &stdout, &stderr)
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
	code := run(context.Background(), []string{"cert", "init", "--output", t.TempDir()}, &stdout, &stderr)
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
	code := run(context.Background(), []string{"config", "validate", "--config", path}, &stdout, &stderr)
	if code != 0 || stdout.String() != "configuration valid\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunConfigValidateRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"config", "validate"}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "invalid config validate arguments\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestServeCommandHandlesSIGTERMThroughLifecycleContext(t *testing.T) {
	originalServe := serveCommand
	t.Cleanup(func() { serveCommand = originalServe })
	started := make(chan struct{})
	cleaned := make(chan struct{})
	serveCommand = func(ctx context.Context, _ string, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		close(cleaned)
		return nil
	}
	ctx, stop := shutdownContext()
	defer stop()
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--config", "unused.yaml"}, io.Discard, io.Discard)
	}()
	<-started
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("serve command ignored SIGTERM")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("serve returned without lifecycle cleanup path")
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
