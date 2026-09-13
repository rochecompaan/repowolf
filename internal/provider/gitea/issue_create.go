package gitea

import (
	"context"
	"fmt"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/proto"
)

func (a *RepositoryAdapter) resolveLabelIDs(ctx context.Context, owner, repo string, requested []string) ([]int64, error) {
	if len(requested) == 0 {
		return []int64{}, nil
	}
	byName := map[string]int64{}
	ids := map[int64]bool{}
	complete := false
	for page := 1; page <= 21; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		labels, callErr := a.api.ListRepoLabels(ctx, owner, repo, sdk.ListLabelsOptions{ListOptions: sdk.ListOptions{Page: page, PageSize: 50}})
		if callErr != nil {
			return nil, classifyProviderError(ctx, callErr)
		}
		if len(labels) > 50 {
			return nil, rpcstatus.ErrProviderFailure
		}
		if page == 21 {
			if len(labels) != 0 {
				return nil, rpcstatus.ErrFailedPrecondition
			}
			complete = true
			break
		}
		for _, label := range labels {
			if label == nil || label.ID <= 0 || label.Name == "" || !validProviderString(label.Name) || ids[label.ID] {
				return nil, rpcstatus.ErrProviderFailure
			}
			if _, exists := byName[label.Name]; exists {
				return nil, rpcstatus.ErrProviderFailure
			}
			ids[label.ID] = true
			byName[label.Name] = label.ID
		}
		if len(labels) < 50 {
			complete = true
			break
		}
	}
	if !complete {
		return nil, rpcstatus.ErrProviderFailure
	}
	out := make([]int64, len(requested))
	for i, name := range requested {
		id, ok := byName[name]
		if !ok {
			return nil, rpcstatus.ErrFailedPrecondition
		}
		out[i] = id
	}
	return out, nil
}

func (a *RepositoryAdapter) issueCreate(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaIssueCreateRequest) (*repowolfv1.GiteaResponse, error) {
	owner, name := repository.Repository.Owner, repository.Repository.Name
	labels, err := a.resolveLabelIDs(ctx, owner, name, request.Labels)
	if err != nil {
		return nil, err
	}
	option := sdk.CreateIssueOption{Title: request.Title, Body: request.GetDescription(), Assignees: append([]string(nil), request.Assignees...), Labels: append([]int64(nil), labels...)}
	var response *repowolfv1.GiteaResponse
	_, err = invokeWrite(ctx, func() (*sdk.Issue, error) { return a.api.CreateIssue(ctx, owner, name, option) }, func(issue *sdk.Issue) error {
		normalized, e := normalizeIssue(issue, owner, name)
		if e != nil {
			return e
		}
		response = &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueCreate{IssueCreate: &repowolfv1.GiteaIssueCreateResult{Issue: projectIssue(normalized, allIssueFields)}}}
		if proto.Size(response) > 8<<20 {
			return rpcstatus.ErrResourceExhausted
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("unreachable")
	}
	return response, nil
}
