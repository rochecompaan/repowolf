package server

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestHealthAdmissionTracksActiveCalls(t *testing.T) {
	service := testServer(t, Options{})
	started := make(chan struct{})
	done := make(chan error, 1)
	ctx, release := context.WithCancel(context.Background())
	defer release()
	go func() {
		_, err := service.admissionUnaryInterceptor()(ctx, nil, &grpc.UnaryServerInfo{FullMethod: grpc_health_v1.Health_Check_FullMethodName}, func(ctx context.Context, _ any) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		done <- err
	}()
	<-started
	service.cancelActive()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("health call was not tracked by lifecycle admission")
	}
}

func TestTLSHealthWatchUsesGlobalConcurrencyAndShutsDown(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	_, index := serverTestIndex(t)
	cleaned := make(chan struct{})
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Second, GracePeriod: 40 * time.Millisecond,
		Cleanup: func() error { close(cleaned); return nil },
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
	client := grpc_health_v1.NewHealthClient(connection)

	watch, err := client.Watch(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := watch.Recv()
	if err != nil || response.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("initial Watch response = %#v, %v", response, err)
	}
	if _, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Check while Watch occupies global lease = %v", err)
	}
	second, err := client.Watch(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second Watch error = %v", err)
	}

	cancel()
	response, err = watch.Recv()
	if err != nil || response.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("shutdown Watch response = %#v, %v", response, err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("health Watch prevented bounded shutdown")
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("shutdown did not invoke cleanup")
	}
}

func TestTLSHealthWatchHasServerDeadline(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	_, index := serverTestIndex(t)
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 2, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: 30 * time.Millisecond, GracePeriod: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.MarkReady()
	ctx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	listener, address := listenLocal(t)
	serveDone := make(chan error, 1)
	go func() { serveDone <- service.Serve(ctx, listener) }()
	connection := dialTLS(t, address, roots)
	defer connection.Close()

	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	watch, err := grpc_health_v1.NewHealthClient(connection).Watch(watchCtx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watch.Recv(); err != nil {
		t.Fatal(err)
	}
	received := make(chan error, 1)
	go func() {
		_, err := watch.Recv()
		received <- err
	}()
	select {
	case err := <-received:
		if code := status.Code(err); code != codes.Canceled && code != codes.DeadlineExceeded {
			t.Fatalf("Watch deadline error = %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		cancelWatch()
		<-received
		t.Fatal("health Watch exceeded server operation deadline")
	}
	cancelServe()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
