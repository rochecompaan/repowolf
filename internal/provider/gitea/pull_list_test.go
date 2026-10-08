package gitea

import (
	"context"
	"errors"
	"testing"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type recordingPullAPI struct {
	listValues   []*sdk.PullRequest
	listPresence map[int64]pullPresence
	listErr      error
	getValue     *sdk.PullRequest
	getPresence  map[int64]pullPresence
	getErr       error
	reviewPages  map[int][]*sdk.PullReview
	reviewErrs   map[int]error
	calls        []string
	listOptions  []sdk.ListPullRequestsOptions
}

func (f *recordingPullAPI) ListRepoPullRequests(_ context.Context, owner, repo string, options sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, map[int64]pullPresence, error) {
	f.calls = append(f.calls, "list:"+owner+"/"+repo)
	f.listOptions = append(f.listOptions, options)
	return f.listValues, f.listPresence, f.listErr
}
func (f *recordingPullAPI) GetPullRequest(_ context.Context, owner, repo string, index int64) (*sdk.PullRequest, map[int64]pullPresence, error) {
	f.calls = append(f.calls, "get-pull:"+owner+"/"+repo)
	return f.getValue, f.getPresence, f.getErr
}
func (f *recordingPullAPI) ListPullReviews(_ context.Context, _, _ string, _ int64, options sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error) {
	f.calls = append(f.calls, "reviews")
	return f.reviewPages[options.Page], f.reviewErrs[options.Page]
}

func pullListRequest(state repowolfv1.GiteaPullState, limit int32) *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullList{PullList: &repowolfv1.GiteaPullListRequest{State: state, Page: 2, Limit: limit, Fields: []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE, repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX}}}}
}

func TestPullListMapsExactOptionsOnceAndPreservesProviderOrder(t *testing.T) {
	first, second := validSDKPull(), validSDKPull()
	first.Index, first.Title = 8, "eight"
	second.Index, second.Title = 3, "three"
	api := &recordingPullAPI{listValues: []*sdk.PullRequest{first, second}, listPresence: map[int64]pullPresence{8: {}, 3: {}}}
	adapter := &RepositoryAdapter{api: &fakeIssueAPI{}, pulls: api}
	response, err := adapter.Execute(context.Background(), resolved(), pullListRequest(repowolfv1.GiteaPullState_GITEA_PULL_STATE_ALL, 50))
	if err != nil {
		t.Fatal(err)
	}
	options := api.listOptions
	pulls := response.GetPullList().GetPulls()
	if len(options) != 1 || options[0].Page != 2 || options[0].PageSize != 50 || options[0].State != sdk.StateAll || len(pulls) != 2 || pulls[0].Index != 8 || pulls[1].Index != 3 || pulls[0].Title != "eight" || pulls[0].Author != "" {
		t.Fatalf("options=%#v pulls=%#v calls=%v", options, pulls, api.calls)
	}
}

func TestPullListRejectsMalformedOrUnboundedProviderResultsAtomically(t *testing.T) {
	for _, test := range []struct {
		name     string
		values   []*sdk.PullRequest
		presence map[int64]pullPresence
		err      error
	}{
		{name: "provider error", err: errors.New("secret provider detail")},
		{name: "over limit", values: []*sdk.PullRequest{validSDKPull(), validSDKPull()}, presence: map[int64]pullPresence{1: {}, 2: {}}},
		{name: "presence mismatch", values: []*sdk.PullRequest{validSDKPull()}, presence: map[int64]pullPresence{}},
		{name: "nil record", values: []*sdk.PullRequest{nil}, presence: map[int64]pullPresence{1: {}}},
		{name: "duplicate index", values: []*sdk.PullRequest{validSDKPull(), validSDKPull()}, presence: map[int64]pullPresence{1: {}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "over limit" {
				test.values[1].Index = 2
			}
			api := &recordingPullAPI{listValues: test.values, listPresence: test.presence, listErr: test.err}
			adapter := &RepositoryAdapter{api: &fakeIssueAPI{}, pulls: api}
			response, err := adapter.Execute(context.Background(), resolved(), pullListRequest(repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, 1))
			if response != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) || len(api.listOptions) != 1 {
				t.Fatalf("response=%#v err=%v calls=%v", response, err, api.calls)
			}
		})
	}
}
