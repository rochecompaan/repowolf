package gitea

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdk "gitea.dev/sdk"

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
func TestWriteOutcomeUnknownAcrossWriteOperations(t *testing.T) {
	marker := errors.New("provider secret")
	t.Run("comment call error", func(t *testing.T) {
		api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: sdkIssue()}, commentErr: marker}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueComment(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCommentRequest{Index: 7, Body: "body"})
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.commentCalls != 1 || strings.Contains(err.Error(), marker.Error()) {
			t.Fatalf("err=%v calls=%d", err, api.commentCalls)
		}
	})
	t.Run("state call error", func(t *testing.T) {
		api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: sdkIssue()}, editErr: marker}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueState(context.Background(), issueResolved(), 7, sdk.StateClosed, true)
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.editCalls != 1 || strings.Contains(err.Error(), marker.Error()) {
			t.Fatalf("err=%v calls=%d", err, api.editCalls)
		}
	})
	t.Run("malformed create response", func(t *testing.T) {
		api := &fakeWriteAPI{createResult: &sdk.Issue{}}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title"})
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.createCalls != 1 {
			t.Fatalf("err=%v calls=%d", err, api.createCalls)
		}
	})
	t.Run("malformed state response", func(t *testing.T) {
		result := sdkIssue()
		result.Index = 8
		api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: sdkIssue()}, editResult: result}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueState(context.Background(), issueResolved(), 7, sdk.StateClosed, true)
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.editCalls != 1 {
			t.Fatalf("err=%v calls=%d", err, api.editCalls)
		}
	})

	t.Run("oversized successful response", func(t *testing.T) {
		issue := sdkIssue()
		issue.Body = strings.Repeat("x", 8<<20)
		api := &fakeWriteAPI{createResult: issue}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title"})
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || api.createCalls != 1 {
			t.Fatalf("err=%v calls=%d", err, api.createCalls)
		}
	})
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
