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
)

type fakeGitHubExecutor struct {
	calls      int
	repository policy.ResolvedRepository
	response   *repowolfv1.GitHubResponse
	err        error
}

func (fake *fakeGitHubExecutor) Execute(_ context.Context, repository policy.ResolvedRepository, _ *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	fake.calls++
	fake.repository = repository
	return fake.response, fake.err
}

type eventSink struct{ events []audit.Event }

func (sink *eventSink) Write(event audit.Event) error {
	sink.events = append(sink.events, event)
	return nil
}

func TestNewRegistersGitHubServiceWhenDependenciesConfigured(t *testing.T) {
	service := testServer(t, Options{Policy: githubPolicy(t, config.IssuesRead, config.ProviderGitHub), GitHub: &fakeGitHubExecutor{}})
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GitHubService"]; !ok {
		t.Fatal("GitHub service was not registered")
	}
}

func TestGitHubServiceAuthorizesExactSelectorAndCapabilityBeforeExecution(t *testing.T) {
	snapshot := githubPolicy(t, config.IssuesRead, config.ProviderGitHub)
	executor := &fakeGitHubExecutor{response: &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueView{IssueView: &repowolfv1.GitHubIssueViewResult{}}}}
	sink := &eventSink{}
	service := newGitHubService(snapshot, executor, sink)
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request-id")
	request := githubServerRequest(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}})
	response, err := service.Execute(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.repository.ID != "project" {
		t.Fatalf("execution = %d %#v", executor.calls, executor.repository)
	}
	if response.GetMeta().GetRequestId() != "request-id" {
		t.Fatalf("meta = %#v", response.GetMeta())
	}
	if len(sink.events) != 1 || sink.events[0].Outcome != audit.OutcomeAccepted || sink.events[0].Repository != "project" || sink.events[0].Operation != "github.issue_view" {
		t.Fatalf("events = %#v", sink.events)
	}
}

func TestGitHubServiceDeniesUnknownRepositoryCapabilityAndProviderWithoutExecution(t *testing.T) {
	for _, test := range []struct {
		name       string
		capability config.Capability
		kind       config.ProviderKind
		mutate     func(*repowolfv1.GitHubRequest)
	}{
		{"repository", config.IssuesRead, config.ProviderGitHub, func(request *repowolfv1.GitHubRequest) { request.Context.Repository.Name = "other" }},
		{"capability", config.RepositoryRead, config.ProviderGitHub, func(*repowolfv1.GitHubRequest) {}},
		{"provider", config.IssuesRead, config.ProviderKind("other"), func(*repowolfv1.GitHubRequest) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := githubPolicy(t, test.capability, test.kind)
			executor := &fakeGitHubExecutor{}
			service := newGitHubService(snapshot, executor, &eventSink{})
			request := githubServerRequest(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}})
			test.mutate(request)
			_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
			if !errors.Is(err, policy.ErrDenied) {
				t.Fatalf("Execute()=%v", err)
			}
			if executor.calls != 0 {
				t.Fatalf("calls=%d", executor.calls)
			}
		})
	}
}

func TestGitHubServiceRejectsMalformedTypedRequestBeforeExecution(t *testing.T) {
	executor := &fakeGitHubExecutor{}
	service := newGitHubService(githubPolicy(t, config.IssuesRead, config.ProviderGitHub), executor, &eventSink{})
	request := githubServerRequest(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{}})
	if _, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request); err == nil {
		t.Fatal("invalid request accepted")
	}
	if executor.calls != 0 {
		t.Fatalf("calls=%d", executor.calls)
	}
}

func githubServerRequest(operation any) *repowolfv1.GitHubRequest {
	request := requestWithOperation(operation)
	request.Context = &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Host: "github.example", Owner: "owner", Name: "repo"}}
	return request
}
func requestWithOperation(operation any) *repowolfv1.GitHubRequest {
	request := &repowolfv1.GitHubRequest{}
	switch value := operation.(type) {
	case *repowolfv1.GitHubRequest_IssueView:
		request.Operation = value
	}
	return request
}
func githubPolicy(t *testing.T, capability config.Capability, kind config.ProviderKind) *policy.Snapshot {
	t.Helper()
	snapshot, err := policy.New(config.Config{Providers: map[string]config.Provider{"provider": {Kind: kind, APIHost: "github.example", GitHost: "github.example", SSHPort: 22}}, Repositories: map[string]config.Repository{"project": {Provider: "provider", Owner: "owner", Name: "repo"}}, Principals: map[string]config.Principal{"agent": {Grants: []config.Grant{{Repository: "project", Capabilities: []config.Capability{capability}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
