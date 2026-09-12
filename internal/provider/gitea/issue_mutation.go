package gitea

import (
	"context"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type mutationMetadataKey struct{}

// MutationMetadata carries only trusted bounded facts from the adapter to audit.
type MutationMetadata struct{ Transitioned *bool }

func WithMutationMetadata(ctx context.Context) (context.Context, *MutationMetadata) {
	value := &MutationMetadata{}
	return context.WithValue(ctx, mutationMetadataKey{}, value), value
}
func recordTransition(ctx context.Context, value bool) {
	if metadata, ok := ctx.Value(mutationMetadataKey{}).(*MutationMetadata); ok {
		v := value
		metadata.Transitioned = &v
	}
}

func (a *RepositoryAdapter) issuePreflight(ctx context.Context, repository policy.ResolvedRepository, index int64) (*normalizedIssue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, name := repository.Repository.Owner, repository.Repository.Name
	issue, statusCode, err := a.api.GetIssue(ctx, owner, name, index)
	if err != nil {
		if statusCode == 404 {
			return nil, rpcstatus.ErrNotFound
		}
		return nil, classifyProviderError(ctx, err)
	}
	if issue == nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	if issue.PullRequest != nil {
		return nil, rpcstatus.ErrIssueKind
	}
	normalized, err := normalizeIssue(issue, owner, name)
	if err != nil || normalized.index != index {
		return nil, rpcstatus.ErrProviderFailure
	}
	return normalized, nil
}

func (a *RepositoryAdapter) issueComment(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaIssueCommentRequest) (*repowolfv1.GiteaResponse, error) {
	if _, err := a.issuePreflight(ctx, repository, request.Index); err != nil {
		return nil, err
	}
	api, err := a.writeAPI()
	if err != nil {
		return nil, err
	}
	owner, name := repository.Repository.Owner, repository.Repository.Name
	var record *repowolfv1.GiteaCommentRecord
	_, err = invokeWrite(ctx, func() (*sdk.Comment, error) {
		return api.CreateIssueComment(ctx, owner, name, request.Index, sdk.CreateIssueCommentOption{Body: request.Body})
	}, func(comment *sdk.Comment) error { var e error; record, e = normalizeComment(comment); return e })
	if err != nil {
		return nil, err
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentResult{Comment: record}}}, nil
}

func (a *RepositoryAdapter) issueState(ctx context.Context, repository policy.ResolvedRepository, index int64, target sdk.StateType, closing bool) (*repowolfv1.GiteaResponse, error) {
	current, err := a.issuePreflight(ctx, repository, index)
	if err != nil {
		return nil, err
	}
	targetState := repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN
	if target == sdk.StateClosed {
		targetState = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED
	}
	var record *repowolfv1.GiteaIssueRecord
	if current.state == targetState {
		record = projectIssue(current, allIssueFields)
		recordTransition(ctx, false)
	} else {
		api, e := a.writeAPI()
		if e != nil {
			return nil, e
		}
		owner, name := repository.Repository.Owner, repository.Repository.Name
		_, err = invokeWrite(ctx, func() (*sdk.Issue, error) {
			return api.EditIssue(ctx, owner, name, index, sdk.EditIssueOption{State: &target})
		}, func(issue *sdk.Issue) error {
			normalized, e := normalizeIssue(issue, owner, name)
			if e != nil || normalized.index != index || normalized.state != targetState {
				return rpcstatus.ErrProviderFailure
			}
			record = projectIssue(normalized, allIssueFields)
			return nil
		})
		if err != nil {
			return nil, err
		}
		recordTransition(ctx, true)
	}
	if closing {
		return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueClose{IssueClose: &repowolfv1.GiteaIssueCloseResult{Issue: record}}}, nil
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueReopen{IssueReopen: &repowolfv1.GiteaIssueReopenResult{Issue: record}}}, nil
}
