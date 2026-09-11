package server

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

type providerLifecycle struct {
	policy *policy.Snapshot
	audit  audit.Sink
}

func (lifecycle providerLifecycle) Resolve(ctx context.Context, selector policy.Selector, capability config.Capability, expectedKind config.ProviderKind, operation string) (policy.ResolvedRepository, error) {
	principal, ok := auth.Principal(ctx)
	if !ok {
		return policy.ResolvedRepository{}, rpcstatus.ErrUnauthenticated
	}
	metadata := providerMetadataFrom(ctx)
	if metadata != nil {
		metadata.operation = operation
	}
	repository, err := lifecycle.policy.Resolve(principal, selector, capability)
	if err != nil || repository.Provider.Kind != expectedKind {
		return policy.ResolvedRepository{}, policy.ErrDenied
	}
	if metadata != nil {
		metadata.provider = string(expectedKind)
		metadata.repository = repository.ID
	}
	requestID, _ := auth.RequestID(ctx)
	if err := lifecycle.audit.Write(audit.Event{RequestID: requestID, Principal: principal, Provider: string(expectedKind), Repository: repository.ID, Operation: operation, Outcome: audit.OutcomeAccepted}); err != nil {
		return policy.ResolvedRepository{}, rpcstatus.ErrServiceUnavailable
	}
	return repository, nil
}
func (lifecycle providerLifecycle) Complete(ctx context.Context, response proto.Message, setMeta func(*repowolfv1.ResponseMeta)) error {
	if response == nil || !response.ProtoReflect().IsValid() {
		return rpcstatus.ErrRepositoryUnavailable
	}
	requestID, _ := auth.RequestID(ctx)
	setMeta(&repowolfv1.ResponseMeta{RequestId: requestID})
	if proto.Size(response) > responseLimitBytes {
		return runner.ErrOutputLimit
	}
	return nil
}
