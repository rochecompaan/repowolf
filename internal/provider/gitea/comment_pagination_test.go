package gitea

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
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
}

func TestCommentPaginationAcceptsGiteaUnpaginatedResponse(t *testing.T) {
	all := make([]*sdk.Comment, 51)
	for i := range all {
		all[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: all}}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(51))
	if err != nil || len(got) != 51 || f.commentCalls != 1 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), f.commentCalls, err)
	}
}

func TestCommentPaginationRejectsOversizedUnpaginatedResponse(t *testing.T) {
	all := make([]*sdk.Comment, maximumComments+1)
	for i := range all {
		all[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: all}}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(int64(len(all))))
	if got != nil || !errors.Is(err, runner.ErrOutputLimit) || f.commentCalls != 1 {
		t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
	}
}

func TestCommentPaginationEnforcesAggregateResponseBudget(t *testing.T) {
	first := make([]*sdk.Comment, commentPageSize)
	second := make([]*sdk.Comment, 40)
	body := strings.Repeat("x", 100<<10)
	for i := range first {
		first[i] = sdkComment(int64(i + 1))
		first[i].Body = body
	}
	for i := range second {
		second[i] = sdkComment(int64(commentPageSize + i + 1))
		second[i].Body = body
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: first, 2: second}}
	got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(90))
	if got != nil || !errors.Is(err, runner.ErrOutputLimit) || f.commentCalls != 2 {
		t.Fatalf("comments=%#v calls=%d err=%v", got, f.commentCalls, err)
	}
}

func TestCommentPaginationRejectsOrder(t *testing.T) {
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: {sdkComment(2), sdkComment(1)}}}
	if got, err := loadIssueComments(context.Background(), f, "o", "r", 7, commentIssue(2)); err == nil || got != nil {
		t.Fatalf("%#v %v", got, err)
	}
}
