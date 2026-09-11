package server

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	providergithub "github.com/rochecompaan/repowolf/internal/provider/github"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

// GitHubExecutor is the narrow provider adapter surface used by the service.
type GitHubExecutor interface {
	Execute(context.Context, policy.ResolvedRepository, *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error)
}

type githubService struct {
	repowolfv1.UnimplementedGitHubServiceServer
	lifecycle providerLifecycle
	executor  GitHubExecutor
}

func newGitHubService(snapshot *policy.Snapshot, executor GitHubExecutor, sink audit.Sink) *githubService {
	return &githubService{lifecycle: providerLifecycle{policy: snapshot, audit: sink}, executor: executor}
}
func (service *githubService) Execute(ctx context.Context, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	if service == nil || service.lifecycle.policy == nil || service.executor == nil || service.lifecycle.audit == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	if err := providergithub.ValidateGitHubRequest(request); err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	capability, err := providergithub.Capability(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	operation, err := providergithub.OperationName(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	selector, err := githubSelector(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	repository, err := service.lifecycle.Resolve(ctx, selector, capability, config.ProviderGitHub, operation)
	if err != nil {
		return nil, err
	}
	response, err := service.executor.Execute(ctx, repository, request)
	if err != nil {
		return nil, err
	}
	if err := service.lifecycle.Complete(ctx, response, func(meta *repowolfv1.ResponseMeta) { response.Meta = meta }); err != nil {
		return nil, err
	}
	return response, nil
}
func githubSelector(request *repowolfv1.GitHubRequest) (policy.Selector, error) {
	if request.Context == nil || request.Context.Repository == nil {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	repository := request.Context.Repository
	if repository.Host == "" || repository.Owner == "" || repository.Name == "" || repository.SshPort != 0 || repository.SshUser != "" {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	return policy.Selector{Kind: config.ProviderGitHub, Host: repository.Host, Owner: repository.Owner, Name: repository.Name}, nil
}
