package gitea

import (
	sdk "code.gitea.io/sdk/gitea"
	"context"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"testing"
)

func TestIssueViewCommentsAreExplicit(t *testing.T) {
	f := &fakeIssueAPI{issue: sdkIssue(), comments: map[int][]*sdk.Comment{}}
	a, _ := newIssueAdapter(f)
	q := &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueView{IssueView: &repowolfv1.GiteaIssueViewRequest{Index: 7}}}
	r, e := a.Execute(context.Background(), issueResolved(), q)
	if e != nil || f.getCalls != 1 || f.commentCalls != 0 || r.GetIssueView().Issue.Index != 7 {
		t.Fatalf("%#v %v", r, e)
	}
	q.GetIssueView().IncludeComments = true
	if _, e = a.Execute(context.Background(), issueResolved(), q); e != nil || f.commentCalls != 1 {
		t.Fatalf("comments=%d err=%v", f.commentCalls, e)
	}
}
