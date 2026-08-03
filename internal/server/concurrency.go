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

func (limits *concurrencyLimits) acquire(principal string) bool {
	select {
	case limits.global <- struct{}{}:
	default:
		return false
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

func (limits *concurrencyLimits) release(principal string) {
	limits.mu.Lock()
	limits.per[principal]--
	if limits.per[principal] == 0 {
		delete(limits.per, principal)
	}
	limits.mu.Unlock()
	<-limits.global
}

func (service *Server) concurrencyUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isHealth(info.FullMethod) {
			return handler(ctx, request)
		}
		principal, ok := auth.Principal(ctx)
		if !ok || !service.limits.acquire(principal) {
			return nil, rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal)
		return handler(ctx, request)
	}
}

func (service *Server) concurrencyStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if isHealth(info.FullMethod) {
			return handler(implementation, stream)
		}
		principal, ok := auth.Principal(stream.Context())
		if !ok || !service.limits.acquire(principal) {
			return rpcstatus.Error(rpcstatus.ErrResourceExhausted)
		}
		defer service.limits.release(principal)
		return handler(implementation, stream)
	}
}
