package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const healthServicePrefix = "/grpc.health.v1.Health/"

// UnaryServerInterceptor authenticates non-health unary RPCs and adds trusted
// identity metadata to their context.
func UnaryServerInterceptor(index *Index) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isHealthMethod(info.FullMethod) {
			return handler(ctx, request)
		}
		ctx, err := authenticatedContext(ctx, index)
		if err != nil {
			return nil, err
		}
		return handler(ctx, request)
	}
}

// StreamServerInterceptor authenticates non-health streaming RPCs and adds
// trusted identity metadata to their context.
func StreamServerInterceptor(index *Index) grpc.StreamServerInterceptor {
	return func(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if isHealthMethod(info.FullMethod) {
			return handler(service, stream)
		}
		ctx, err := authenticatedContext(stream.Context(), index)
		if err != nil {
			return err
		}
		return handler(service, &contextServerStream{ServerStream: stream, ctx: ctx})
	}
}

func authenticatedContext(ctx context.Context, index *Index) (context.Context, error) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	principal, ok := index.Authenticate(token)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	requestID, err := newRequestID()
	if err != nil {
		return nil, status.Error(codes.Unavailable, "service unavailable")
	}
	return WithRequestID(WithPrincipal(ctx, principal), requestID), nil
}

func newRequestID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy[:]), nil
}

func isHealthMethod(method string) bool {
	return strings.HasPrefix(method, healthServicePrefix)
}

type contextServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *contextServerStream) Context() context.Context { return stream.ctx }
