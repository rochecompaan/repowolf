package server

import (
	"context"
	"errors"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func giteaEditRequest() *repowolfv1.GiteaRequest {
	title := "edited"
	return &repowolfv1.GiteaRequest{
		Context:   &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}},
		Operation: &repowolfv1.GiteaRequest_IssueEdit{IssueEdit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title}},
	}
}

func TestGiteaIssueEditRequiresWriteAuthorization(t *testing.T) {
	for _, capability := range []config.Capability{config.RepositoryRead, config.IssuesRead} {
		executor := &fakeGiteaExecutor{}
		service := newGiteaService(giteaPolicy(t, capability, config.ProviderGitea), executor, &eventSink{})
		_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), giteaEditRequest())
		if !errors.Is(err, policy.ErrDenied) || executor.calls != 0 {
			t.Fatalf("capability=%q calls=%d err=%v", capability, executor.calls, err)
		}
	}
}

func TestGiteaIssueEditRoutesWithTrustedOperation(t *testing.T) {
	sink := &eventSink{}
	executor := &fakeGiteaExecutor{response: &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueEdit{IssueEdit: &repowolfv1.GiteaIssueEditResult{Issue: &repowolfv1.GiteaIssueRecord{}}}}}
	service := newGiteaService(giteaPolicy(t, config.IssuesWrite, config.ProviderGitea), executor, sink)
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
	if _, err := service.Execute(ctx, giteaEditRequest()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.repository.ID != "project" || len(sink.events) != 1 || sink.events[0].Operation != "gitea.issue_edit" || sink.events[0].Outcome != audit.OutcomeAccepted {
		t.Fatalf("calls=%d repository=%#v events=%#v", executor.calls, executor.repository, sink.events)
	}
}

func TestGiteaIssueEditMalformedBeforePolicy(t *testing.T) {
	sink := &eventSink{}
	executor := &fakeGiteaExecutor{}
	service := newGiteaService(giteaPolicy(t, config.IssuesWrite, config.ProviderGitea), executor, sink)
	request := giteaEditRequest()
	request.GetIssueEdit().Title = nil
	_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
	if !errors.Is(err, rpcstatus.ErrInvalidArgument) || executor.calls != 0 || len(sink.events) != 0 {
		t.Fatalf("calls=%d events=%#v err=%v", executor.calls, sink.events, err)
	}
}

func TestGiteaIssueEditAuditOutcomes(t *testing.T) {
	if got := auditOutcome(rpcstatus.ErrEditPartial); got != audit.OutcomePartial {
		t.Fatalf("partial outcome=%q", got)
	}
	if got := auditOutcome(rpcstatus.ErrWriteOutcomeUnknown); got != audit.OutcomeUnknown {
		t.Fatalf("unknown outcome=%q", got)
	}
}
