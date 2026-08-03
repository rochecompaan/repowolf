package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/policy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestBlockingAuditRetainsRequestLeaseAndDoesNotBlockForcedShutdown(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	output := newBlockingOutput()
	cleaned := make(chan struct{})
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(output),
		MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Second, GracePeriod: 30 * time.Millisecond,
		Register: func(registrar grpc.ServiceRegistrar) {
			registrar.RegisterService(&echoServiceDesc, echoImplementation{})
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
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- connection.Invoke(metadataContext(context.Background(), token), echoMethod, &wrapperspb.BytesValue{}, &wrapperspb.BytesValue{})
	}()
	<-output.entered

	secondCtx, cancelSecond := context.WithTimeout(metadataContext(context.Background(), token), 100*time.Millisecond)
	defer cancelSecond()
	err = connection.Invoke(secondCtx, echoMethod, &wrapperspb.BytesValue{}, &wrapperspb.BytesValue{})
	if status.Code(err) != codes.ResourceExhausted {
		output.unblock()
		t.Fatalf("request admitted while terminal audit held its lease: %v", err)
	}

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			output.unblock()
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		output.unblock()
		t.Fatal("blocking audit prevented forced shutdown")
	}
	select {
	case <-cleaned:
	default:
		output.unblock()
		t.Fatal("blocking audit prevented runtime cleanup")
	}
	output.unblock()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("audited request did not terminate after sink release")
	}
}

func TestTerminalAuditPreservesCompletedAndRejectedOutcomes(t *testing.T) {
	var output bytes.Buffer
	service := testServer(t, Options{AuditWriter: audit.NewWriter(&output), MaxConcurrentRequests: 1, MaxConcurrentRequestsPerPrincipal: 1})
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
	interceptor := service.auditUnaryInterceptor()
	if _, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: echoMethod}, noopUnary); err != nil {
		t.Fatal(err)
	}
	if _, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: echoMethod}, func(context.Context, any) (any, error) {
		return nil, policy.ErrDenied
	}); err != policy.ErrDenied {
		t.Fatalf("handler error = %v", err)
	}

	decoder := json.NewDecoder(&output)
	for index, want := range []audit.Outcome{audit.OutcomeCompleted, audit.OutcomeDenied} {
		var event audit.Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
		if event.Outcome != want || event.Principal != "agent" || event.RequestID != "request" {
			t.Fatalf("event %d = %#v, want outcome %q", index, event, want)
		}
	}
}

type blockingOutput struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingOutput() *blockingOutput {
	return &blockingOutput{entered: make(chan struct{}), release: make(chan struct{})}
}

func (output *blockingOutput) Write(payload []byte) (int, error) {
	output.once.Do(func() { close(output.entered) })
	<-output.release
	return len(payload), nil
}

func (output *blockingOutput) unblock() {
	select {
	case <-output.release:
	default:
		close(output.release)
	}
}

var _ io.Writer = (*blockingOutput)(nil)
