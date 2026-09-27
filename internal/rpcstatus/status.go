// Package rpcstatus converts trusted domain failures to stable, sanitized gRPC statuses.
package rpcstatus

import (
	"context"
	"errors"

	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	issueKindReason                = "GITEA_ISSUE_KIND_PULL_REQUEST"
	issueKindDomain                = "repowolf.dev/gitea"
	giteaWriteOutcomeUnknownReason = "GITEA_WRITE_OUTCOME_UNKNOWN"
	giteaWriteOutcomeUnknownDomain = "repowolf.dev/gitea"
	giteaEditPartialReason         = "GITEA_EDIT_PARTIAL"
	giteaEditPartialDomain         = "repowolf.dev/gitea"
)

var (
	ErrUnauthenticated       = errors.New("unauthenticated")
	ErrInvalidArgument       = errors.New("invalid argument")
	ErrUnsupported           = errors.New("unsupported operation")
	ErrRepositoryUnavailable = errors.New("repository unavailable")
	ErrProviderFailure       = errors.New("provider failure")
	ErrNotFound              = errors.New("not found")
	ErrIssueKind             = errors.New("issue kind mismatch")
	ErrFailedPrecondition    = errors.New("operation precondition failed")
	ErrWriteOutcomeUnknown   = errors.New("write outcome unknown")
	ErrEditPartial           = errors.New("issue edit partially applied")
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
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, "not found")
	case errors.Is(err, ErrIssueKind):
		return issueKindError()
	case errors.Is(err, ErrFailedPrecondition):
		return status.Error(codes.FailedPrecondition, "operation precondition failed")
	case errors.Is(err, ErrWriteOutcomeUnknown):
		return writeOutcomeUnknownError()
	case errors.Is(err, ErrEditPartial):
		return editPartialError()
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, ErrResourceExhausted), errors.Is(err, runner.ErrInputLimit), errors.Is(err, runner.ErrOutputLimit):
		return status.Error(codes.ResourceExhausted, "request limit exceeded")
	case errors.Is(err, ErrServiceUnavailable):
		return status.Error(codes.Unavailable, "service unavailable")
	case errors.Is(err, ErrProviderFailure):
		return status.Error(codes.Unavailable, "provider failure")
	case errors.Is(err, runner.ErrStartFailed), errors.Is(err, runner.ErrCommandFailed), errors.Is(err, runner.ErrCleanupFailed):
		return status.Error(codes.Unavailable, "provider failure")
	default:
		return nil
	}
}

func issueKindError() error {
	value, err := status.New(codes.FailedPrecondition, "index is a pull request; use tea pulls").WithDetails(&errdetails.ErrorInfo{
		Reason: issueKindReason,
		Domain: issueKindDomain,
	})
	if err != nil {
		return status.Error(codes.Internal, "internal failure")
	}
	return value.Err()
}

type trustedWriteOutcomeUnknown struct{ status *status.Status }

func (e trustedWriteOutcomeUnknown) Error() string              { return e.status.Err().Error() }
func (e trustedWriteOutcomeUnknown) GRPCStatus() *status.Status { return e.status }
func (trustedWriteOutcomeUnknown) Is(target error) bool         { return target == ErrWriteOutcomeUnknown }

func writeOutcomeUnknownError() error {
	value, err := status.New(codes.Unavailable, "write outcome unknown").WithDetails(&errdetails.ErrorInfo{Reason: giteaWriteOutcomeUnknownReason, Domain: giteaWriteOutcomeUnknownDomain})
	if err != nil {
		return status.Error(codes.Internal, "internal failure")
	}
	return trustedWriteOutcomeUnknown{status: value}
}

type trustedEditPartial struct{ status *status.Status }

func (e trustedEditPartial) Error() string              { return e.status.Err().Error() }
func (e trustedEditPartial) GRPCStatus() *status.Status { return e.status }
func (trustedEditPartial) Is(target error) bool         { return target == ErrEditPartial }

func editPartialError() error {
	value, err := status.New(codes.FailedPrecondition, "issue edit partially applied").WithDetails(&errdetails.ErrorInfo{Reason: giteaEditPartialReason, Domain: giteaEditPartialDomain})
	if err != nil {
		return status.Error(codes.Internal, "internal failure")
	}
	return trustedEditPartial{status: value}
}

// IsGiteaEditPartial recognizes only the exact safe status tuple.
func IsGiteaEditPartial(err error) bool {
	value, ok := status.FromError(err)
	if !ok || value.Code() != codes.FailedPrecondition || value.Message() != "issue edit partially applied" || len(value.Details()) != 1 {
		return false
	}
	info, ok := value.Details()[0].(*errdetails.ErrorInfo)
	return ok && info.Reason == giteaEditPartialReason && info.Domain == giteaEditPartialDomain
}

// IsGiteaWriteOutcomeUnknown recognizes only the exact safe status tuple.
func IsGiteaWriteOutcomeUnknown(err error) bool {
	value, ok := status.FromError(err)
	if !ok || value.Code() != codes.Unavailable || value.Message() != "write outcome unknown" || len(value.Details()) != 1 {
		return false
	}
	info, ok := value.Details()[0].(*errdetails.ErrorInfo)
	return ok && info.Reason == giteaWriteOutcomeUnknownReason && info.Domain == giteaWriteOutcomeUnknownDomain
}

// IsIssueKindStatus recognizes only the trusted structured issue-kind status.
func IsIssueKindStatus(err error) bool {
	value, ok := status.FromError(err)
	if !ok || value.Code() != codes.FailedPrecondition {
		return false
	}
	for _, detail := range value.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if ok && info.Reason == issueKindReason && info.Domain == issueKindDomain {
			return true
		}
	}
	return false
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
	case codes.NotFound:
		return status.Error(code, "not found")
	case codes.FailedPrecondition:
		return status.Error(code, "operation precondition failed")
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
