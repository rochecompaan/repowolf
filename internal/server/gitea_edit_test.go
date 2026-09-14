package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestGiteaIssueEditAuditLifecycle(t *testing.T) {
	tests := []struct {
		name        string
		response    *repowolfv1.GiteaResponse
		err         error
		wantOutcome audit.Outcome
		wantErr     error
	}{
		{name: "completed", response: giteaEditResponse(), wantOutcome: audit.OutcomeCompleted},
		{name: "trusted partial", err: rpcstatus.ErrEditPartial, wantOutcome: audit.OutcomePartial, wantErr: rpcstatus.ErrEditPartial},
		{name: "unknown", err: rpcstatus.ErrWriteOutcomeUnknown, wantOutcome: audit.OutcomeUnknown, wantErr: rpcstatus.ErrWriteOutcomeUnknown},
		{name: "untrusted partial lookalike", err: status.Error(codes.FailedPrecondition, "MARKER-partially-applied"), wantOutcome: audit.OutcomeFailed},
		{name: "cancelled", err: context.Canceled, wantOutcome: audit.OutcomeCancelled, wantErr: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := &eventSink{}
			executor := &fakeGiteaExecutor{response: test.response, err: test.err, before: func() {
				if len(sink.events) != 1 || sink.events[0].Outcome != audit.OutcomeAccepted || sink.events[0].Operation != "gitea.issue_edit" {
					t.Fatalf("accepted event before provider = %#v", sink.events)
				}
			}}
			service := newGiteaService(giteaPolicy(t, config.IssuesWrite, config.ProviderGitea), executor, sink)
			server := &Server{audit: sink}
			ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
			request := giteaEditRequest()
			request.GetIssueEdit().Title = stringPointer("MARKER-title")
			response, err := server.auditUnaryInterceptor()(ctx, request, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.GiteaService/Execute"}, func(ctx context.Context, _ any) (any, error) {
				return service.Execute(ctx, request)
			})
			if test.wantErr != nil && !errors.Is(err, test.wantErr) || test.wantErr == nil && test.err == nil && err != nil {
				t.Fatalf("response=%#v err=%v", response, err)
			}
			if len(sink.events) != 2 || sink.events[0].Outcome != audit.OutcomeAccepted || sink.events[1].Outcome != test.wantOutcome || sink.events[1].Operation != "gitea.issue_edit" {
				t.Fatalf("events=%#v", sink.events)
			}
			encoded, marshalErr := json.Marshal(sink.events)
			if marshalErr != nil || strings.Contains(string(encoded), "MARKER") || strings.Contains(string(encoded), "IssueEdit") {
				t.Fatalf("audit leaked request/provider data: %s (%v)", encoded, marshalErr)
			}
		})
	}
}

func TestGiteaIssueEditResponseLimitAuditsConfirmedCompletion(t *testing.T) {
	sink := &eventSink{}
	response := giteaEditResponse()
	response.GetIssueEdit().Issue.Body = strings.Repeat("x", responseLimitBytes)
	executor := &fakeGiteaExecutor{response: response}
	service := newGiteaService(giteaPolicy(t, config.IssuesWrite, config.ProviderGitea), executor, sink)
	server := &Server{audit: sink}
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
	request := giteaEditRequest()
	got, err := server.auditUnaryInterceptor()(ctx, request, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.GiteaService/Execute"}, func(ctx context.Context, _ any) (any, error) {
		return service.Execute(ctx, request)
	})
	if !errors.Is(err, runner.ErrOutputLimit) || len(sink.events) != 2 || sink.events[1].Outcome != audit.OutcomeCompleted {
		t.Fatalf("response=%#v err=%v events=%#v", got, err, sink.events)
	}
}

func giteaEditResponse() *repowolfv1.GiteaResponse {
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueEdit{IssueEdit: &repowolfv1.GiteaIssueEditResult{Issue: &repowolfv1.GiteaIssueRecord{Index: 7, Title: "edited", State: repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN, Url: "https://gitea.test/Owner/Repo/issues/7"}}}}
}

func stringPointer(value string) *string { return &value }
