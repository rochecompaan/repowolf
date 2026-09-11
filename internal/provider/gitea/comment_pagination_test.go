package gitea

import (
	sdk "code.gitea.io/sdk/gitea"
	"context"
	"testing"
	"time"
)

func sdkComment(id int64) *sdk.Comment {
	return &sdk.Comment{ID: id, Poster: &sdk.User{ID: 1, UserName: "a"}, HTMLURL: "https://g/c", Created: time.Unix(1, 0), Updated: time.Unix(2, 0)}
}
func TestCommentPaginationUsesExplicitPages(t *testing.T) {
	page := make([]*sdk.Comment, 50)
	for i := range page {
		page[i] = sdkComment(int64(i + 1))
	}
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: page, 2: {sdkComment(51)}}}
	got, e := loadIssueComments(context.Background(), f, "o", "r", 7)
	if e != nil || len(got) != 51 || f.commentCalls != 2 {
		t.Fatalf("len=%d calls=%d err=%v", len(got), f.commentCalls, e)
	}
}
func TestCommentPaginationRejectsOrder(t *testing.T) {
	f := &fakeIssueAPI{comments: map[int][]*sdk.Comment{1: {sdkComment(2), sdkComment(1)}}}
	if got, e := loadIssueComments(context.Background(), f, "o", "r", 7); e == nil || got != nil {
		t.Fatalf("%#v %v", got, e)
	}
}
