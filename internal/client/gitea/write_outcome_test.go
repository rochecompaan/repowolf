package gitea

import (
	"testing"

	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMutationOutcomeUnknownClassification(t *testing.T) {
	for _, err := range []error{rpcstatus.Error(rpcstatus.ErrWriteOutcomeUnknown), status.Error(codes.Unavailable, "secret"), status.Error(codes.Canceled, "secret"), status.Error(codes.DeadlineExceeded, "secret"), status.Error(codes.Internal, "secret"), status.Error(codes.ResourceExhausted, "secret")} {
		if !mutationOutcomeUnknown(err) {
			t.Fatalf("not unknown: %v", err)
		}
	}
	for _, err := range []error{status.Error(codes.PermissionDenied, "secret"), status.Error(codes.FailedPrecondition, "secret"), status.Error(codes.InvalidArgument, "secret")} {
		if mutationOutcomeUnknown(err) {
			t.Fatalf("unexpected unknown: %v", err)
		}
	}
}
