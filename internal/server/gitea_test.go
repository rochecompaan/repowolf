package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type fakeGiteaExecutor struct {
	calls      int
	repository policy.ResolvedRepository
	response   *repowolfv1.GiteaResponse
	err        error
}

func (f *fakeGiteaExecutor) Execute(_ context.Context, r policy.ResolvedRepository, _ *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	f.calls++
	f.repository = r
	return f.response, f.err
}
func giteaRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}}, Operation: &repowolfv1.GiteaRequest_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewRequest{}}}
}
func giteaPolicy(t *testing.T, capability config.Capability, kind config.ProviderKind) *policy.Snapshot {
	t.Helper()
	snapshot, err := policy.New(config.Config{Providers: map[string]config.Provider{"gitea": {Kind: kind, APIHost: "gitea.example", GitHost: "gitea.example", SSHPort: 22}}, Repositories: map[string]config.Repository{"project": {Provider: "gitea", Owner: "Owner", Name: "Repo"}}, Principals: map[string]config.Principal{"agent": {Grants: []config.Grant{{Repository: "project", Capabilities: []config.Capability{capability}}}}}})
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
func TestNewRegistersGiteaIndependently(t *testing.T) {
	service := testServer(t, Options{Policy: giteaPolicy(t, config.RepositoryRead, config.ProviderGitea), Gitea: &fakeGiteaExecutor{}})
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GiteaService"]; !ok {
		t.Fatal("Gitea not registered")
	}
	if _, ok := service.grpc.GetServiceInfo()["repowolf.v1.GitHubService"]; ok {
		t.Fatal("GitHub registered")
	}
}
