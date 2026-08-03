package server

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestForceStopReturnsAndCleansUpWhenHandlerIgnoresCancellation(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	started := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	cleaned := make(chan struct{})
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Minute, GracePeriod: 30 * time.Millisecond,
		Register: func(registrar grpc.ServiceRegistrar) {
			registrar.RegisterService(&echoServiceDesc, cancellationIgnoringEcho{started: started, release: release, returned: returned})
		},
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
	callDone := make(chan error, 1)
	go func() {
		callDone <- connection.Invoke(metadataContext(context.Background(), token), echoMethod, &wrapperspb.BytesValue{}, &wrapperspb.BytesValue{})
	}()
	<-started

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(400 * time.Millisecond):
		close(release)
		t.Fatal("Serve() waited for cancellation-ignoring handler after force deadline")
	}
	select {
	case <-cleaned:
	default:
		close(release)
		t.Fatal("forced shutdown returned without runtime cleanup")
	}

	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("released handler did not return")
	}
	select {
	case <-callDone:
	case <-time.After(time.Second):
		t.Fatal("client call did not terminate")
	}
}

type cancellationIgnoringEcho struct {
	started  chan<- struct{}
	release  <-chan struct{}
	returned chan<- struct{}
}

func (service cancellationIgnoringEcho) Call(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	close(service.started)
	<-service.release
	close(service.returned)
	return &wrapperspb.BytesValue{}, nil
}
