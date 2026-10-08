package gitea

import (
	"context"
	"errors"
	"reflect"
	"testing"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type orderedIssueAPI struct {
	*fakeIssueAPI
	calls *[]string
}

func (f *orderedIssueAPI) GetIssue(ctx context.Context, owner, repo string, index int64) (*sdk.Issue, int, error) {
	*f.calls = append(*f.calls, "issue")
	return f.fakeIssueAPI.GetIssue(ctx, owner, repo, index)
}
func (f *orderedIssueAPI) ListIssueTimeline(ctx context.Context, owner, repo string, index int64, options sdk.ListIssueCommentOptions) (issueCommentPage, error) {
	*f.calls = append(*f.calls, "comments")
	return f.fakeIssueAPI.ListIssueTimeline(ctx, owner, repo, index, options)
}

type orderedPullAPI struct {
	*recordingPullAPI
	callsRef *[]string
}

func (f *orderedPullAPI) GetPullRequest(ctx context.Context, owner, repo string, index int64) (*sdk.PullRequest, map[int64]pullPresence, error) {
	*f.callsRef = append(*f.callsRef, "pull")
	return f.recordingPullAPI.GetPullRequest(ctx, owner, repo, index)
}
func (f *orderedPullAPI) ListPullReviews(ctx context.Context, owner, repo string, index int64, options sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error) {
	*f.callsRef = append(*f.callsRef, "reviews")
	return f.recordingPullAPI.ListPullReviews(ctx, owner, repo, index, options)
}

func pullViewRequest(comments bool) *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullView{PullView: &repowolfv1.GiteaPullViewRequest{Index: 1, IncludeComments: comments}}}
}

func TestPullViewUsesKindFirstHydrationOrder(t *testing.T) {
	for _, includeComments := range []bool{false, true} {
		t.Run(map[bool]string{false: "without comments", true: "with comments"}[includeComments], func(t *testing.T) {
			calls := []string{}
			issueAPI := &orderedIssueAPI{fakeIssueAPI: &fakeIssueAPI{issue: &sdk.Issue{Index: 1, PullRequest: &sdk.PullRequestMeta{}}, comments: map[int][]*sdk.Comment{1: {}}}, calls: &calls}
			pull := validSDKPull()
			pull.Comments = 0
			pullAPI := &orderedPullAPI{recordingPullAPI: &recordingPullAPI{getValue: pull, getPresence: map[int64]pullPresence{1: {}}, reviewPages: map[int][]*sdk.PullReview{1: {}}}, callsRef: &calls}
			adapter := &RepositoryAdapter{api: issueAPI, pulls: pullAPI}
			response, err := adapter.Execute(context.Background(), resolved(), pullViewRequest(includeComments))
			if err != nil || response.GetPullView().GetPull() == nil {
				t.Fatalf("response=%#v err=%v", response, err)
			}
			want := []string{"issue", "pull", "reviews"}
			if includeComments {
				want = append(want, "comments")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls=%v want=%v", calls, want)
			}
		})
	}
}

func TestPullViewRejectsMismatchedIssueIndexBeforeKindHandling(t *testing.T) {
	for _, pullMarker := range []*sdk.PullRequestMeta{nil, {}} {
		calls := []string{}
		issueAPI := &orderedIssueAPI{fakeIssueAPI: &fakeIssueAPI{issue: &sdk.Issue{Index: 2, PullRequest: pullMarker}}, calls: &calls}
		pullAPI := &orderedPullAPI{recordingPullAPI: &recordingPullAPI{}, callsRef: &calls}
		adapter := &RepositoryAdapter{api: issueAPI, pulls: pullAPI}
		response, err := adapter.Execute(context.Background(), resolved(), pullViewRequest(true))
		if response != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) || !reflect.DeepEqual(calls, []string{"issue"}) {
			t.Fatalf("marker=%#v response=%#v err=%v calls=%v", pullMarker, response, err, calls)
		}
	}
}

func TestPullViewReturnsKindCorrectionBeforePullHydration(t *testing.T) {
	calls := []string{}
	issueAPI := &orderedIssueAPI{fakeIssueAPI: &fakeIssueAPI{issue: &sdk.Issue{Index: 1}}, calls: &calls}
	pullAPI := &orderedPullAPI{recordingPullAPI: &recordingPullAPI{}, callsRef: &calls}
	adapter := &RepositoryAdapter{api: issueAPI, pulls: pullAPI}
	response, err := adapter.Execute(context.Background(), resolved(), pullViewRequest(true))
	if response != nil || !errors.Is(err, rpcstatus.ErrPullKind) || !reflect.DeepEqual(calls, []string{"issue"}) {
		t.Fatalf("response=%#v err=%v calls=%v", response, err, calls)
	}
}

func TestPullViewReturnsNoPartialResultWhenDependentHydrationFails(t *testing.T) {
	for _, test := range []struct {
		name      string
		pullError error
		reviewErr error
	}{
		{name: "pull", pullError: errors.New("pull secret")},
		{name: "review", reviewErr: errors.New("review secret")},
	} {
		t.Run(test.name, func(t *testing.T) {
			issueAPI := &fakeIssueAPI{issue: &sdk.Issue{Index: 1, PullRequest: &sdk.PullRequestMeta{}}}
			pullAPI := &recordingPullAPI{getValue: validSDKPull(), getPresence: map[int64]pullPresence{1: {}}, getErr: test.pullError, reviewPages: map[int][]*sdk.PullReview{}, reviewErrs: map[int]error{1: test.reviewErr}}
			adapter := &RepositoryAdapter{api: issueAPI, pulls: pullAPI}
			response, err := adapter.Execute(context.Background(), resolved(), pullViewRequest(false))
			if response != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) {
				t.Fatalf("response=%#v err=%v", response, err)
			}
		})
	}
}
