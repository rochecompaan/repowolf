package server

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
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
	policy   *policy.Snapshot
	executor GitHubExecutor
	audit    audit.Sink
}

func newGitHubService(snapshot *policy.Snapshot, executor GitHubExecutor, sink audit.Sink) *githubService {
	return &githubService{policy: snapshot, executor: executor, audit: sink}
}

func (service *githubService) Execute(ctx context.Context, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	if service == nil || service.policy == nil || service.executor == nil || service.audit == nil {
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
	principal, ok := auth.Principal(ctx)
	if !ok {
		return nil, rpcstatus.ErrUnauthenticated
	}
	repository, err := service.policy.Resolve(principal, selector, capability)
	if err != nil {
		return nil, policy.ErrDenied
	}
	if repository.Provider.Kind != config.ProviderGitHub {
		return nil, policy.ErrDenied
	}
	requestID, _ := auth.RequestID(ctx)
	if err := service.audit.Write(audit.Event{RequestID: requestID, Principal: principal, Provider: string(config.ProviderGitHub), Repository: repository.ID, Operation: operation, Outcome: audit.OutcomeAccepted}); err != nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	response, err := service.executor.Execute(ctx, repository, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, rpcstatus.ErrRepositoryUnavailable
	}
	response.Meta = &repowolfv1.ResponseMeta{RequestId: requestID}
	return response, nil
}

func githubSelector(request *repowolfv1.GitHubRequest) (policy.Selector, error) {
	if request.Context == nil || request.Context.Repository == nil {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	repository := request.Context.Repository
	if repository.Host == "" || repository.Owner == "" || repository.Name == "" || repository.SshPort != 0 {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	return policy.Selector{Kind: config.ProviderGitHub, Host: repository.Host, Owner: repository.Owner, Name: repository.Name}, nil
}
