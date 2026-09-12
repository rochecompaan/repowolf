package gitea

import (
	"context"
	"errors"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func TestWriteOutcomeUnknownAfterSingleCreateAttempt(t *testing.T) {
	marker := "provider secret"
	api := &fakeWriteAPI{createErr: errors.New(marker)}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title"})
	if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.createCalls != 1 {
		t.Fatalf("err=%v calls=%d", err, api.createCalls)
	}
	if err.Error() == marker {
		t.Fatal("provider error leaked")
	}
}
func TestWriteOutcomeKnownBeforeInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &fakeWriteAPI{}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.issueCreate(ctx, issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title"})
	if !errors.Is(err, context.Canceled) || api.createCalls != 0 {
		t.Fatalf("err=%v calls=%d", err, api.createCalls)
	}
}
