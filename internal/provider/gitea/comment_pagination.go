package gitea

import (
	"context"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const (
	commentPageSize     = 50
	maximumCommentPages = 20
	maximumComments     = commentPageSize * maximumCommentPages
)

func loadIssueComments(ctx context.Context, api issueAPI, owner, repo string, index int64, issue *repowolfv1.GiteaIssueRecord) ([]*repowolfv1.GiteaCommentRecord, error) {
	if issue == nil || issue.CommentCount < 0 {
		return nil, rpcstatus.ErrProviderFailure
	}
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
		// Gitea 1.27 ignores page and limit on this endpoint. A first response
		// larger than the requested page proves that behavior and is the complete
		// collection; retain the same record and response-size bounds.
		unpaginated := page == 1 && len(values) > commentPageSize
		if len(values) > commentPageSize && !unpaginated {
			return nil, rpcstatus.ErrProviderFailure
		}
		if unpaginated && len(values) > maximumComments {
			return nil, runner.ErrOutputLimit
		}
		if page == maximumCommentPages+1 {
			if len(values) != 0 {
				return nil, runner.ErrOutputLimit
			}
			return completeIssueComments(issue, comments)
		}
		if page == 2 && issue.CommentCount == commentPageSize && len(comments) == commentPageSize && len(values) == commentPageSize {
			probe, _, err := normalizeCommentPage(values, 0)
			if err != nil {
				return nil, err
			}
			if equalComments(comments, probe) {
				return completeIssueComments(issue, comments)
			}
		}
		normalized, next, err := normalizeCommentPage(values, last)
		if err != nil {
			return nil, err
		}
		candidate := make([]*repowolfv1.GiteaCommentRecord, 0, len(comments)+len(normalized))
		candidate = append(candidate, comments...)
		candidate = append(candidate, normalized...)
		if issueViewResponseSize(issue, candidate) > int(maxResponseBytes) {
			return nil, runner.ErrOutputLimit
		}
		comments = candidate
		last = next
		if int64(len(comments)) > issue.CommentCount {
			return nil, rpcstatus.ErrProviderFailure
		}
		if unpaginated || len(values) < commentPageSize {
			return completeIssueComments(issue, comments)
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

func equalComments(left, right []*repowolfv1.GiteaCommentRecord) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !proto.Equal(left[i], right[i]) {
			return false
		}
	}
	return true
}

func completeIssueComments(issue *repowolfv1.GiteaIssueRecord, comments []*repowolfv1.GiteaCommentRecord) ([]*repowolfv1.GiteaCommentRecord, error) {
	if int64(len(comments)) != issue.CommentCount {
		return nil, rpcstatus.ErrProviderFailure
	}
	return comments, nil
}

func issueViewResponseSize(issue *repowolfv1.GiteaIssueRecord, comments []*repowolfv1.GiteaCommentRecord) int {
	candidate := proto.Clone(issue).(*repowolfv1.GiteaIssueRecord)
	candidate.Comments = comments
	// Production request IDs are 16 random bytes encoded as 32 hex digits.
	return proto.Size(&repowolfv1.GiteaResponse{
		Meta: &repowolfv1.ResponseMeta{RequestId: "00000000000000000000000000000000"},
		Result: &repowolfv1.GiteaResponse_IssueView{
			IssueView: &repowolfv1.GiteaIssueViewResult{Issue: candidate},
		},
	})
}
