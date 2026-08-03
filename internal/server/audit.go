package server

import (
	"context"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (service *Server) auditUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isHealth(info.FullMethod) {
			return handler(ctx, request)
		}
		started := time.Now()
		response, err := handler(ctx, request)
		if writeErr := service.writeTerminal(ctx, info.FullMethod, started, err); writeErr != nil {
			return nil, status.Error(codes.Unavailable, "audit unavailable")
		}
		return response, err
	}
}

func (service *Server) auditStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if isHealth(info.FullMethod) {
			return handler(implementation, stream)
		}
		started := time.Now()
		err := handler(implementation, stream)
		if writeErr := service.writeTerminal(stream.Context(), info.FullMethod, started, err); writeErr != nil {
			return status.Error(codes.Unavailable, "audit unavailable")
		}
		return err
	}
}

func (service *Server) writeTerminal(ctx context.Context, operation string, started time.Time, err error) error {
	principal, _ := auth.Principal(ctx)
	requestID, _ := auth.RequestID(ctx)
	mapped := rpcstatus.Error(err)
	return service.audit.Write(audit.Event{
		RequestID: requestID, Principal: principal, Operation: operation,
		Outcome: auditOutcome(mapped), Reason: status.Code(mapped).String(),
		DurationMS: time.Since(started).Milliseconds(),
	})
}

func auditOutcome(err error) audit.Outcome {
	switch status.Code(err) {
	case codes.OK:
		return audit.OutcomeCompleted
	case codes.PermissionDenied:
		return audit.OutcomeDenied
	case codes.Canceled, codes.DeadlineExceeded:
		return audit.OutcomeCancelled
	default:
		return audit.OutcomeFailed
	}
}
