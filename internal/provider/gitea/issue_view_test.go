package gitea

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func TestIssueViewMapsCompleteIssueAndCommentsAreExplicit(t *testing.T) {
	deadline := time.Unix(10, 0).UTC()
	issue := sdkIssue()
	issue.Body = "body"
	issue.Deadline = &deadline
	issue.Assignees = []*sdk.User{{ID: 3, UserName: "bob"}}
	issue.Milestone = &sdk.Milestone{Title: "v1"}
	issue.Labels = []*sdk.Label{{ID: 4, Name: "bug"}}
	issue.Comments = 1
	f := &fakeIssueAPI{issue: issue, comments: map[int][]*sdk.Comment{1: {sdkComment(1)}}}
	adapter, _ := newIssueAdapter(f)
	request := issueViewRequest(false)

	response, err := adapter.Execute(context.Background(), issueResolved(), request)
	if err != nil {
		t.Fatal(err)
	}
	record := response.GetIssueView().GetIssue()
	if f.getCalls != 1 || f.getOwner != "Owner" || f.getRepo != "Repo" || f.getIndex != 7 || f.commentCalls != 0 {
		t.Fatalf("provider calls = get %d %s/%s#%d, comments %d", f.getCalls, f.getOwner, f.getRepo, f.getIndex, f.commentCalls)
	}
	if record.Index != 7 || record.Author != "alice" || record.AuthorId != 2 || record.Title != "title" || record.Body != "body" || record.Owner != "Owner" || record.Repo != "Repo" || record.Deadline == nil || !record.Deadline.AsTime().Equal(deadline) || record.Milestone == nil || record.GetMilestone() != "v1" || len(record.Assignees) != 1 || record.Assignees[0] != "bob" || len(record.Labels) != 1 || record.Labels[0] != "bug" || record.CommentCount != 1 || record.Comments != nil {
		t.Fatalf("incomplete issue projection: %#v", record)
	}

	request.GetIssueView().IncludeComments = true
	response, err = adapter.Execute(context.Background(), issueResolved(), request)
	if err != nil || f.getCalls != 2 || f.commentCalls != 1 || len(response.GetIssueView().GetIssue().Comments) != 1 {
		t.Fatalf("comment view = %#v, err %v; get=%d comments=%d", response, err, f.getCalls, f.commentCalls)
	}
}

func TestIssueViewFailsClosedWithoutRetryOrPartialResponse(t *testing.T) {
	secret := errors.New("provider secret")
	for _, test := range []struct {
		name        string
		configure   func(*fakeIssueAPI)
		want        error
		wantComment int
	}{
		{name: "not found", configure: func(fake *fakeIssueAPI) { fake.getStatus, fake.getError = 404, secret }, want: rpcstatus.ErrNotFound},
		{name: "provider error", configure: func(fake *fakeIssueAPI) { fake.getError = secret }, want: rpcstatus.ErrProviderFailure},
		{name: "cancellation", configure: func(fake *fakeIssueAPI) { fake.getError = context.Canceled }, want: context.Canceled},
		{name: "deadline", configure: func(fake *fakeIssueAPI) { fake.getError = context.DeadlineExceeded }, want: context.DeadlineExceeded},
		{name: "nil issue", configure: func(fake *fakeIssueAPI) { fake.issue = nil }, want: rpcstatus.ErrProviderFailure},
		{name: "mismatched index", configure: func(fake *fakeIssueAPI) { fake.issue.Index = 8 }, want: rpcstatus.ErrProviderFailure},
		{name: "pull request kind", configure: func(fake *fakeIssueAPI) { fake.issue.PullRequest = &sdk.PullRequestMeta{} }, want: rpcstatus.ErrIssueKind},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeIssueAPI{issue: sdkIssue(), comments: map[int][]*sdk.Comment{1: {sdkComment(1)}}}
			test.configure(fake)
			adapter, _ := newIssueAdapter(fake)

			response, err := adapter.Execute(context.Background(), issueResolved(), issueViewRequest(true))

			if response != nil || !errors.Is(err, test.want) || fake.getCalls != 1 || fake.commentCalls != test.wantComment {
				t.Fatalf("Execute() = %#v, %v; calls get=%d comments=%d; want error %v", response, err, fake.getCalls, fake.commentCalls, test.want)
			}
			if test.name == "provider error" && err.Error() == secret.Error() {
				t.Fatal("provider error text leaked")
			}
		})
	}
}

func issueViewRequest(includeComments bool) *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueView{IssueView: &repowolfv1.GiteaIssueViewRequest{Index: 7, IncludeComments: includeComments}}}
}
