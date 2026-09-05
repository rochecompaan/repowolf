package app

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/server"
)

// githubExecutor dispatches GitHub operations to the adapter selected by policy.
type githubExecutor struct {
	adapters map[string]server.GitHubExecutor
}

func (executor *githubExecutor) Execute(
	ctx context.Context,
	repository policy.ResolvedRepository,
	request *repowolfv1.GitHubRequest,
) (*repowolfv1.GitHubResponse, error) {
	if executor == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	adapter := executor.adapters[repository.Repository.Provider]
	if adapter == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	return adapter.Execute(ctx, repository, request)
}
