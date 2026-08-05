package server

import (
	"context"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func (service *Server) unaryInterceptors() []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		service.globalConcurrencyUnaryInterceptor(), auth.UnaryServerInterceptor(service.tokens), service.statusUnaryInterceptor(),
		service.admissionUnaryInterceptor(), service.concurrencyUnaryInterceptor(),
		service.deadlineUnaryInterceptor(), service.auditUnaryInterceptor(),
	}
}

func (service *Server) streamInterceptors() []grpc.StreamServerInterceptor {
	return []grpc.StreamServerInterceptor{
		service.globalConcurrencyStreamInterceptor(), auth.StreamServerInterceptor(service.tokens), service.statusStreamInterceptor(),
		service.admissionStreamInterceptor(), service.concurrencyStreamInterceptor(),
		service.deadlineStreamInterceptor(), service.auditStreamInterceptor(),
	}
}

func (service *Server) statusUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		response, err := handler(ctx, request)
		return response, rpcstatus.Error(err)
	}
}

func (service *Server) statusStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return rpcstatus.Error(handler(implementation, stream))
	}
}

func (service *Server) admissionUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		admitted, done, err := service.admit(ctx)
		if err != nil {
			return nil, service.auditRejection(ctx, info.FullMethod, err)
		}
		defer done()
		return handler(admitted, request)
	}
}

func (service *Server) admissionStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		admitted, done, err := service.admit(stream.Context())
		if err != nil {
			return service.auditRejection(stream.Context(), info.FullMethod, err)
		}
		defer done()
		return handler(implementation, &serverStreamContext{ServerStream: stream, ctx: admitted})
	}
}

func (service *Server) admit(parent context.Context) (context.Context, func(), error) {
	if service.stopping.Load() {
		return nil, nil, rpcstatus.ErrServiceUnavailable
	}
	ctx, cancel := context.WithCancel(parent)
	service.activeMu.Lock()
	if service.stopping.Load() {
		service.activeMu.Unlock()
		cancel()
		return nil, nil, rpcstatus.ErrServiceUnavailable
	}
	service.nextID++
	id := service.nextID
	service.active[id] = cancel
	service.activeMu.Unlock()
	return ctx, func() {
		cancel()
		service.activeMu.Lock()
		delete(service.active, id)
		service.activeMu.Unlock()
	}, nil
}

func (service *Server) deadlineUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := context.WithTimeout(ctx, service.timeout)
		defer cancel()
		response, err := handler(ctx, request)
		return response, rpcstatus.Error(err)
	}
}

func (service *Server) deadlineStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, cancel := context.WithTimeout(stream.Context(), service.timeout)
		defer cancel()
		return rpcstatus.Error(handler(implementation, &serverStreamContext{ServerStream: stream, ctx: ctx}))
	}
}

func isHealth(method string) bool {
	return method == grpc_health_v1.Health_Check_FullMethodName || method == grpc_health_v1.Health_Watch_FullMethodName
}

type serverStreamContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *serverStreamContext) Context() context.Context { return stream.ctx }
