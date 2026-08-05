package server

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const bytesValueWireOverhead = 4

func TestEffectiveGRPCMessageLimitIsExactInBothDirections(t *testing.T) {
	tlsConfig, roots := testTLS(t)
	token, index := serverTestIndex(t)
	service, err := New(Options{
		TLSConfig: tlsConfig, Tokens: index, AuditWriter: audit.NewWriter(io.Discard),
		MaxConcurrentRequests: 2, MaxConcurrentRequestsPerPrincipal: 1,
		OperationTimeout: time.Second, GracePeriod: time.Second,
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
	authCtx := metadataContext(context.Background(), token)

	atLimit := &wrapperspb.BytesValue{Value: bytes.Repeat([]byte{'x'}, messageLimitBytes-bytesValueWireOverhead)}
	if got := proto.Size(atLimit); got != messageLimitBytes {
		t.Fatalf("at-limit encoded size = %d", got)
	}
	var response wrapperspb.BytesValue
	if err := connection.Invoke(authCtx, echoMethod, atLimit, &response); err != nil {
		t.Fatalf("exact-limit request/response failed: %v", err)
	}
	if got := proto.Size(&response); got != messageLimitBytes {
		t.Fatalf("exact-limit response encoded size = %d", got)
	}

	nextByte := &wrapperspb.BytesValue{Value: bytes.Repeat([]byte{'x'}, messageLimitBytes-bytesValueWireOverhead+1)}
	if got := proto.Size(nextByte); got != messageLimitBytes+1 {
		t.Fatalf("next-byte encoded size = %d", got)
	}
	if err := connection.Invoke(authCtx, echoMethod, nextByte, &response); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("next-byte receive error = %v", err)
	}
	if err := connection.Invoke(authCtx, echoMethod, &wrapperspb.BytesValue{Value: []byte("next-encoded-response")}, &response); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("next-byte send error = %v", err)
	}

	cancel()
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}
