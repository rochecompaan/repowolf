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
	"google.golang.org/protobuf/proto"
)

func (service *Server) auditUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		var inputBytes int64
		if message, ok := request.(proto.Message); ok && message != nil {
			inputBytes = int64(proto.Size(message))
		}
		ctx = withProviderMetadata(ctx, info.FullMethod, inputBytes)
		response, err := handler(ctx, request)
		if message, ok := response.(proto.Message); ok && message != nil {
			providerMetadataFrom(ctx).outputBytes = int64(proto.Size(message))
		}
		if writeErr := service.writeTerminal(ctx, info.FullMethod, started, err); writeErr != nil {
			return nil, status.Error(codes.Unavailable, "audit unavailable")
		}
		return response, err
	}
}

func (service *Server) auditStreamInterceptor() grpc.StreamServerInterceptor {
	return func(implementation any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		started := time.Now()
		err := handler(implementation, stream)
		if writeErr := service.writeTerminal(stream.Context(), info.FullMethod, started, err); writeErr != nil {
			return status.Error(codes.Unavailable, "audit unavailable")
		}
		return err
	}
}

// auditRejection writes the only terminal event for authenticated calls that
// are rejected before the terminal audit interceptor is entered.
func (service *Server) auditRejection(ctx context.Context, operation string, err error) error {
	if _, ok := auth.Principal(ctx); !ok {
		return rpcstatus.Error(err)
	}
	if writeErr := service.writeTerminal(ctx, operation, time.Now(), err); writeErr != nil {
		return status.Error(codes.Unavailable, "audit unavailable")
	}
	return rpcstatus.Error(err)
}

func (service *Server) writeTerminal(ctx context.Context, operation string, started time.Time, err error) error {
	principal, _ := auth.Principal(ctx)
	requestID, _ := auth.RequestID(ctx)
	mapped := rpcstatus.Error(err)
	event := audit.Event{
		RequestID: requestID, Principal: principal, Operation: operation,
		Outcome: auditOutcome(mapped), Reason: status.Code(mapped).String(),
		DurationMS: time.Since(started).Milliseconds(),
	}
	if metadata := providerMetadataFrom(ctx); metadata != nil {
		event.Operation = metadata.operation
		event.Provider = metadata.provider
		event.Repository = metadata.repository
		event.InputBytes = metadata.inputBytes
		event.OutputBytes = metadata.outputBytes
	}
	return service.audit.Write(event)
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
