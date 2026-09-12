package gitea

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func sdkComment(id int64) *sdk.Comment {
	return &sdk.Comment{ID: id, Poster: &sdk.User{ID: 1, UserName: "a"}, HTMLURL: "https://g/c", Created: time.Unix(1, 0), Updated: time.Unix(2, 0)}
}

func commentIssue(count int64) *repowolfv1.GiteaIssueRecord {
	return &repowolfv1.GiteaIssueRecord{CommentCount: count}
}

func TestCommentPaginationUsesExplicitPages(t *testing.T) {
	page := make([]*sdk.Comment, 50)
	for i := range page {
		page[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: page, 2: {sdkComment(51)}}}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(51))
	if err != nil || len(got) != 51 || f.commentCalls != 2 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), f.commentCalls, err)
	}
	for i, option := range f.commentOptions {
		if option.Page != i+1 || option.PageSize != commentPageSize {
			t.Fatalf("comment option %d = %#v", i, option)
		}
	}
}

func TestCommentPaginationContinuesPastFullTimelinePageWithEvents(t *testing.T) {
	first := make([]*sdk.Comment, commentPageSize-1)
	for i := range first {
		first[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{
		comments:           map[int][]*sdk.Comment{1: first, 2: {sdkComment(commentPageSize)}},
		commentEntryCounts: map[int]int{1: commentPageSize, 2: 1},
	}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(commentPageSize))
	if err != nil || len(got) != commentPageSize || f.commentCalls != 2 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), f.commentCalls, err)
	}
}

func TestCommentPaginationRejectsProviderPageLongerThanRequested(t *testing.T) {
	page := make([]*sdk.Comment, commentPageSize+1)
	for i := range page {
		page[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: page}}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(int64(len(page))))
	if got != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) || f.commentCalls != 1 {
		t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
	}
}

func TestCommentPaginationReturnsNoPartialResultOnProviderFailure(t *testing.T) {
	first := make([]*sdk.Comment, commentPageSize)
	for i := range first {
		first[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{
		comments:      map[int][]*sdk.Comment{1: first},
		commentErrors: map[int]error{2: errors.New("provider details")},
	}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(commentPageSize+1))
	if got != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) || f.commentCalls != 2 {
		t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
	}
}

func TestCommentPaginationHonorsCancellationBeforeAndBetweenPages(t *testing.T) {
	t.Run("before page one", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		f := &fakeIssueAPI{}
		got, err := loadIssueComments(ctx, f, "o", "r", 7, commentIssue(0))
		if got != nil || !errors.Is(err, context.Canceled) || f.commentCalls != 0 {
			t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
		}
	})
	t.Run("between pages", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		first := make([]*sdk.Comment, commentPageSize)
		for i := range first {
			first[i] = sdkComment(int64(i + 1))
		}
		f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: first}, afterCommentPage: func(page int) {
			if page == 1 {
				cancel()
			}
		}}
		got, err := loadIssueComments(ctx, f, "o", "r", 7, commentIssue(commentPageSize+1))
		if got != nil || !errors.Is(err, context.Canceled) || f.commentCalls != 1 {
			t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
		}
	})
}

func TestCommentPaginationRequiresIssueCommentCountOnEveryCompletion(t *testing.T) {
	for _, test := range []struct {
		name     string
		comments map[int][]*sdk.Comment
		count    int64
	}{
		{name: "empty page", comments: map[int][]*sdk.Comment{1: {}}, count: 1},
		{name: "short page", comments: map[int][]*sdk.Comment{1: {sdkComment(1)}}, count: 2},
		{name: "short response with stale count", comments: map[int][]*sdk.Comment{1: {sdkComment(1), sdkComment(2)}}, count: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &fakeIssueAPI{comments: test.comments}
			got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(test.count))
			if got != nil || err == nil {
				t.Fatalf("comments=%#v err=%v, want no partial result", got, err)
			}
		})
	}
}

func TestCommentPaginationAcceptsMaximumAfterEmptyOverflowProbe(t *testing.T) {
	pages := make(map[int][]*sdk.Comment, maximumCommentPages+1)
	for page := 1; page <= maximumCommentPages; page++ {
		values := make([]*sdk.Comment, commentPageSize)
		for i := range values {
			values[i] = sdkComment(int64((page-1)*commentPageSize + i + 1))
		}
		pages[page] = values
	}
	pages[maximumCommentPages+1] = []*sdk.Comment{}
	f := &fakeIssueAPI{comments: pages}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(maximumComments))
	if err != nil || len(got) != maximumComments || f.commentCalls != maximumCommentPages+1 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), f.commentCalls, err)
	}
}

func TestCommentPaginationClassifiesEveryNonEmptyOverflowProbeAsOutputLimit(t *testing.T) {
	for _, test := range []struct {
		name      string
		probeSize int
	}{
		{name: "single record", probeSize: 1},
		{name: "oversized page", probeSize: commentPageSize + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			probeSize := test.probeSize
			pages := make(map[int][]*sdk.Comment, maximumCommentPages+1)
			for page := 1; page <= maximumCommentPages; page++ {
				values := make([]*sdk.Comment, commentPageSize)
				for i := range values {
					values[i] = sdkComment(int64((page-1)*commentPageSize + i + 1))
				}
				pages[page] = values
			}
			probe := make([]*sdk.Comment, probeSize)
			for i := range probe {
				probe[i] = sdkComment(int64(maximumComments + i + 1))
			}
			pages[maximumCommentPages+1] = probe
			f := &fakeIssueAPI{comments: pages}
			got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(maximumComments))
			if got != nil || !errors.Is(err, runner.ErrOutputLimit) || f.commentCalls != maximumCommentPages+1 {
				t.Fatalf("probe size=%d comments=%#v calls=%d err=%v", probeSize, got, f.commentCalls, err)
			}
		})
	}
}

func TestCommentPaginationRejectsMalformedOrOutOfOrderComments(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []*sdk.Comment
	}{
		{name: "nil", values: []*sdk.Comment{nil}},
		{name: "decreasing IDs", values: []*sdk.Comment{sdkComment(2), sdkComment(1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: test.values}}
			if got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(int64(len(test.values)))); !errors.Is(err, rpcstatus.ErrProviderFailure) || got != nil {
				t.Fatalf("comments=%#v err=%v", got, err)
			}
		})
	}
}
