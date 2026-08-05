package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestNewRequiresTLSAndValidLimits(t *testing.T) {
	_, err := New(Options{})
	if err == nil {
		t.Fatal("New() accepted missing TLS")
	}
	tlsConfig, _ := testTLS(t)
	_, err = New(Options{TLSConfig: tlsConfig, MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 2, OperationTimeout: time.Second})
	if err == nil {
		t.Fatal("New() accepted per-principal concurrency above global")
	}
	insecure := tlsConfig.Clone()
	insecure.MinVersion = tls.VersionTLS12
	_, err = New(Options{TLSConfig: insecure, Tokens: &auth.Index{}, AuditWriter: audit.NewWriter(io.Discard), MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1, OperationTimeout: time.Second, GracePeriod: time.Second})
	if err == nil {
		t.Fatal("New() accepted TLS below 1.3")
	}
}

func TestNewRegistersInjectedGitService(t *testing.T) {
	service := testServer(t, Options{Git: testGitService{}})
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GitService"]; !ok {
		t.Fatal("GitService was not registered")
	}
}

type testGitService struct {
	repowolfv1.UnimplementedGitServiceServer
}

func TestHealthReadinessTransitions(t *testing.T) {
	service := testServer(t, Options{})
	assertHealthStatus(t, service, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	service.MarkReady()
	assertHealthStatus(t, service, grpc_health_v1.HealthCheckResponse_SERVING)
	service.markStopping()
	assertHealthStatus(t, service, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
}

func TestConcurrencyLimitsAreGlobalAndPerPrincipal(t *testing.T) {
	service := testServer(t, Options{MaxConcurrentRequests: 2, MaxConcurrentRequestsPerPrincipal: 1})
	interceptor := func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return service.globalConcurrencyUnaryInterceptor()(ctx, request, info, func(ctx context.Context, request any) (any, error) {
			return service.concurrencyUnaryInterceptor()(ctx, request, info, handler)
		})
	}
	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := interceptor(auth.WithPrincipal(context.Background(), "a"), nil, &grpc.UnaryServerInfo{}, blockingUnary(startedA, releaseA))
		firstDone <- err
	}()
	<-startedA

	if _, err := interceptor(auth.WithPrincipal(context.Background(), "a"), nil, &grpc.UnaryServerInfo{}, noopUnary); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second principal-a call error = %v", err)
	}
	startedB := make(chan struct{})
	releaseB := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		_, err := interceptor(auth.WithPrincipal(context.Background(), "b"), nil, &grpc.UnaryServerInfo{}, blockingUnary(startedB, releaseB))
		secondDone <- err
	}()
	<-startedB
	if _, err := interceptor(auth.WithPrincipal(context.Background(), "c"), nil, &grpc.UnaryServerInfo{}, noopUnary); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("global overflow error = %v", err)
	}
	close(releaseA)
	close(releaseB)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineInterceptorBoundsOperation(t *testing.T) {
	service := testServer(t, Options{OperationTimeout: 20 * time.Millisecond})
	_, err := service.deadlineUnaryInterceptor()(context.Background(), nil, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("deadline error = %v", err)
	}
}

func TestAuditFailureFailsCompletedRPC(t *testing.T) {
	service := testServer(t, Options{AuditWriter: audit.NewWriter(failingSink{})})
	ctx := auth.WithPrincipal(context.Background(), "agent")
	ctx = auth.WithRequestID(ctx, "request")
	_, err := service.auditUnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.Test/Call"}, noopUnary)
	if status.Code(err) != codes.Unavailable || status.Convert(err).Message() != "audit unavailable" {
		t.Fatalf("audit sink error = %v", err)
	}
}

func TestTLSHealthIsUnauthenticatedAndMessageCapsAreOneMiB(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	var output bytes.Buffer
	service, err := New(Options{
		TLSConfig:                         tlsConfig,
		Tokens:                            index,
		AuditWriter:                       audit.NewWriter(&output),
		MaxConcurrentRequests:             4,
		MaxConcurrentRequestsPerPrincipal: 2,
		OperationTimeout:                  time.Second,
		GracePeriod:                       time.Second,
		Register: func(registrar grpc.ServiceRegistrar) {
			registrar.RegisterService(&echoServiceDesc, echoImplementation{})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	ctx, cancel := context.WithCancel(context.Background())
	listener, address := listenLocal(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- service.Serve(ctx, listener) }()
	connection := dialTLS(t, address, roots)
	defer connection.Close()

	healthResponse, err := grpc_health_v1.NewHealthClient(connection).Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil || healthResponse.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("unauthenticated health = %#v, %v", healthResponse, err)
	}
	authCtx := metadataContext(context.Background(), token)
	large := &wrapperspb.BytesValue{Value: bytes.Repeat([]byte{'x'}, messageLimitBytes+1)}
	if err := connection.Invoke(authCtx, echoMethod, large, &wrapperspb.BytesValue{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized receive error = %v", err)
	}
	if err := connection.Invoke(authCtx, echoMethod, &wrapperspb.BytesValue{Value: []byte("large-response")}, &wrapperspb.BytesValue{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("oversized send error = %v", err)
	}
	cancel()
	if err := <-serveDone; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
}

func TestServeGracefullyDrainsThenForceCancels(t *testing.T) {
	t.Run("drains active call", func(t *testing.T) {
		runLifecycleTest(t, time.Second, true)
	})
	t.Run("force cancels at grace deadline", func(t *testing.T) {
		runLifecycleTest(t, 40*time.Millisecond, false)
	})
}

func runLifecycleTest(t *testing.T, grace time.Duration, release bool) {
	t.Helper()
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	started := make(chan struct{})
	finish := make(chan struct{})
	canceled := make(chan struct{})
	implementation := lifecycleEcho{started: started, finish: finish, canceled: canceled}
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 2, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Minute, GracePeriod: grace,
		Register: func(registrar grpc.ServiceRegistrar) { registrar.RegisterService(&echoServiceDesc, implementation) },
	})
	if err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	ctx, cancel := context.WithCancel(context.Background())
	listener, address := listenLocal(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- service.Serve(ctx, listener) }()
	connection := dialTLS(t, address, roots)
	callDone := make(chan error, 1)
	go func() {
		callDone <- connection.Invoke(metadataContext(context.Background(), token), echoMethod, &wrapperspb.BytesValue{}, &wrapperspb.BytesValue{})
	}()
	<-started
	cancel()
	waitHealthStatus(t, service, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	if release {
		close(finish)
		if err := <-callDone; err != nil {
			t.Fatalf("drained call error = %v", err)
		}
	} else {
		select {
		case <-canceled:
		case <-time.After(time.Second):
			t.Fatal("active call was not canceled")
		}
		if code := status.Code(<-callDone); code != codes.Canceled && code != codes.Unavailable {
			t.Fatalf("forced call code = %v", code)
		}
	}
	if err := <-serveDone; (release && err != nil) || (!release && !errors.Is(err, audit.ErrIncomplete)) {
		t.Fatalf("Serve() error = %v, release=%v", err, release)
	}
	connection.Close()
}

func testServer(t *testing.T, overrides Options) *Server {
	t.Helper()
	tlsConfig, _ := testTLS(t)
	overrides.TLSConfig = tlsConfig
	if overrides.Tokens == nil {
		overrides.Tokens = &auth.Index{}
	}
	if overrides.MaxConcurrentRequests == 0 {
		overrides.MaxConcurrentRequests = 4
	}
	if overrides.MaxConcurrentRequestsPerPrincipal == 0 {
		overrides.MaxConcurrentRequestsPerPrincipal = 2
	}
	if overrides.OperationTimeout == 0 {
		overrides.OperationTimeout = time.Second
	}
	if overrides.GracePeriod == 0 {
		overrides.GracePeriod = time.Second
	}
	if overrides.AuditWriter == nil {
		overrides.AuditWriter = audit.NewWriter(io.Discard)
	}
	service, err := New(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func assertHealthStatus(t *testing.T, service *Server, want grpc_health_v1.HealthCheckResponse_ServingStatus) {
	t.Helper()
	response, err := service.health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil || response.Status != want {
		t.Fatalf("health = %#v, %v, want %v", response, err, want)
	}
}

func waitHealthStatus(t *testing.T, service *Server, want grpc_health_v1.HealthCheckResponse_ServingStatus) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err := service.health.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		if err == nil && response.Status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	assertHealthStatus(t, service, want)
}

func blockingUnary(started chan<- struct{}, release <-chan struct{}) grpc.UnaryHandler {
	return func(context.Context, any) (any, error) { close(started); <-release; return nil, nil }
}
func noopUnary(context.Context, any) (any, error) { return nil, nil }

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

const echoMethod = "/repowolf.test.Echo/Call"

type echoService interface {
	Call(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)
}
type echoImplementation struct{}

func (echoImplementation) Call(_ context.Context, request *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	switch string(request.Value) {
	case "large-response":
		return &wrapperspb.BytesValue{Value: bytes.Repeat([]byte{'x'}, messageLimitBytes+1)}, nil
	case "next-encoded-response":
		return &wrapperspb.BytesValue{Value: bytes.Repeat([]byte{'x'}, messageLimitBytes-bytesValueWireOverhead+1)}, nil
	default:
		return request, nil
	}
}

type lifecycleEcho struct {
	started  chan<- struct{}
	finish   <-chan struct{}
	canceled chan<- struct{}
}

func (service lifecycleEcho) Call(ctx context.Context, _ *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	close(service.started)
	select {
	case <-service.finish:
		return &wrapperspb.BytesValue{}, nil
	case <-ctx.Done():
		close(service.canceled)
		return nil, ctx.Err()
	}
}

var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "repowolf.test.Echo", HandlerType: (*echoService)(nil),
	Methods: []grpc.MethodDesc{{MethodName: "Call", Handler: func(service any, ctx context.Context, decoder func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		request := new(wrapperspb.BytesValue)
		if err := decoder(request); err != nil {
			return nil, err
		}
		handler := func(ctx context.Context, request any) (any, error) {
			return service.(echoService).Call(ctx, request.(*wrapperspb.BytesValue))
		}
		if interceptor == nil {
			return handler(ctx, request)
		}
		return interceptor(ctx, request, &grpc.UnaryServerInfo{Server: service, FullMethod: echoMethod}, handler)
	}}},
}

func serverTestIndex(t *testing.T) (string, *auth.Index) {
	t.Helper()
	token, err := auth.Generate(strings.NewReader(strings.Repeat("z", 32)))
	if err != nil {
		t.Fatal(err)
	}
	index, err := auth.Load(map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}}}, func(string) (string, bool) { return token, true })
	if err != nil {
		t.Fatal(err)
	}
	return token, index
}

func metadataContext(ctx context.Context, token string) context.Context {
	return metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+token))
}

func listenLocal(t *testing.T) (net.Listener, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener, listener.Addr().String()
}

func dialTLS(t *testing.T, address string, roots *x509.CertPool) *grpc.ClientConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, err := grpc.DialContext(ctx, address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{RootCAs: roots, ServerName: "localhost"})), grpc.WithBlock())
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func testTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}, roots
}
