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

const (
	defaultReadinessTimeout = 10 * time.Second
	defaultStartAttempts    = 5
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
	done    chan struct{}
	mu      sync.Mutex
	waitErr error
	stopped bool
}

type serverStartSettings struct {
	attempts         int
	readinessTimeout time.Duration
	address          func() (string, error)
}

// StartServer renders the strict test policy and starts the public service binary.
func StartServer(t testing.TB, options ServerOptions) *Server {
	t.Helper()
	server, err := startServer(t, options, serverStartSettings{
		attempts: defaultStartAttempts, readinessTimeout: defaultReadinessTimeout, address: reserveAddress,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func startServer(t testing.TB, options ServerOptions, settings serverStartSettings) (*Server, error) {
	t.Helper()
	if settings.attempts <= 0 || settings.readinessTimeout <= 0 || settings.address == nil {
		return nil, fmt.Errorf("invalid server start settings")
	}
	template, err := os.ReadFile(options.PolicyPath)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < settings.attempts; attempt++ {
		address, err := settings.address()
		if err != nil {
			return nil, err
		}
		server, err := startServerAttempt(t, options, template, address)
		if err != nil {
			return nil, err
		}
		if err := waitForTLS(server, settings.readinessTimeout); err == nil {
			return server, nil
		} else {
			lastErr = fmt.Errorf("%w: %s", err, readDiagnostic(server.StderrPath))
		}
		exited := server.exited()
		_ = server.stop()
		if !exited {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("service failed after %d bind attempts: %w", settings.attempts, lastErr)
}

func startServerAttempt(t testing.TB, options ServerOptions, template []byte, address string) (*Server, error) {
	t.Helper()
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
		return nil, err
	}
	server := &Server{
		Endpoint: "https://" + address, Certificate: options.Certificate,
		AuditPath: work + "/audit.jsonl", StderrPath: work + "/server.stderr", done: make(chan struct{}),
	}
	auditFile, err := os.OpenFile(server.AuditPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	stderrFile, err := os.OpenFile(server.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		_ = auditFile.Close()
		return nil, err
	}
	server.command = exec.Command(options.Binary, "serve", "--config", configPath)
	server.command.Env = Environment(os.Environ(), options.Environment...)
	server.command.Stdout, server.command.Stderr = auditFile, stderrFile
	if err := server.command.Start(); err != nil {
		_ = auditFile.Close()
		_ = stderrFile.Close()
		return nil, err
	}
	t.Cleanup(func() {
		if err := server.stop(); err != nil {
			t.Errorf("service cleanup: %v; stderr=%s", err, readDiagnostic(server.StderrPath))
		}
	})
	go func() {
		err := server.command.Wait()
		_ = auditFile.Close()
		_ = stderrFile.Close()
		server.mu.Lock()
		server.waitErr = err
		server.mu.Unlock()
		close(server.done)
	}()
	return server, nil
}

// Stop terminates and fully waits for the service process.
func (server *Server) Stop(t testing.TB) {
	t.Helper()
	if err := server.stop(); err != nil {
		t.Fatalf("service exit: %v; stderr=%s", err, readDiagnostic(server.StderrPath))
	}
}

func (server *Server) stop() error {
	server.mu.Lock()
	if server.stopped {
		server.mu.Unlock()
		return nil
	}
	server.stopped = true
	server.mu.Unlock()
	select {
	case <-server.done:
	default:
		_ = server.command.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-server.done:
	case <-time.After(10 * time.Second):
		_ = server.command.Process.Kill()
		<-server.done
		return fmt.Errorf("service did not stop")
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.waitErr != nil && server.command.ProcessState != nil && !server.command.ProcessState.Success() {
		return server.waitErr
	}
	return nil
}

func (server *Server) exited() bool {
	select {
	case <-server.done:
		return true
	default:
		return false
	}
}

func (server *Server) processError() error {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.waitErr
}

func reserveAddress() (string, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func waitForTLS(server *Server, timeout time.Duration) error {
	ca, err := os.ReadFile(server.Certificate.CAFile)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return fmt.Errorf("load generated CA")
	}
	settings := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: server.Certificate.ServerName}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connection, dialErr := tls.DialWithDialer(&net.Dialer{Timeout: 100 * time.Millisecond}, "tcp", strings.TrimPrefix(server.Endpoint, "https://"), settings)
		if dialErr == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-server.done:
			return fmt.Errorf("service exited before readiness: %v", server.processError())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("service TLS readiness timeout")
}

func readDiagnostic(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(contents)
}
