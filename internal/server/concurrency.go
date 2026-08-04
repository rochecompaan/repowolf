package server

import (
	"context"
	"sync"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc"
)

// concurrencyLimits owns only post-authentication principal capacity. The
// global lease is deliberately separate so it can bound authentication too.
type concurrencyLimits struct {
	perMax int
	mu     sync.Mutex
	per    map[string]int
}

func newConcurrencyLimits(_ int, perPrincipal int) concurrencyLimits {
	return concurrencyLimits{perMax: perPrincipal, per: make(map[string]int)}
}

func (limits *concurrencyLimits) acquire(principal string) bool {
	limits.mu.Lock()
	defer limits.mu.Unlock()
	if limits.per[principal] >= limits.perMax {
		return false
	}
	limits.per[principal]++
	return true
}

func (limits *concurrencyLimits) release(principal string) {
	limits.mu.Lock()
	limits.per[principal]--
	if limits.per[principal] == 0 {
		delete(limits.per, principal)
	}
	limits.mu.Unlock()
}

func (service *Server) acquireGlobal() bool {
	select {
	case service.global <- struct{}{}:
		return true
	default:
		return false
	}
}

func (service *Server) releaseGlobal() { <-service.global }

// globalConcurrency*Interceptor is intentionally first in the chain. It
// bounds all RPC work, including malformed and invalid bearer authentication.
func (service *Server) globalConcurrencyUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !service.acquireGlobal() {
			return nil, rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.releaseGlobal()
		return handler(ctx, request)
	}
}

func (service *Server) globalConcurrencyStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !service.acquireGlobal() {
			return rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.releaseGlobal()
		return handler(implementation, stream)
	}
}

func (service *Server) concurrencyUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		principal, perPrincipal, ok := concurrencyPrincipal(ctx, info.FullMethod)
		if !ok {
			return nil, rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		if !perPrincipal {
			return handler(ctx, request)
		}
		if !service.limits.acquire(principal) {
			return nil, service.auditRejection(ctx, info.FullMethod, rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal)
		return handler(ctx, request)
	}
}

func (service *Server) concurrencyStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		principal, perPrincipal, ok := concurrencyPrincipal(stream.Context(), info.FullMethod)
		if !ok {
			return rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		if !perPrincipal {
			return handler(implementation, stream)
		}
		if !service.limits.acquire(principal) {
			return service.auditRejection(stream.Context(), info.FullMethod, rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal)
		return handler(implementation, stream)
	}
}

func concurrencyPrincipal(ctx context.Context, method string) (string, bool, bool) {
	if isHealth(method) {
		return "", false, true
	}
	principal, ok := auth.Principal(ctx)
	return principal, true, ok
}
