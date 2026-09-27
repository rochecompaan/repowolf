package gitea

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "gitea.dev/sdk"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

type reviewOnlyAPI struct {
	pages     map[int][]*sdk.PullReview
	errors    map[int]error
	options   []sdk.ListPullReviewsOptions
	afterPage func(int)
}

func (f *reviewOnlyAPI) ListPullReviews(_ context.Context, _, _ string, _ int64, opt sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error) {
	f.options = append(f.options, opt)
	if f.afterPage != nil {
		f.afterPage(opt.Page)
	}
	return f.pages[opt.Page], f.errors[opt.Page]
}
func (f *reviewOnlyAPI) ListRepoPullRequests(context.Context, string, string, sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, map[int64]pullPresence, error) {
	return nil, nil, nil
}
func (f *reviewOnlyAPI) GetPullRequest(context.Context, string, string, int64) (*sdk.PullRequest, map[int64]pullPresence, error) {
	return nil, nil, nil
}

func sdkReview(id int64) *sdk.PullReview {
	return &sdk.PullReview{ID: id, Reviewer: &sdk.User{ID: 2, UserName: "alice"}, State: sdk.ReviewStateComment, Submitted: time.Unix(id, 0).UTC(), HTMLURL: "https://g/review"}
}

func TestReviewPaginationPreservesOrderActorsAndExplicitOptions(t *testing.T) {
	now := time.Now().UTC()
	api := &reviewOnlyAPI{pages: map[int][]*sdk.PullReview{1: {{ID: 1, Reviewer: &sdk.User{ID: 2, UserName: "alice"}, State: sdk.ReviewStateApproved, Submitted: now, HTMLURL: "https://g/review/1"}, {ID: 2, ReviewerTeam: &sdk.Team{ID: 3, Name: "team"}, State: sdk.ReviewStateRequestReview, Submitted: now, HTMLURL: "https://g/review/2"}}}}
	values, err := loadPullReviews(context.Background(), api, "Owner", "Repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0].GetActor().GetUser().GetLogin() != "alice" || values[1].GetActor().GetTeam().GetName() != "team" || len(api.options) != 1 || api.options[0].Page != 1 || api.options[0].PageSize != reviewPageSize {
		t.Fatalf("reviews=%#v options=%#v", values, api.options)
	}
}

func TestReviewPaginationAcceptsMaximumAfterEmptyProbeAndRejectsOverflow(t *testing.T) {
	pages := make(map[int][]*sdk.PullReview, maximumReviewPages+1)
	for page := 1; page <= maximumReviewPages; page++ {
		values := make([]*sdk.PullReview, reviewPageSize)
		for i := range values {
			values[i] = sdkReview(int64((page-1)*reviewPageSize + i + 1))
		}
		pages[page] = values
	}
	pages[maximumReviewPages+1] = []*sdk.PullReview{}
	api := &reviewOnlyAPI{pages: pages}
	values, err := loadPullReviews(context.Background(), api, "Owner", "Repo", 1)
	if err != nil || len(values) != maximumReviews || len(api.options) != maximumReviewPages+1 || api.options[maximumReviewPages].Page != maximumReviewPages+1 {
		t.Fatalf("len=%d calls=%d err=%v", len(values), len(api.options), err)
	}
	pages[maximumReviewPages+1] = []*sdk.PullReview{sdkReview(maximumReviews + 1)}
	api = &reviewOnlyAPI{pages: pages}
	values, err = loadPullReviews(context.Background(), api, "Owner", "Repo", 1)
	if values != nil || !errors.Is(err, runner.ErrOutputLimit) {
		t.Fatalf("values=%#v err=%v", values, err)
	}
}

func TestReviewPaginationRejectsMalformedPagesAndReturnsNoPartialResult(t *testing.T) {
	oversized := make([]*sdk.PullReview, reviewPageSize+1)
	for i := range oversized {
		oversized[i] = sdkReview(int64(i + 1))
	}
	for _, test := range []struct {
		name   string
		pages  map[int][]*sdk.PullReview
		errors map[int]error
	}{
		{name: "oversized page", pages: map[int][]*sdk.PullReview{1: oversized}},
		{name: "nil review", pages: map[int][]*sdk.PullReview{1: {nil}}},
		{name: "decreasing ids", pages: map[int][]*sdk.PullReview{1: {sdkReview(2), sdkReview(1)}}},
		{name: "provider failure", pages: map[int][]*sdk.PullReview{1: {sdkReview(1)}}, errors: map[int]error{1: errors.New("provider secret")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &reviewOnlyAPI{pages: test.pages, errors: test.errors}
			values, err := loadPullReviews(context.Background(), api, "o", "r", 1)
			if values != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) {
				t.Fatalf("values=%#v err=%v", values, err)
			}
		})
	}
}

func TestReviewPaginationHonorsCancellationBeforeAndBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := &reviewOnlyAPI{}
	values, err := loadPullReviews(ctx, api, "o", "r", 1)
	if values != nil || !errors.Is(err, context.Canceled) || len(api.options) != 0 {
		t.Fatalf("values=%#v calls=%d err=%v", values, len(api.options), err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	full := make([]*sdk.PullReview, reviewPageSize)
	for i := range full {
		full[i] = sdkReview(int64(i + 1))
	}
	api = &reviewOnlyAPI{pages: map[int][]*sdk.PullReview{1: full}, afterPage: func(page int) {
		if page == 1 {
			cancel()
		}
	}}
	values, err = loadPullReviews(ctx, api, "o", "r", 1)
	if values != nil || !errors.Is(err, context.Canceled) || len(api.options) != 1 {
		t.Fatalf("values=%#v calls=%d err=%v", values, len(api.options), err)
	}
}
