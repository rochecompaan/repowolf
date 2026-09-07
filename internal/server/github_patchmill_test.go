package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var patchmillServerOperations = []struct {
	request    any
	capability config.Capability
	name       string
}{
	{&repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}, config.RepositoryRead, "github.current_user"},
	{&repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: 1000}}, config.IssuesRead, "github.label_list"},
	{&repowolfv1.GitHubRequest_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateRequest{Name: "patchmill:ready", Color: "1a2b3c", Description: "Ready"}}, config.IssuesWrite, "github.label_create"},
	{&repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: &repowolfv1.GitHubIssueLabelChangeRequest{Number: 18, AddLabels: []string{"patchmill:ready"}}}, config.IssuesWrite, "github.issue_label_change"},
}

func TestGitHubPatchmillPolicyAndAudit(t *testing.T) {
	for _, test := range patchmillServerOperations {
		t.Run(test.name, func(t *testing.T) {
			request := githubServerRequest(test.request)
			ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request-id")
			sink := &eventSink{}
			executor := &fakeGitHubExecutor{response: &repowolfv1.GitHubResponse{}, before: func() {
				if len(sink.events) != 1 || sink.events[0].Operation != test.name || sink.events[0].Outcome != audit.OutcomeAccepted {
					t.Fatalf("audit before provider execution = %#v", sink.events)
				}
			}}
			service := newGitHubService(githubPolicy(t, test.capability, config.ProviderGitHub), executor, sink)
			if _, err := service.Execute(ctx, request); err != nil {
				t.Fatalf("approved Execute() = %v", err)
			}
			if executor.calls != 1 || executor.repository.ID != "project" {
				t.Fatalf("approved execution = %d %#v", executor.calls, executor.repository)
			}

			missing := config.RepositoryRead
			if missing == test.capability {
				missing = config.IssuesRead
			}
			deniedExecutor := &fakeGitHubExecutor{}
			denied := newGitHubService(githubPolicy(t, missing, config.ProviderGitHub), deniedExecutor, &eventSink{})
			if _, err := denied.Execute(auth.WithPrincipal(context.Background(), "agent"), request); !errors.Is(err, policy.ErrDenied) {
				t.Fatalf("missing capability Execute() = %v, want denied", err)
			}
			if deniedExecutor.calls != 0 {
				t.Fatalf("missing capability calls = %d", deniedExecutor.calls)
			}

			providerError := errors.New("sensitive provider detail")
			failing := newGitHubService(githubPolicy(t, test.capability, config.ProviderGitHub), &fakeGitHubExecutor{err: providerError}, &eventSink{})
			_, err := testServer(t, Options{}).statusUnaryInterceptor()(auth.WithPrincipal(context.Background(), "agent"), request, &grpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
				return failing.Execute(ctx, request)
			})
			if status.Code(err) != codes.Internal || status.Convert(err).Message() != "internal failure" || strings.Contains(err.Error(), providerError.Error()) {
				t.Fatalf("provider error = %v", err)
			}
		})
	}
}

func TestGitHubPatchmillResponseLimit(t *testing.T) {
	const requestID = "request-id"
	for _, test := range patchmillServerOperations {
		t.Run(test.name, func(t *testing.T) {
			request := githubServerRequest(test.request)
			executor := &fakeGitHubExecutor{response: githubResponseWithFinalSize(t, responseLimitBytes+1, requestID)}
			service := newGitHubService(githubPolicy(t, test.capability, config.ProviderGitHub), executor, &eventSink{})
			response, err := service.Execute(auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), requestID), request)
			if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
				t.Fatalf("Execute() = %#v, %v, want output limit", response, err)
			}
		})
	}
}
