package rpcstatus_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorMapsDomainFailuresToStableStatuses(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    codes.Code
		message string
	}{
		{"unauthenticated", rpcstatus.ErrUnauthenticated, codes.Unauthenticated, "authentication required"},
		{"denied", policy.ErrDenied, codes.PermissionDenied, "permission denied"},
		{"invalid", policy.ErrRefPolicy, codes.InvalidArgument, "invalid request"},
		{"unsupported", rpcstatus.ErrUnsupported, codes.Unimplemented, "unsupported operation"},
		{"repository", rpcstatus.ErrRepositoryUnavailable, codes.Unavailable, "repository unavailable"},
		{"provider", runner.ErrCommandFailed, codes.Unavailable, "provider failure"},
		{"deadline", context.DeadlineExceeded, codes.DeadlineExceeded, "deadline exceeded"},
		{"limit", runner.ErrOutputLimit, codes.ResourceExhausted, "request limit exceeded"},
		{"service", rpcstatus.ErrServiceUnavailable, codes.Unavailable, "service unavailable"},
		{"canceled", context.Canceled, codes.Canceled, "request canceled"},
		{"unknown", errors.New("raw provider secret"), codes.Internal, "internal failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := rpcstatus.Error(fmt.Errorf("wrapped unsafe detail: %w", test.err))
			if status.Code(err) != test.code || status.Convert(err).Message() != test.message {
				t.Fatalf("Error() = (%v, %q), want (%v, %q)", status.Code(err), status.Convert(err).Message(), test.code, test.message)
			}
		})
	}
	if rpcstatus.Error(nil) != nil {
		t.Fatal("Error(nil) is non-nil")
	}
}

func TestErrorSanitizesExistingGRPCStatus(t *testing.T) {
	err := rpcstatus.Error(status.Error(codes.PermissionDenied, "repository secret-repo exists"))
	if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "permission denied" {
		t.Fatalf("Error() = %v", err)
	}
}
