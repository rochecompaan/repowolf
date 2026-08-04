package testutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ServerOptions describe one external RepoWolf test service.
type ServerOptions struct {
	Binary      string
	PolicyPath  string
	Certificate Certificate
	GHPath      string
	SSHPath     string
	Environment []string
}

// Server is a real loopback TLS service process.
type Server struct {
	Endpoint    string
	Certificate Certificate
	AuditPath   string
	StderrPath  string

	command *exec.Cmd
	done    chan error
	mu      sync.Mutex
	stopped bool
}

// StartServer renders the strict test policy and starts the public service binary.
func StartServer(t testing.TB, options ServerOptions) *Server {
	t.Helper()
	address := reserveAddress(t)
	template, err := os.ReadFile(options.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	config := string(template)
	values := map[string]string{
		"__LISTEN__": address, "__CERTIFICATE__": options.Certificate.CertificateFile,
		"__PRIVATE_KEY__": options.Certificate.KeyFile, "__GH__": options.GHPath, "__SSH__": options.SSHPath,
	}
	for marker, value := range values {
		config = strings.ReplaceAll(config, marker, strconv.Quote(value))
	}
	work := t.TempDir()
	configPath := work + "/policy.yaml"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		Endpoint: "https://" + address, Certificate: options.Certificate,
		AuditPath: work + "/audit.jsonl", StderrPath: work + "/server.stderr", done: make(chan error, 1),
	}
	auditFile, err := os.OpenFile(server.AuditPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	stderrFile, err := os.OpenFile(server.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		auditFile.Close()
		t.Fatal(err)
	}
	server.command = exec.Command(options.Binary, "serve", "--config", configPath)
	server.command.Env = Environment(os.Environ(), options.Environment...)
	server.command.Stdout, server.command.Stderr = auditFile, stderrFile
	if err := server.command.Start(); err != nil {
		auditFile.Close()
		stderrFile.Close()
		t.Fatal(err)
	}
	go func() {
		err := server.command.Wait()
		_ = auditFile.Close()
		_ = stderrFile.Close()
		server.done <- err
	}()
	waitForTLS(t, server)
	t.Cleanup(func() { server.Stop(t) })
	return server
}

// Stop terminates and fully waits for the service process.
func (server *Server) Stop(t testing.TB) {
	t.Helper()
	server.mu.Lock()
	if server.stopped {
		server.mu.Unlock()
		return
	}
	server.stopped = true
	server.mu.Unlock()
	if server.command.ProcessState == nil {
		_ = server.command.Process.Signal(syscall.SIGTERM)
	}
	select {
	case err := <-server.done:
		if err != nil && server.command.ProcessState != nil && !server.command.ProcessState.Success() {
			t.Fatalf("service exit: %v; stderr=%s", err, readDiagnostic(server.StderrPath))
		}
	case <-time.After(10 * time.Second):
		_ = server.command.Process.Kill()
		<-server.done
		t.Fatal("service did not stop")
	}
}

func reserveAddress(t testing.TB) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForTLS(t testing.TB, server *Server) {
	t.Helper()
	ca, err := os.ReadFile(server.Certificate.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("load generated CA")
	}
	settings := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: server.Certificate.ServerName}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 100 * time.Millisecond}, "tcp", strings.TrimPrefix(server.Endpoint, "https://"), settings)
		if dialErr == nil {
			_ = connection.Close()
			return
		}
		select {
		case processErr := <-server.done:
			t.Fatalf("service exited before readiness: %v; stderr=%s", processErr, readDiagnostic(server.StderrPath))
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("service TLS readiness timeout: %s", readDiagnostic(server.StderrPath))
}

func readDiagnostic(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(contents)
}
