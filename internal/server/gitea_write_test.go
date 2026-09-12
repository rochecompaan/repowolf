package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGiteaWriteAuditUnknownOutcome(t *testing.T) {
	if got := auditOutcome(rpcstatus.ErrWriteOutcomeUnknown); got != audit.OutcomeUnknown {
		t.Fatalf("outcome=%q", got)
	}
	if got := auditOutcome(context.Canceled); got != audit.OutcomeCancelled {
		t.Fatalf("cancelled=%q", got)
	}
	if got := auditOutcome(errors.New("provider secret")); got != audit.OutcomeFailed {
		t.Fatalf("failed=%q", got)
	}
}
func TestGiteaWriteAuditTransitionMetadata(t *testing.T) {
	value := false
	ctx := withProviderMetadata(context.Background(), "gitea.issue_close", 1)
	metadata := providerMetadataFrom(ctx)
	metadata.transitioned = &value
	sink := &eventSink{}
	service := &Server{audit: sink}
	if err := service.writeTerminal(ctx, "ignored", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 || sink.events[0].Transitioned == nil || *sink.events[0].Transitioned {
		t.Fatalf("events=%#v", sink.events)
	}
}
func TestGiteaCompletedWriteAuditSurvivesDeliveryLimit(t *testing.T) {
	ctx := withProviderMetadata(context.Background(), "gitea.issue_create", 1)
	providerMetadataFrom(ctx).providerCompleted = true
	sink := &eventSink{}
	service := &Server{audit: sink}
	if err := service.writeTerminal(ctx, "ignored", time.Now(), rpcstatus.ErrResourceExhausted); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 || sink.events[0].Outcome != audit.OutcomeCompleted {
		t.Fatalf("events=%#v", sink.events)
	}
}

func TestAuditUnknownCanonicalStatus(t *testing.T) {
	mapped := rpcstatus.Error(rpcstatus.ErrWriteOutcomeUnknown)
	if status.Code(mapped) != codes.Unavailable || !rpcstatus.IsGiteaWriteOutcomeUnknown(mapped) {
		t.Fatalf("mapped=%v", mapped)
	}
}
