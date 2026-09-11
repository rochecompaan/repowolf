package server

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	providergitea "github.com/rochecompaan/repowolf/internal/provider/gitea"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

// GiteaExecutor is the narrow provider adapter surface used by the service.
type GiteaExecutor interface {
	Execute(context.Context, policy.ResolvedRepository, *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error)
}
type giteaService struct {
	repowolfv1.UnimplementedGiteaServiceServer
	lifecycle providerLifecycle
	executor  GiteaExecutor
}

func newGiteaService(snapshot *policy.Snapshot, executor GiteaExecutor, sink audit.Sink) *giteaService {
	return &giteaService{lifecycle: providerLifecycle{policy: snapshot, audit: sink}, executor: executor}
}
func (service *giteaService) Execute(ctx context.Context, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	if service == nil || service.lifecycle.policy == nil || service.executor == nil || service.lifecycle.audit == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	if err := providergitea.ValidateRequest(request); err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	capability, err := providergitea.Capability(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	operation, err := providergitea.OperationName(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	selector, err := giteaSelector(request)
	if err != nil {
		return nil, rpcstatus.ErrInvalidArgument
	}
	repository, err := service.lifecycle.Resolve(ctx, selector, capability, config.ProviderGitea, operation)
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
func giteaSelector(request *repowolfv1.GiteaRequest) (policy.Selector, error) {
	if request.GetContext() == nil || request.GetContext().GetRepository() == nil {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	repository := request.GetContext().GetRepository()
	if repository.Owner == "" || repository.Name == "" || repository.Host != "" || repository.SshPort != 0 {
		return policy.Selector{}, rpcstatus.ErrInvalidArgument
	}
	return policy.Selector{Kind: config.ProviderGitea, Owner: repository.Owner, Name: repository.Name}, nil
}
