package gitea

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/proto"
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

func commentURLMatches(rawURL, owner, repo string, index int64, apiURL bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" {
		return false
	}
	want := path.Join(owner, repo, "issues", strconv.FormatInt(index, 10))
	got := strings.TrimPrefix(path.Clean(parsed.EscapedPath()), "/")
	if !apiURL {
		return got == want
	}
	return got == want || got == path.Join("api", "v1", "repos", want)
}

func normalizeIssueMutationComment(comment *sdk.Comment, owner, repo string, index int64) (*repowolfv1.GiteaCommentRecord, error) {
	if comment == nil || comment.PRURL != "" || comment.IssueURL != "" && !commentURLMatches(comment.IssueURL, owner, repo, index, true) || !commentURLMatches(comment.HTMLURL, owner, repo, index, false) {
		return nil, fmt.Errorf("invalid issue comment association")
	}
	return normalizeComment(comment)
}

func (a *RepositoryAdapter) issueComment(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaIssueCommentRequest) (*repowolfv1.GiteaResponse, error) {
	if _, err := a.issuePreflight(ctx, repository, request.Index); err != nil {
		return nil, err
	}
	owner, name := repository.Repository.Owner, repository.Repository.Name
	var response *repowolfv1.GiteaResponse
	_, err := invokeWrite(ctx, func() (*sdk.Comment, error) {
		return a.api.CreateIssueComment(ctx, owner, name, request.Index, sdk.CreateIssueCommentOption{Body: request.Body})
	}, func(comment *sdk.Comment) error {
		record, e := normalizeIssueMutationComment(comment, owner, name, request.Index)
		if e != nil {
			return e
		}
		response = &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentResult{Comment: record}}}
		if proto.Size(response) > 8<<20 {
			return rpcstatus.ErrResourceExhausted
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func issueStateResponse(target sdk.StateType, record *repowolfv1.GiteaIssueRecord) *repowolfv1.GiteaResponse {
	if target == sdk.StateClosed {
		return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueClose{IssueClose: &repowolfv1.GiteaIssueCloseResult{Issue: record}}}
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueReopen{IssueReopen: &repowolfv1.GiteaIssueReopenResult{Issue: record}}}
}

func (a *RepositoryAdapter) issueState(ctx context.Context, repository policy.ResolvedRepository, index int64, target sdk.StateType) (*repowolfv1.GiteaResponse, error) {
	current, err := a.issuePreflight(ctx, repository, index)
	if err != nil {
		return nil, err
	}
	targetState := repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN
	if target == sdk.StateClosed {
		targetState = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED
	}
	var response *repowolfv1.GiteaResponse
	if current.state == targetState {
		response = issueStateResponse(target, projectIssue(current, allIssueFields))
		if proto.Size(response) > 8<<20 {
			return nil, rpcstatus.ErrProviderFailure
		}
		recordTransition(ctx, false)
	} else {
		owner, name := repository.Repository.Owner, repository.Repository.Name
		_, err = invokeWrite(ctx, func() (*sdk.Issue, error) {
			return a.api.EditIssue(ctx, owner, name, index, sdk.EditIssueOption{State: &target})
		}, func(issue *sdk.Issue) error {
			normalized, e := normalizeIssue(issue, owner, name)
			if e != nil || normalized.index != index || normalized.state != targetState {
				return rpcstatus.ErrProviderFailure
			}
			response = issueStateResponse(target, projectIssue(normalized, allIssueFields))
			if proto.Size(response) > 8<<20 {
				return rpcstatus.ErrResourceExhausted
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		recordTransition(ctx, true)
	}
	return response, nil
}
