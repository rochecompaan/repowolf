package gitea

import (
	"context"
	"testing"
	"time"

	sdk "gitea.dev/sdk"
)

type reviewOnlyAPI struct{ pages map[int][]*sdk.PullReview }

func (f *reviewOnlyAPI) ListPullReviews(_ context.Context, _, _ string, _ int64, opt sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error) {
	return f.pages[opt.Page], nil
}
func (f *reviewOnlyAPI) ListRepoPullRequests(context.Context, string, string, sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, map[int64]pullPresence, error) {
	return nil, nil, nil
}
func (f *reviewOnlyAPI) GetPullRequest(context.Context, string, string, int64) (*sdk.PullRequest, map[int64]pullPresence, error) {
	return nil, nil, nil
}
func TestReviewPaginationPreservesOrderAndActors(t *testing.T) {
	now := time.Now().UTC()
	api := &reviewOnlyAPI{pages: map[int][]*sdk.PullReview{1: {{ID: 1, Reviewer: &sdk.User{ID: 2, UserName: "alice"}, State: sdk.ReviewStateApproved, Submitted: now, HTMLURL: "https://g/review/1"}, {ID: 2, ReviewerTeam: &sdk.Team{ID: 3, Name: "team"}, State: sdk.ReviewStateRequestReview, Submitted: now, HTMLURL: "https://g/review/2"}}}}
	values, err := loadPullReviews(context.Background(), api, "Owner", "Repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].GetActor().GetUser().GetLogin() != "alice" || values[1].GetActor().GetTeam().GetName() != "team" {
		t.Fatalf("unexpected reviews %#v", values)
	}
}
func TestReviewPaginationRejectsDecreasingIDs(t *testing.T) {
	now := time.Now().UTC()
	api := &reviewOnlyAPI{pages: map[int][]*sdk.PullReview{1: {{ID: 2, Reviewer: &sdk.User{ID: 2, UserName: "a"}, State: sdk.ReviewStateComment, Submitted: now, HTMLURL: "u"}, {ID: 1, Reviewer: &sdk.User{ID: 2, UserName: "a"}, State: sdk.ReviewStateComment, Submitted: now, HTMLURL: "u"}}}}
	if _, err := loadPullReviews(context.Background(), api, "o", "r", 1); err == nil {
		t.Fatal("accepted decreasing ids")
	}
}
