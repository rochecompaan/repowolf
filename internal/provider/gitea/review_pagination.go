package gitea

import (
	"context"
	"fmt"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	reviewPageSize     = 50
	maximumReviewPages = 20
	maximumReviews     = reviewPageSize * maximumReviewPages
)

func loadPullReviews(ctx context.Context, api pullAPI, owner, repo string, index int64) ([]*repowolfv1.GiteaPullReviewRecord, error) {
	reviews := make([]*repowolfv1.GiteaPullReviewRecord, 0)
	var last int64
	for page := 1; page <= maximumReviewPages+1; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values, err := api.ListPullReviews(ctx, owner, repo, index, sdk.ListPullReviewsOptions{ListOptions: sdk.ListOptions{Page: page, PageSize: reviewPageSize}})
		if err != nil {
			return nil, classifyProviderError(ctx, err)
		}
		if page == maximumReviewPages+1 {
			if len(values) != 0 {
				return nil, runner.ErrOutputLimit
			}
			return reviews, nil
		}
		if len(values) > reviewPageSize {
			return nil, rpcstatus.ErrProviderFailure
		}
		for _, value := range values {
			review, err := normalizePullReview(value)
			if err != nil || review.Id <= last {
				return nil, rpcstatus.ErrProviderFailure
			}
			reviews = append(reviews, review)
			last = review.Id
		}
		if len(values) < reviewPageSize {
			return reviews, nil
		}
	}
	return nil, runner.ErrOutputLimit
}

func normalizePullReview(value *sdk.PullReview) (*repowolfv1.GiteaPullReviewRecord, error) {
	if value == nil || value.ID <= 0 || value.CodeCommentsCount < 0 || value.HTMLURL == "" || !validProtoTime(value.Submitted) {
		return nil, fmt.Errorf("invalid review")
	}
	for _, text := range []string{value.Body, value.CommitID, value.HTMLURL} {
		if !validProviderString(text) {
			return nil, fmt.Errorf("invalid review string")
		}
	}
	actor, err := normalizeReviewActor(value.Reviewer, value.ReviewerTeam)
	if err != nil {
		return nil, err
	}
	states := map[sdk.ReviewStateType]repowolfv1.GiteaPullReviewState{sdk.ReviewStateApproved: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_APPROVED, sdk.ReviewStatePending: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_PENDING, sdk.ReviewStateComment: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_COMMENT, sdk.ReviewStateRequestChanges: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_REQUEST_CHANGES, sdk.ReviewStateRequestReview: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_REQUEST_REVIEW}
	state, ok := states[value.State]
	if !ok {
		return nil, fmt.Errorf("invalid review state")
	}
	return &repowolfv1.GiteaPullReviewRecord{Id: value.ID, Actor: actor, State: state, Body: value.Body, CommitId: value.CommitID, Stale: value.Stale, Official: value.Official, Dismissed: value.Dismissed, CodeCommentCount: int64(value.CodeCommentsCount), Submitted: timestamppb.New(value.Submitted), Url: value.HTMLURL}, nil
}
