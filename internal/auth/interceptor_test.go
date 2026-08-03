package auth_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestUnaryServerInterceptorAuthenticatesExactBearerMetadata(t *testing.T) {
	token, index := testIndex(t)
	interceptor := auth.UnaryServerInterceptor(index)
	tests := []struct {
		name   string
		values []string
		want   codes.Code
	}{
		{name: "missing", want: codes.Unauthenticated},
		{name: "empty", values: []string{""}, want: codes.Unauthenticated},
		{name: "wrong scheme", values: []string{"bearer " + token}, want: codes.Unauthenticated},
		{name: "missing space", values: []string{"Bearer" + token}, want: codes.Unauthenticated},
		{name: "invalid token", values: []string{"Bearer rw1_invalid"}, want: codes.Unauthenticated},
		{name: "duplicate", values: []string{"Bearer " + token, "Bearer " + token}, want: codes.Unauthenticated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{"authorization": test.values})
			_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.Test/Call"}, func(context.Context, any) (any, error) {
				t.Fatal("handler called")
				return nil, nil
			})
			if status.Code(err) != test.want || status.Convert(err).Message() != "authentication required" {
				t.Fatalf("error = %v, want stable %v", err, test.want)
			}
		})
	}
}

func TestUnaryServerInterceptorAddsPrincipalAndRequestID(t *testing.T) {
	token, index := testIndex(t)
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	_, err := auth.UnaryServerInterceptor(index)(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.Test/Call"}, func(ctx context.Context, _ any) (any, error) {
		if principal, ok := auth.Principal(ctx); !ok || principal != "agent" {
			t.Fatalf("principal = %q, %v", principal, ok)
		}
		if requestID, ok := auth.RequestID(ctx); !ok || len(requestID) != 32 || strings.Trim(requestID, "0123456789abcdef") != "" {
			t.Fatalf("request ID = %q, %v", requestID, ok)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamServerInterceptorAddsPrincipalAndRequestID(t *testing.T) {
	token, index := testIndex(t)
	stream := &testServerStream{ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))}
	err := auth.StreamServerInterceptor(index)(nil, stream, &grpc.StreamServerInfo{FullMethod: "/repowolf.v1.Test/Stream"}, func(_ any, stream grpc.ServerStream) error {
		principal, principalOK := auth.Principal(stream.Context())
		requestID, requestOK := auth.RequestID(stream.Context())
		if !principalOK || principal != "agent" || !requestOK || requestID == "" {
			t.Fatalf("principal=%q/%v requestID=%q/%v", principal, principalOK, requestID, requestOK)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOnlyExactHealthMethodsBypassBearerAuthentication(t *testing.T) {
	_, index := testIndex(t)
	for _, method := range []string{
		grpc_health_v1.Health_Check_FullMethodName,
		grpc_health_v1.Health_Watch_FullMethodName,
	} {
		t.Run(method, func(t *testing.T) {
			called := false
			_, err := auth.UnaryServerInterceptor(index)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			})
			if err != nil || !called {
				t.Fatalf("health call error=%v called=%v", err, called)
			}
		})
	}

	called := false
	_, err := auth.UnaryServerInterceptor(index)(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/FutureMethod"}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if status.Code(err) != codes.Unauthenticated || called {
		t.Fatalf("future health method error=%v called=%v", err, called)
	}
}

func testIndex(t *testing.T) (string, *auth.Index) {
	t.Helper()
	token, err := auth.Generate(io.LimitReader(strings.NewReader(strings.Repeat("x", 32)), 32))
	if err != nil {
		t.Fatal(err)
	}
	index, err := auth.Load(map[string]config.Principal{"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}}}, func(name string) (string, bool) {
		return token, name == "REPOWOLF_TOKEN_AGENT"
	})
	if err != nil {
		t.Fatal(err)
	}
	return token, index
}

type testServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *testServerStream) Context() context.Context { return stream.ctx }
