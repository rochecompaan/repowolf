// Package rpcstatus converts trusted domain failures to stable, sanitized gRPC statuses.
package rpcstatus

import (
	"context"
	"errors"

	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrInvalidArgument       = errors.New("invalid argument")
	ErrUnsupported           = errors.New("unsupported operation")
	ErrRepositoryUnavailable = errors.New("repository unavailable")
	ErrResourceExhausted     = errors.New("resource exhausted")
	ErrServiceUnavailable    = errors.New("service unavailable")
)

// Error maps err without preserving untrusted error text.
func Error(err error) error {
	if err == nil {
		return nil
	}
	if mapped := mapDomainError(err); mapped != nil {
		return mapped
	}
	if existing, ok := status.FromError(err); ok {
		return canonical(existing.Code())
	}
	return status.Error(codes.Internal, "internal failure")
}

func mapDomainError(err error) error {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "authentication required")
	case errors.Is(err, policy.ErrDenied):
		return status.Error(codes.PermissionDenied, "permission denied")
	case errors.Is(err, ErrInvalidArgument), errors.Is(err, policy.ErrRefPolicy), errors.Is(err, runner.ErrInvalidCommand):
		return status.Error(codes.InvalidArgument, "invalid request")
	case errors.Is(err, ErrUnsupported):
		return status.Error(codes.Unimplemented, "unsupported operation")
	case errors.Is(err, ErrRepositoryUnavailable):
		return status.Error(codes.Unavailable, "repository unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, ErrResourceExhausted), errors.Is(err, runner.ErrInputLimit), errors.Is(err, runner.ErrOutputLimit):
		return status.Error(codes.ResourceExhausted, "request limit exceeded")
	case errors.Is(err, ErrServiceUnavailable):
		return status.Error(codes.Unavailable, "service unavailable")
	case errors.Is(err, runner.ErrStartFailed), errors.Is(err, runner.ErrCommandFailed), errors.Is(err, runner.ErrCleanupFailed):
		return status.Error(codes.Unavailable, "provider failure")
	default:
		return nil
	}
}

func canonical(code codes.Code) error {
	switch code {
	case codes.Unauthenticated:
		return status.Error(code, "authentication required")
	case codes.PermissionDenied:
		return status.Error(code, "permission denied")
	case codes.InvalidArgument:
		return status.Error(code, "invalid request")
	case codes.Unimplemented:
		return status.Error(code, "unsupported operation")
	case codes.Unavailable:
		return status.Error(code, "service unavailable")
	case codes.DeadlineExceeded:
		return status.Error(code, "deadline exceeded")
	case codes.ResourceExhausted:
		return status.Error(code, "request limit exceeded")
	case codes.Canceled:
		return status.Error(code, "request canceled")
	default:
		return status.Error(codes.Internal, "internal failure")
	}
}
