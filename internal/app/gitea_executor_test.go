package app

import (
	"context"
	"errors"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/server"
)

type recordingGiteaExecutor struct {
	calls    int
	requests []*repowolfv1.GiteaRequest
}

func (f *recordingGiteaExecutor) Execute(_ context.Context, _ policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	f.calls++
	f.requests = append(f.requests, request)
	return &repowolfv1.GiteaResponse{}, nil
}
func TestGiteaExecutorDispatchesPullReadsOnlyByTrustedProviderID(t *testing.T) {
	first, second := &recordingGiteaExecutor{}, &recordingGiteaExecutor{}
	executor := &giteaExecutor{adapters: map[string]server.GiteaExecutor{"first": first, "second": second}}
	repository := policy.ResolvedRepository{Repository: config.Repository{Provider: "second"}}
	requests := []*repowolfv1.GiteaRequest{
		{Operation: &repowolfv1.GiteaRequest_PullList{PullList: &repowolfv1.GiteaPullListRequest{State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Page: 2, Limit: 50}}},
		{Operation: &repowolfv1.GiteaRequest_PullView{PullView: &repowolfv1.GiteaPullViewRequest{Index: 7, IncludeComments: true}}},
	}
	for _, request := range requests {
		if _, err := executor.Execute(context.Background(), repository, request); err != nil {
			t.Fatal(err)
		}
	}
	if first.calls != 0 || second.calls != 2 || second.requests[0].GetPullList().Page != 2 || second.requests[1].GetPullView().Index != 7 {
		t.Fatalf("calls=%d,%d requests=%#v", first.calls, second.calls, second.requests)
	}
	repository.Repository.Provider = "missing"
	if _, err := executor.Execute(context.Background(), repository, requests[0]); !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
