package testutil

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReadinessFailureTerminatesAndReapsProcess(t *testing.T) {
	root := t.TempDir()
	certificate := GenerateCertificate(t, filepath.Join(root, "tls"))
	policy := filepath.Join(root, "policy.yaml")
	if err := os.WriteFile(policy, []byte("listen: __LISTEN__\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(root, "service.pid")
	binary := filepath.Join(root, "never-ready")
	script := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$$\" > \"$PID_FILE\"\nexec \"$SLEEP\" 30\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = startServer(t, ServerOptions{
		Binary: binary, PolicyPath: policy, Certificate: certificate,
		Environment: []string{"PID_FILE=" + pidPath, "SLEEP=" + sleep},
	}, serverStartSettings{attempts: 1, readinessTimeout: 100 * time.Millisecond, address: reserveAddress})
	if err == nil || !strings.Contains(err.Error(), "readiness timeout") {
		t.Fatalf("startServer error = %v", err)
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("readiness-failed process %d was not reaped: %v", pid, err)
	}
}

func TestStartServerRetriesAddressCollision(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	binaries := BuildBinaries(t, binDir)
	repository := repositoryRoot(t)
	provider := InstallExecutable(t, filepath.Join(repository, "integration/testdata/fake-provider.sh"), filepath.Join(binDir, "fake-provider"))
	ssh := InstallExecutable(t, filepath.Join(repository, "integration/testdata/fake-ssh.sh"), filepath.Join(binDir, "fake-ssh"))
	certificate := GenerateCertificate(t, filepath.Join(root, "tls"))
	calls := 0
	address := func() (string, error) {
		calls++
		if calls == 1 {
			return listener.Addr().String(), nil
		}
		return reserveAddress()
	}
	server, err := startServer(t, ServerOptions{
		Binary: binaries.Service, PolicyPath: filepath.Join(repository, "integration/testdata/policy.yaml"),
		Certificate: certificate, GHPath: provider, SSHPath: ssh,
		Environment: []string{"REPOWOLF_TOKEN_AGENT=rw1_AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"},
	}, serverStartSettings{attempts: 2, readinessTimeout: 5 * time.Second, address: address})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.TrimPrefix(server.Endpoint, "https://") == listener.Addr().String() {
		t.Fatalf("collision attempts = %d, endpoint = %q", calls, server.Endpoint)
	}
	server.Stop(t)
}
