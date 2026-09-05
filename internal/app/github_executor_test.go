package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/server"
)

type recordingExecutor struct {
	calls      int
	repository policy.ResolvedRepository
	response   *repowolfv1.GitHubResponse
}

func (executor *recordingExecutor) Execute(_ context.Context, repository policy.ResolvedRepository, _ *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	executor.calls++
	executor.repository = repository
	return executor.response, nil
}

func TestGitHubExecutorDispatchesByResolvedProviderID(t *testing.T) {
	first := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
	second := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
	executor := &githubExecutor{adapters: map[string]server.GitHubExecutor{
		"github-a": first,
		"github-b": second,
	}}
	repository := policy.ResolvedRepository{
		Repository: config.Repository{Provider: "github-b"},
	}

	if _, err := executor.Execute(context.Background(), repository, &repowolfv1.GitHubRequest{}); err != nil {
		t.Fatal(err)
	}
	if first.calls != 0 {
		t.Fatalf("first.calls = %d, want 0", first.calls)
	}
	if second.calls != 1 {
		t.Fatalf("second.calls = %d, want 1", second.calls)
	}
	if !reflect.DeepEqual(second.repository, repository) {
		t.Fatalf("second.repository = %#v, want %#v", second.repository, repository)
	}
}

func TestGitHubExecutorRejectsMissingResolvedProviderAdapter(t *testing.T) {
	first := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
	second := &recordingExecutor{response: &repowolfv1.GitHubResponse{}}
	executor := &githubExecutor{adapters: map[string]server.GitHubExecutor{
		"github-a": first,
		"github-b": second,
	}}

	_, err := executor.Execute(context.Background(), policy.ResolvedRepository{
		Repository: config.Repository{Provider: "missing"},
	}, &repowolfv1.GitHubRequest{})
	if !errors.Is(err, rpcstatus.ErrServiceUnavailable) {
		t.Fatalf("Execute() error = %v, want service unavailable", err)
	}
	if first.calls != 0 || second.calls != 0 {
		t.Fatalf("calls = %d, %d, want 0, 0", first.calls, second.calls)
	}
}
