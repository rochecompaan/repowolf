package rpcstatus

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGiteaWriteOutcomeUnknownStatus(t *testing.T) {
	first := Error(ErrWriteOutcomeUnknown)
	if !IsGiteaWriteOutcomeUnknown(first) {
		t.Fatalf("status=%v", first)
	}
	second := Error(first)
	if !IsGiteaWriteOutcomeUnknown(second) {
		t.Fatalf("second=%v", second)
	}
	if !errors.Is(first, ErrWriteOutcomeUnknown) {
		t.Fatal("trusted identity lost")
	}
}
func TestArbitraryUnknownDetailIsNotTrusted(t *testing.T) {
	value, _ := status.New(codes.Unavailable, "write outcome unknown").WithDetails(&errdetails.ErrorInfo{Reason: giteaWriteOutcomeUnknownReason, Domain: giteaWriteOutcomeUnknownDomain})
	mapped := Error(value.Err())
	if IsGiteaWriteOutcomeUnknown(mapped) {
		t.Fatalf("untrusted detail preserved: %v", mapped)
	}
}
