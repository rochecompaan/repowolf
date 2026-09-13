package rpcstatus

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGiteaEditPartialStatus(t *testing.T) {
	err := Error(ErrEditPartial)
	if !errors.Is(err, ErrEditPartial) || !IsGiteaEditPartial(err) {
		t.Fatalf("partial not preserved: %v", err)
	}
	value, _ := status.FromError(err)
	if value.Code() != codes.FailedPrecondition || value.Message() != "issue edit partially applied" || len(value.Details()) != 1 {
		t.Fatalf("status=%v", value)
	}
	info, ok := value.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.Reason != "GITEA_EDIT_PARTIAL" || info.Domain != "repowolf.dev/gitea" {
		t.Fatalf("detail=%#v", value.Details())
	}
}

func TestGiteaEditPartialRejectsNearMatches(t *testing.T) {
	for _, err := range []error{status.Error(codes.FailedPrecondition, "issue edit partially applied"), status.Error(codes.Unavailable, "issue edit partially applied")} {
		if IsGiteaEditPartial(err) {
			t.Fatalf("accepted %v", err)
		}
	}
}
