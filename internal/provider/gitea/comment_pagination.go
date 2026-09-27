package gitea

import (
	"context"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const (
	commentPageSize     = 50
	maximumCommentPages = 20
	maximumComments     = commentPageSize * maximumCommentPages
)

func loadIssueComments(ctx context.Context, api giteaAPI, owner, repo string, index int64, issue *repowolfv1.GiteaIssueRecord) ([]*repowolfv1.GiteaCommentRecord, error) {
	if issue == nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	return loadOrdinaryComments(ctx, api, owner, repo, index, issue.CommentCount)
}
func loadPullComments(ctx context.Context, api giteaAPI, owner, repo string, index, expected int64) ([]*repowolfv1.GiteaCommentRecord, error) {
	return loadOrdinaryComments(ctx, api, owner, repo, index, expected)
}
func loadOrdinaryComments(ctx context.Context, api giteaAPI, owner, repo string, index, expected int64) ([]*repowolfv1.GiteaCommentRecord, error) {
	if expected < 0 {
		return nil, rpcstatus.ErrProviderFailure
	}
	comments := make([]*repowolfv1.GiteaCommentRecord, 0)
	var last int64
	for page := 1; page <= maximumCommentPages+1; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values, err := api.ListIssueTimeline(ctx, owner, repo, index, sdk.ListIssueCommentOptions{ListOptions: sdk.ListOptions{Page: page, PageSize: commentPageSize}})
		if err != nil {
			return nil, classifyProviderError(ctx, err)
		}
		if page == maximumCommentPages+1 {
			if values.entryCount != 0 {
				return nil, runner.ErrOutputLimit
			}
			return completeOrdinaryComments(expected, comments)
		}
		if values.entryCount > commentPageSize || len(values.comments) > values.entryCount {
			return nil, rpcstatus.ErrProviderFailure
		}
		normalized, next, err := normalizeCommentPage(values.comments, last)
		if err != nil {
			return nil, err
		}
		comments = append(comments, normalized...)
		last = next
		if values.entryCount < commentPageSize {
			return completeOrdinaryComments(expected, comments)
		}
	}
	return nil, runner.ErrOutputLimit
}
func normalizeCommentPage(values []*sdk.Comment, last int64) ([]*repowolfv1.GiteaCommentRecord, int64, error) {
	normalized := make([]*repowolfv1.GiteaCommentRecord, len(values))
	next := last
	for i, value := range values {
		comment, err := normalizeComment(value)
		if err != nil || comment.Id <= next {
			return nil, 0, rpcstatus.ErrProviderFailure
		}
		normalized[i] = comment
		next = comment.Id
	}
	return normalized, next, nil
}
func completeOrdinaryComments(expected int64, comments []*repowolfv1.GiteaCommentRecord) ([]*repowolfv1.GiteaCommentRecord, error) {
	if int64(len(comments)) != expected {
		return nil, rpcstatus.ErrProviderFailure
	}
	return comments, nil
}
