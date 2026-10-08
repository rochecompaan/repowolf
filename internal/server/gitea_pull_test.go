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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func giteaPullListRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}}, Operation: &repowolfv1.GiteaRequest_PullList{PullList: &repowolfv1.GiteaPullListRequest{State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Page: 1, Limit: 30, Fields: []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX}}}}
}
func giteaPullViewRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}}, Operation: &repowolfv1.GiteaRequest_PullView{PullView: &repowolfv1.GiteaPullViewRequest{Index: 7}}}
}

func TestGiteaPullAuthorizationAndCanonicalResolution(t *testing.T) {
	response := &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_PullList{PullList: &repowolfv1.GiteaPullListResult{Pulls: []*repowolfv1.GiteaPullRecord{}}}}
	executor := &fakeGiteaExecutor{response: response}
	service := newGiteaService(giteaPolicy(t, config.PullRequestsRead, config.ProviderGitea), executor, &eventSink{})
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
	request := giteaPullListRequest()
	request.Context.Repository.Owner, request.Context.Repository.Name = "owner", "REPO"
	if _, err := service.Execute(ctx, request); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || executor.repository.Repository.Owner != "Owner" || executor.repository.Repository.Name != "Repo" {
		t.Fatalf("calls=%d repository=%#v", executor.calls, executor.repository)
	}
}

func TestGiteaPullDenialsAreIndistinguishableBeforeProviderExecution(t *testing.T) {
	for _, test := range []struct {
		name   string
		cap    config.Capability
		kind   config.ProviderKind
		mutate func(*repowolfv1.GiteaRequest)
	}{
		{name: "unknown repository", cap: config.PullRequestsRead, kind: config.ProviderGitea, mutate: func(r *repowolfv1.GiteaRequest) { r.Context.Repository.Name = "Unknown" }},
		{name: "ungranted repository", cap: config.PullRequestsRead, kind: config.ProviderGitea, mutate: func(r *repowolfv1.GiteaRequest) { r.Context.Repository.Name = "OtherRepo" }},
		{name: "missing capability", cap: config.IssuesRead, kind: config.ProviderGitea, mutate: func(*repowolfv1.GiteaRequest) {}},
		{name: "wrong provider", cap: config.PullRequestsRead, kind: config.ProviderGitHub, mutate: func(*repowolfv1.GiteaRequest) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &fakeGiteaExecutor{}
			service := newGiteaService(giteaPolicy(t, test.cap, test.kind), executor, &eventSink{})
			request := giteaPullListRequest()
			test.mutate(request)
			_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
			if !errors.Is(err, policy.ErrDenied) || executor.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, executor.calls)
			}
		})
	}
}

func TestGiteaPullMalformedRequestsFailBeforePolicyAndAudit(t *testing.T) {
	for _, request := range []*repowolfv1.GiteaRequest{giteaPullListRequest(), giteaPullViewRequest()} {
		if list := request.GetPullList(); list != nil {
			list.Limit = 51
		} else {
			request.GetPullView().Index = 0
		}
		sink := &eventSink{}
		executor := &fakeGiteaExecutor{}
		service := newGiteaService(giteaPolicy(t, config.PullRequestsRead, config.ProviderGitea), executor, sink)
		_, err := service.Execute(auth.WithPrincipal(context.Background(), "agent"), request)
		if !errors.Is(err, rpcstatus.ErrInvalidArgument) || executor.calls != 0 || len(sink.events) != 0 {
			t.Fatalf("err=%v calls=%d events=%#v", err, executor.calls, sink.events)
		}
	}
}

func TestGiteaPullAuditLifecycleUsesOnlyCanonicalMetadata(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation string
		request   *repowolfv1.GiteaRequest
		response  *repowolfv1.GiteaResponse
		err       error
		outcome   audit.Outcome
	}{
		{name: "list success", operation: "gitea.pull_list", request: giteaPullListRequest(), response: &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_PullList{PullList: &repowolfv1.GiteaPullListResult{}}}, outcome: audit.OutcomeCompleted},
		{name: "view failure", operation: "gitea.pull_view", request: giteaPullViewRequest(), err: errors.New("MARKER-provider-secret"), outcome: audit.OutcomeFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			sink := &eventSink{}
			executor := &fakeGiteaExecutor{response: test.response, err: test.err, before: func() {
				if len(sink.events) != 1 || sink.events[0].Operation != test.operation || sink.events[0].Outcome != audit.OutcomeAccepted {
					t.Fatalf("accepted event=%#v", sink.events)
				}
			}}
			service := newGiteaService(giteaPolicy(t, config.PullRequestsRead, config.ProviderGitea), executor, sink)
			server := &Server{audit: sink}
			ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
			_, _ = server.auditUnaryInterceptor()(ctx, test.request, &grpc.UnaryServerInfo{FullMethod: "/repowolf.v1.GiteaService/Execute"}, func(ctx context.Context, _ any) (any, error) {
				return service.Execute(ctx, test.request)
			})
			if len(sink.events) != 2 || sink.events[1].Operation != test.operation || sink.events[1].Outcome != test.outcome {
				t.Fatalf("events=%#v", sink.events)
			}
			encoded, err := json.Marshal(sink.events)
			if err != nil || strings.Contains(string(encoded), "MARKER") || strings.Contains(string(encoded), "PullView") {
				t.Fatalf("audit leaked data: %s (%v)", encoded, err)
			}
		})
	}
}

func TestGiteaPullKindDiagnosticRequiresAuthorization(t *testing.T) {
	request := giteaPullViewRequest()
	interceptors := testServer(t, Options{})
	info := &grpc.UnaryServerInfo{}
	executor := &fakeGiteaExecutor{err: rpcstatus.ErrPullKind}
	service := newGiteaService(giteaPolicy(t, config.PullRequestsRead, config.ProviderGitea), executor, &eventSink{})
	_, err := interceptors.statusUnaryInterceptor()(auth.WithPrincipal(context.Background(), "agent"), request, info, func(ctx context.Context, _ any) (any, error) { return service.Execute(ctx, request) })
	if status.Code(err) != codes.FailedPrecondition || status.Convert(err).Message() != "index is an issue; use tea issues" || executor.calls != 1 {
		t.Fatalf("authorized err=%v calls=%d", err, executor.calls)
	}
	deniedExecutor := &fakeGiteaExecutor{err: rpcstatus.ErrPullKind}
	denied := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), deniedExecutor, &eventSink{})
	_, err = interceptors.statusUnaryInterceptor()(auth.WithPrincipal(context.Background(), "agent"), request, info, func(ctx context.Context, _ any) (any, error) { return denied.Execute(ctx, request) })
	if status.Code(err) != codes.PermissionDenied || deniedExecutor.calls != 0 {
		t.Fatalf("denied err=%v calls=%d", err, deniedExecutor.calls)
	}
}
