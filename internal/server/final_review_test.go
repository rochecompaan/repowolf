package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const echoStreamMethod = "/repowolf.test.Stream/Chat"

func TestForcedShutdownReapsActiveProviderBeforeServeReturns(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	runnerRegistry := &runner.Runner{}
	started := make(chan struct{})
	pidFile := filepath.Join(t.TempDir(), "provider.pid")
	cwdFile := filepath.Join(t.TempDir(), "provider.cwd")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(script, []byte("#!"+shell+"\nprintf '%s' \"$$\" >\"$PIDFILE\"\npwd >\"$CWDFILE\"\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Minute, GracePeriod: 20 * time.Millisecond,
		Register: func(registrar grpc.ServiceRegistrar) {
			registrar.RegisterService(&echoServiceDesc, providerIgnoringEcho{runner: runnerRegistry, command: runner.Command{
				Path: script, Env: []string{"PIDFILE=" + pidFile, "CWDFILE=" + cwdFile}, Timeout: time.Minute,
				StdinLimit: 1, StdoutLimit: 1, StderrLimit: 1024,
			}, started: started})
		},
		Cleanup: runnerRegistry.Cleanup,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	serveContext, cancelServe := context.WithCancel(context.Background())
	listener, address := listenLocal(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- service.Serve(serveContext, listener) }()
	connection := dialTLS(t, address, roots)
	defer connection.Close()
	callDone := make(chan error, 1)
	go func() {
		callDone <- connection.Invoke(metadataContext(context.Background(), token), echoMethod, &wrapperspb.BytesValue{}, &wrapperspb.BytesValue{})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider did not start")
	}
	pid := readProviderPID(t, pidFile)
	cwd := strings.TrimSpace(string(readProviderFile(t, cwdFile)))
	cancelServe()
	select {
	case err := <-serveDone:
		if !errors.Is(err, audit.ErrIncomplete) {
			t.Fatalf("forced Serve error = %v, want audit incomplete", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forced Serve did not wait for provider cleanup")
	}
	assertProviderGone(t, pid)
	if _, err := os.Stat(cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private provider cwd remains after Serve: %v", err)
	}
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("forced RPC did not terminate")
	}
}

func TestGlobalLimitRejectsInvalidBearerBeforeAuthenticationForUnaryAndStream(t *testing.T) {
	var output bytes.Buffer
	service, connection, _, stop := reviewTestServer(t, &output, 1, 1, reviewEcho{started: make(chan struct{}), release: make(chan struct{})})
	defer stop()
	if !service.acquireGlobal() {
		t.Fatal("failed to reserve global lease")
	}
	defer service.releaseGlobal()
	invalid := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer rw1_invalid"))
	if err := connection.Invoke(invalid, echoMethod, &wrapperspb.BytesValue{Value: []byte("untrusted")}, &wrapperspb.BytesValue{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("invalid unary at global capacity = %v", err)
	}
	stream, err := connection.NewStream(invalid, &echoStreamDesc.Streams[0], echoStreamMethod)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.SendMsg(&wrapperspb.BytesValue{Value: []byte("untrusted")}); err != nil {
		t.Fatalf("invalid stream send = %v", err)
	}
	if err := stream.RecvMsg(&wrapperspb.BytesValue{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("invalid stream at global capacity = %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("pre-auth rejections emitted audit metadata: %q", output.String())
	}
}

func TestAuthenticatedAdmissionAndPrincipalCapacityRejectionsWriteSafeTerminalAudit(t *testing.T) {
	for _, test := range []struct {
		name   string
		stream bool
		mode   string
	}{
		{name: "unary admission", mode: "admission"},
		{name: "stream admission", stream: true, mode: "admission"},
		{name: "unary principal capacity", mode: "capacity"},
		{name: "stream principal capacity", stream: true, mode: "capacity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			implementation := reviewEcho{started: make(chan struct{}), release: make(chan struct{})}
			service, connection, token, stop := reviewTestServer(t, &output, 2, 1, implementation)
			defer stop()
			authenticated := metadataContext(context.Background(), token)
			if test.mode == "admission" {
				service.markStopping()
			} else if test.stream {
				stream, err := connection.NewStream(authenticated, &echoStreamDesc.Streams[0], echoStreamMethod)
				if err != nil {
					t.Fatal(err)
				}
				if err := stream.SendMsg(&wrapperspb.BytesValue{Value: []byte("secret stream payload")}); err != nil {
					t.Fatal(err)
				}
				<-implementation.started
			} else {
				go func() {
					_ = connection.Invoke(authenticated, echoMethod, &wrapperspb.BytesValue{Value: []byte("secret first payload")}, &wrapperspb.BytesValue{})
				}()
				<-implementation.started
			}
			var callErr error
			if test.stream {
				stream, err := connection.NewStream(authenticated, &echoStreamDesc.Streams[0], echoStreamMethod)
				if err != nil {
					t.Fatal(err)
				}
				if err := stream.SendMsg(&wrapperspb.BytesValue{Value: []byte("secret rejected payload")}); err != nil {
					t.Fatal(err)
				}
				callErr = stream.RecvMsg(&wrapperspb.BytesValue{})
			} else {
				callErr = connection.Invoke(authenticated, echoMethod, &wrapperspb.BytesValue{Value: []byte("secret rejected payload")}, &wrapperspb.BytesValue{})
			}
			want := codes.Unavailable
			if test.mode == "capacity" {
				want = codes.ResourceExhausted
			}
			if status.Code(callErr) != want {
				t.Fatalf("rejection code = %v, want %v", callErr, want)
			}
			events := reviewAuditEvents(t, output.Bytes())
			if len(events) != 1 || events[0].Principal != "agent" || events[0].RequestID == "" || events[0].Operation == "" || events[0].Reason != want.String() {
				t.Fatalf("rejection audit events = %#v", events)
			}
			if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), token) {
				t.Fatalf("rejection audit leaked request value or token: %q", output.String())
			}
			if test.mode == "capacity" {
				close(implementation.release)
			}
		})
	}
}

type providerIgnoringEcho struct {
	runner  *runner.Runner
	command runner.Command
	started chan<- struct{}
}

func (service providerIgnoringEcho) Call(ctx context.Context, _ *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	if _, err := service.runner.Start(ctx, service.command); err != nil {
		return nil, err
	}
	close(service.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

type reviewEcho struct {
	started chan struct{}
	release chan struct{}
}

func (service reviewEcho) Call(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	close(service.started)
	<-service.release
	return &wrapperspb.BytesValue{}, nil
}

func (service reviewEcho) Chat(stream grpc.ServerStream) error {
	request := new(wrapperspb.BytesValue)
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	close(service.started)
	select {
	case <-service.release:
		return nil
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
}

var echoStreamDesc = grpc.ServiceDesc{
	ServiceName: "repowolf.test.Stream", HandlerType: (*echoStreamService)(nil),
	Streams: []grpc.StreamDesc{{StreamName: "Chat", Handler: func(service any, stream grpc.ServerStream) error {
		return service.(echoStreamService).Chat(stream)
	}, ServerStreams: true, ClientStreams: true}},
}

type echoStreamService interface {
	Chat(grpc.ServerStream) error
}

func reviewTestServer(t *testing.T, output *bytes.Buffer, global, perPrincipal int, implementation any) (*Server, *grpc.ClientConn, string, func()) {
	t.Helper()
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(output),
		MaxConcurrentRequests: global, MaxConcurrentRequestsPerPrincipal: perPrincipal,
		OperationTimeout: time.Minute, GracePeriod: time.Second,
		Register: func(registrar grpc.ServiceRegistrar) {
			registrar.RegisterService(&echoServiceDesc, implementation)
			if _, ok := implementation.(echoStreamService); ok {
				registrar.RegisterService(&echoStreamDesc, implementation)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	serveContext, cancel := context.WithCancel(context.Background())
	listener, address := listenLocal(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- service.Serve(serveContext, listener) }()
	connection := dialTLS(t, address, roots)
	return service, connection, token, func() {
		_ = connection.Close()
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("review server did not stop")
		}
	}
}

func reviewAuditEvents(t *testing.T, data []byte) []audit.Event {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var events []audit.Event
	for decoder.More() {
		var event audit.Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func readProviderPID(t *testing.T, path string) int {
	t.Helper()
	value, err := strconv.Atoi(strings.TrimSpace(string(readProviderFile(t, path))))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readProviderFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}

func assertProviderGone(t *testing.T, pid int) {
	t.Helper()
	if _, err := os.FindProcess(pid); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("provider process %d remains: %v", pid, err)
	}
}
