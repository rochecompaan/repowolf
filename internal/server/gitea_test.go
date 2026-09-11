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
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeGiteaExecutor struct {
	calls      int
	repository policy.ResolvedRepository
	response   *repowolfv1.GiteaResponse
	err        error
	before     func()
}

func (f *fakeGiteaExecutor) Execute(_ context.Context, r policy.ResolvedRepository, _ *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	f.calls++
	f.repository = r
	if f.before != nil {
		f.before()
	}
	return f.response, f.err
}
func giteaRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}}, Operation: &repowolfv1.GiteaRequest_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewRequest{}}}
}
func giteaIssueListRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{
		Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}},
		Operation: &repowolfv1.GiteaRequest_IssueList{IssueList: &repowolfv1.GiteaIssueListRequest{
			State:  repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN,
			Page:   1,
			Limit:  30,
			Fields: []repowolfv1.GiteaIssueField{repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_INDEX},
		}},
	}
}

func giteaIssueViewRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{
		Context:   &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}},
		Operation: &repowolfv1.GiteaRequest_IssueView{IssueView: &repowolfv1.GiteaIssueViewRequest{Index: 7}},
	}
}

func giteaPolicy(t *testing.T, capability config.Capability, kind config.ProviderKind) *policy.Snapshot {
	t.Helper()
	snapshot, err := policy.New(config.Config{Providers: map[string]config.Provider{"gitea": {Kind: kind, APIHost: "gitea.example", GitHost: "gitea.example", SSHPort: 22}}, Repositories: map[string]config.Repository{"project": {Provider: "gitea", Owner: "Owner", Name: "Repo"}, "ungranted": {Provider: "gitea", Owner: "Owner", Name: "OtherRepo"}}, Principals: map[string]config.Principal{"agent": {Grants: []config.Grant{{Repository: "project", Capabilities: []config.Capability{capability}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func TestGiteaServiceAuthorizesAndCompletes(t *testing.T) {
	for _, identity := range []struct {
		name  string
		owner string
		repo  string
	}{
		{name: "configured casing", owner: "Owner", repo: "Repo"},
		{name: "case folded", owner: "owner", repo: "REPO"},
	} {
		t.Run(identity.name, func(t *testing.T) {
			executor := &fakeGiteaExecutor{response: &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: &repowolfv1.GiteaRepositoryRecord{}}}}}
			service := newGiteaService(giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), executor, &eventSink{})
			ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
			request := giteaRequest()
			request.Context.Repository.Owner = identity.owner
			request.Context.Repository.Name = identity.repo
			response, err := service.Execute(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			if executor.calls != 1 || executor.repository.ID != "project" || executor.repository.Repository.Owner != "Owner" || executor.repository.Repository.Name != "Repo" || response.GetMeta().GetRequestId() != "request" {
				t.Fatalf("calls=%d repository=%#v response=%#v", executor.calls, executor.repository, response)
			}
		})
	}
}
func TestGiteaServiceRejectsInvalidRepositorySelectorsBeforePolicy(t *testing.T) {
	for _, test := range []struct {
		name  string
		owner string
		repo  string
	}{
		{name: "Unicode confusable", owner: "Kelvin", repo: "Repo"},
		{name: "NUL", owner: "Owner\x00", repo: "Repo"},
		{name: "dot owner", owner: ".", repo: "Repo"},
		{name: "dot-dot repository", owner: "Owner", repo: ".."},
		{name: "leading punctuation", owner: "-Owner", repo: "Repo"},
		{name: "Git suffix", owner: "Owner", repo: "Repo.GIT"},
		{name: "overlong owner", owner: strings.Repeat("a", 101), repo: "Repo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeGiteaExecutor{}
			service := newGiteaService(giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), executor, &eventSink{})
			request := giteaRequest()
			request.Context.Repository.Owner = test.owner
			request.Context.Repository.Name = test.repo

			_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
			if !errors.Is(err, rpcstatus.ErrInvalidArgument) {
				t.Fatalf("Execute() error = %v, want invalid argument", err)
			}
			if executor.calls != 0 {
				t.Fatal("executor called")
			}
		})
	}
}

func TestGiteaServiceFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name   string
		cap    config.Capability
		kind   config.ProviderKind
		mutate func(*repowolfv1.GiteaRequest)
	}{{"unknown", config.RepositoryRead, config.ProviderGitea, func(r *repowolfv1.GiteaRequest) { r.Context.Repository.Name = "other" }}, {"capability", config.IssuesRead, config.ProviderGitea, func(*repowolfv1.GiteaRequest) {}}, {"kind", config.RepositoryRead, config.ProviderGitHub, func(*repowolfv1.GiteaRequest) {}}} {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeGiteaExecutor{}
			service := newGiteaService(giteaPolicy(t, test.cap, test.kind), executor, &eventSink{})
			request := giteaRequest()
			test.mutate(request)
			_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
			if !errors.Is(err, policy.ErrDenied) {
				t.Fatalf("err=%v", err)
			}
			if executor.calls != 0 {
				t.Fatal("executor called")
			}
		})
	}
	for _, mutate := range []func(*repowolfv1.GiteaRequest){func(r *repowolfv1.GiteaRequest) { r.Context.Repository.Host = "evil" }, func(r *repowolfv1.GiteaRequest) { r.Context.Repository.SshPort = 22 }, func(r *repowolfv1.GiteaRequest) { r.Context.Repository.SshUser = "git" }, func(r *repowolfv1.GiteaRequest) { r.Operation = nil }} {
		executor := &fakeGiteaExecutor{}
		service := newGiteaService(giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), executor, &eventSink{})
		request := giteaRequest()
		mutate(request)
		if _, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request); err == nil {
			t.Error("invalid request accepted")
		}
		if executor.calls != 0 {
			t.Fatal("executor called")
		}
	}
}
func TestGiteaIssueServiceAuthorizesAndAuditsReadOperations(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation string
		request   func() *repowolfv1.GiteaRequest
		response  *repowolfv1.GiteaResponse
	}{
		{
			name:      "list",
			operation: "gitea.issue_list",
			request:   giteaIssueListRequest,
			response:  &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{}}},
		},
		{
			name:      "view",
			operation: "gitea.issue_view",
			request:   giteaIssueViewRequest,
			response:  &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: &repowolfv1.GiteaIssueRecord{}}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &eventSink{}
			executor := &fakeGiteaExecutor{response: test.response, before: func() {
				if len(sink.events) != 1 || sink.events[0].Operation != test.operation || sink.events[0].Outcome != audit.OutcomeAccepted {
					t.Fatalf("audit before provider execution = %#v", sink.events)
				}
			}}
			service := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), executor, sink)
			ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
			response, err := service.Execute(ctx, test.request())
			if err != nil {
				t.Fatal(err)
			}
			if executor.calls != 1 || executor.repository.ID != "project" || response.GetMeta().GetRequestId() != "request" {
				t.Fatalf("calls=%d repository=%#v response=%#v", executor.calls, executor.repository, response)
			}
		})
	}
}

func TestGiteaIssueServiceDeniesBeforeProviderExecution(t *testing.T) {
	for _, test := range []struct {
		name   string
		cap    config.Capability
		kind   config.ProviderKind
		mutate func(*repowolfv1.GiteaRequest)
	}{
		{name: "unknown repository", cap: config.IssuesRead, kind: config.ProviderGitea, mutate: func(request *repowolfv1.GiteaRequest) { request.Context.Repository.Name = "Unknown" }},
		{name: "ungranted repository", cap: config.IssuesRead, kind: config.ProviderGitea, mutate: func(request *repowolfv1.GiteaRequest) { request.Context.Repository.Name = "OtherRepo" }},
		{name: "missing capability", cap: config.RepositoryRead, kind: config.ProviderGitea, mutate: func(*repowolfv1.GiteaRequest) {}},
		{name: "wrong provider kind", cap: config.IssuesRead, kind: config.ProviderGitHub, mutate: func(*repowolfv1.GiteaRequest) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeGiteaExecutor{}
			service := newGiteaService(giteaPolicy(t, test.cap, test.kind), executor, &eventSink{})
			request := giteaIssueListRequest()
			test.mutate(request)
			_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
			if !errors.Is(err, policy.ErrDenied) || executor.calls != 0 {
				t.Fatalf("Execute() = %v, calls=%d, want denied without provider call", err, executor.calls)
			}
		})
	}
}

func TestGiteaIssueServiceRejectsMalformedRequestsBeforePolicy(t *testing.T) {
	for _, request := range []*repowolfv1.GiteaRequest{giteaIssueListRequest(), giteaIssueViewRequest()} {
		if list := request.GetIssueList(); list != nil {
			list.State = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_UNSPECIFIED
		} else {
			request.GetIssueView().Index = 0
		}
		sink := &eventSink{}
		executor := &fakeGiteaExecutor{}
		service := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), executor, sink)
		_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
		if !errors.Is(err, rpcstatus.ErrInvalidArgument) || executor.calls != 0 || len(sink.events) != 0 {
			t.Fatalf("Execute() = %v, calls=%d audit=%#v", err, executor.calls, sink.events)
		}
	}
}

func TestGiteaIssueKindDiagnosticRequiresAuthorization(t *testing.T) {
	request := giteaIssueViewRequest()
	executor := &fakeGiteaExecutor{err: rpcstatus.ErrIssueKind}
	service := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), executor, &eventSink{})
	interceptors := testServer(t, Options{})
	info := &grpc.UnaryServerInfo{}
	_, err := interceptors.statusUnaryInterceptor()(auth.WithPrincipal(context.Background(), "agent"), request, info, func(ctx context.Context, _ any) (any, error) {
		return interceptors.deadlineUnaryInterceptor()(ctx, request, info, func(ctx context.Context, _ any) (any, error) {
			return service.Execute(ctx, request)
		})
	})
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "index is a pull request; use tea pulls" || executor.calls != 1 {
		t.Fatalf("authorized kind mismatch = %v, calls=%d", err, executor.calls)
	}

	deniedExecutor := &fakeGiteaExecutor{err: rpcstatus.ErrIssueKind}
	denied := newGiteaService(giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), deniedExecutor, &eventSink{})
	_, err = interceptors.statusUnaryInterceptor()(auth.WithPrincipal(context.Background(), "agent"), request, info, func(ctx context.Context, _ any) (any, error) {
		return interceptors.deadlineUnaryInterceptor()(ctx, request, info, func(ctx context.Context, _ any) (any, error) {
			return denied.Execute(ctx, request)
		})
	})
	if status.Code(err) != codes.PermissionDenied || status.Convert(err).Message() != "permission denied" || deniedExecutor.calls != 0 {
		t.Fatalf("unauthorized kind mismatch = %v, calls=%d", err, deniedExecutor.calls)
	}
}

func TestNewRegistersGiteaIndependently(t *testing.T) {
	service := testServer(t, Options{Policy: giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), Gitea: &fakeGiteaExecutor{}})
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GiteaService"]; !ok {
		t.Fatal("Gitea not registered")
	}
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GitHubService"]; ok {
		t.Fatal("GitHub registered")
	}
}
