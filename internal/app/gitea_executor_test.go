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

type recordingGiteaExecutor struct{ calls int }

func (f *recordingGiteaExecutor) Execute(context.Context, policy.ResolvedRepository, *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	f.calls++
	return &repowolfv1.GiteaResponse{}, nil
}
func TestGiteaExecutorDispatchesOnlyTrustedProviderID(t *testing.T) {
	first, second := &recordingGiteaExecutor{}, &recordingGiteaExecutor{}
	executor := &giteaExecutor{adapters: map[string]server.GiteaExecutor{"first": first, "second": second}}
	repository := policy.ResolvedRepository{Repository: config.Repository{Provider: "second"}}
	if _, err := executor.Execute(context.Background(), repository, &repowolfv1.GiteaRequest{}); err != nil {
		t.Fatal(err)
	}
	if first.calls != 0 || second.calls != 1 {
		t.Fatalf("calls=%d,%d", first.calls, second.calls)
	}
	repository.Repository.Provider = "missing"
	if _, err := executor.Execute(context.Background(), repository, &repowolfv1.GiteaRequest{}); !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
