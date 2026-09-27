package server

import (
	"context"
	"errors"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
)

func giteaPullListRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: "Owner", Name: "Repo"}}, Operation: &repowolfv1.GiteaRequest_PullList{PullList: &repowolfv1.GiteaPullListRequest{State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Page: 1, Limit: 30, Fields: []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX}}}}
}
func TestGiteaPullAuthorization(t *testing.T) {
	response := &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_PullList{PullList: &repowolfv1.GiteaPullListResult{Pulls: []*repowolfv1.GiteaPullRecord{}}}}
	executor := &fakeGiteaExecutor{response: response}
	service := newGiteaService(giteaPolicy(t, config.PullRequestsRead, config.ProviderGitea), executor, &eventSink{})
	ctx := auth.WithRequestID(auth.WithPrincipal(context.Background(), "agent"), "request")
	if _, err := service.Execute(ctx, giteaPullListRequest()); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 {
		t.Fatalf("calls=%d", executor.calls)
	}
	deniedExecutor := &fakeGiteaExecutor{}
	denied := newGiteaService(giteaPolicy(t, config.IssuesRead, config.ProviderGitea), deniedExecutor, &eventSink{})
	if _, err := denied.Execute(ctx, giteaPullListRequest()); !errors.Is(err, policy.ErrDenied) || deniedExecutor.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, deniedExecutor.calls)
	}
}
