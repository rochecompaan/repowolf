package server

import (
	"context"
	"sync"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc"
)

type concurrencyLimits struct {
	global chan struct{}
	perMax int
	mu     sync.Mutex
	per    map[string]int
}

func newConcurrencyLimits(global, perPrincipal int) concurrencyLimits {
	return concurrencyLimits{global: make(chan struct{}, global), perMax: perPrincipal, per: make(map[string]int)}
}

func (limits *concurrencyLimits) acquire(principal string, perPrincipal bool) bool {
	select {
	case limits.global <- struct{}{}:
	default:
		return false
	}
	if !perPrincipal {
		return true
	}
	limits.mu.Lock()
	defer limits.mu.Unlock()
	if limits.per[principal] >= limits.perMax {
		<-limits.global
		return false
	}
	limits.per[principal]++
	return true
}

func (limits *concurrencyLimits) release(principal string, perPrincipal bool) {
	if perPrincipal {
		limits.mu.Lock()
		limits.per[principal]--
		if limits.per[principal] == 0 {
			delete(limits.per, principal)
		}
		limits.mu.Unlock()
	}
	<-limits.global
}

func (service *Server) concurrencyUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		principal, perPrincipal, ok := concurrencyPrincipal(ctx, info.FullMethod)
		if !ok || !service.limits.acquire(principal, perPrincipal) {
			return nil, rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal, perPrincipal)
		return handler(ctx, request)
	}
}

func (service *Server) concurrencyStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		principal, perPrincipal, ok := concurrencyPrincipal(stream.Context(), info.FullMethod)
		if !ok || !service.limits.acquire(principal, perPrincipal) {
			return rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal, perPrincipal)
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
