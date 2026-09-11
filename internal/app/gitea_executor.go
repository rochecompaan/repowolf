package app

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/server"
)

type giteaExecutor struct {
	adapters map[string]server.GiteaExecutor
}

func (executor *giteaExecutor) Execute(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	if executor == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	adapter, ok := executor.adapters[repository.Repository.Provider]
	if !ok || adapter == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	return adapter.Execute(ctx, repository, request)
}
