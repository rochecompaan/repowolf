package gitea

import (
	sdk "code.gitea.io/sdk/gitea"
	"context"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const (
	commentPageSize     = 50
	maximumCommentPages = 20
	maximumComments     = commentPageSize * maximumCommentPages
)

func loadIssueComments(ctx context.Context, api issueAPI, owner, repo string, index int64) ([]*repowolfv1.GiteaCommentRecord, error) {
	comments := make([]*repowolfv1.GiteaCommentRecord, 0)
	var last int64
	for page := 1; page <= maximumCommentPages+1; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values, err := api.ListIssueComments(ctx, owner, repo, index, sdk.ListIssueCommentOptions{ListOptions: sdk.ListOptions{Page: page, PageSize: commentPageSize}})
		if err != nil {
			return nil, classifyProviderError(ctx, err)
		}
		if len(values) > commentPageSize {
			return nil, rpcstatus.ErrProviderFailure
		}
		if page == maximumCommentPages+1 {
			if len(values) != 0 {
				return nil, runner.ErrOutputLimit
			}
			return comments, nil
		}
		normalized := make([]*repowolfv1.GiteaCommentRecord, len(values))
		next := last
		for i, v := range values {
			c, e := normalizeComment(v)
			if e != nil || c.Id <= next {
				return nil, rpcstatus.ErrProviderFailure
			}
			normalized[i] = c
			next = c.Id
		}
		comments = append(comments, normalized...)
		last = next
		if len(values) < commentPageSize {
			return comments, nil
		}
	}
	return nil, runner.ErrOutputLimit
}
