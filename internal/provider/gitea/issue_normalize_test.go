package gitea

import (
	"testing"
	"time"

	sdk "code.gitea.io/sdk/gitea"
)

func TestNormalizeIssueRejectsProtobufInvalidTimestamps(t *testing.T) {
	beforeProtobufRange := time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)
	afterProtobufRange := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		mutate func(*sdk.Issue)
	}{
		{name: "created", mutate: func(issue *sdk.Issue) { issue.Created = beforeProtobufRange }},
		{name: "updated", mutate: func(issue *sdk.Issue) { issue.Updated = afterProtobufRange }},
		{name: "deadline", mutate: func(issue *sdk.Issue) { issue.Deadline = &afterProtobufRange }},
	} {
		t.Run(test.name, func(t *testing.T) {
			issue := sdkIssue()
			test.mutate(issue)
			if normalized, err := normalizeIssue(issue, "Owner", "Repo"); err == nil || normalized != nil {
				t.Fatalf("normalizeIssue() = %#v, %v, want rejection", normalized, err)
			}
		})
	}
}

func TestNormalizeCommentRejectsProtobufInvalidTimestamps(t *testing.T) {
	beforeProtobufRange := time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC)
	afterProtobufRange := time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		mutate func(*sdk.Comment)
	}{
		{name: "created", mutate: func(comment *sdk.Comment) { comment.Created = beforeProtobufRange }},
		{name: "updated", mutate: func(comment *sdk.Comment) { comment.Updated = afterProtobufRange }},
	} {
		t.Run(test.name, func(t *testing.T) {
			comment := &sdk.Comment{
				ID:      1,
				Poster:  &sdk.User{ID: 2, UserName: "alice"},
				HTMLURL: "https://g/Owner/Repo/issues/7#issuecomment-1",
				Created: time.Unix(1, 0),
				Updated: time.Unix(2, 0),
			}
			test.mutate(comment)
			if normalized, err := normalizeComment(comment); err == nil || normalized != nil {
				t.Fatalf("normalizeComment() = %#v, %v, want rejection", normalized, err)
			}
		})
	}
}
