package gitea

import (
	sdk "code.gitea.io/sdk/gitea"
	"context"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"testing"
	"time"
)

type fakeIssueAPI struct {
	issues                            []*sdk.Issue
	issue                             *sdk.Issue
	comments                          map[int][]*sdk.Comment
	commentEntryCounts                map[int]int
	commentErrors                     map[int]error
	afterCommentPage                  func(int)
	listCalls, getCalls, commentCalls int
	commentOptions                    []sdk.ListIssueCommentOptions
	option                            sdk.ListIssueOption
}

func (f *fakeIssueAPI) GetRepo(context.Context, string, string) (*sdk.Repository, error) {
	return nil, nil
}
func (f *fakeIssueAPI) ListRepoIssues(_ context.Context, _, _ string, o sdk.ListIssueOption) ([]*sdk.Issue, error) {
	f.listCalls++
	f.option = o
	return f.issues, nil
}
func (f *fakeIssueAPI) GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error) {
	f.getCalls++
	return f.issue, 200, nil
}
func (f *fakeIssueAPI) ListIssueTimeline(_ context.Context, _, _ string, _ int64, o sdk.ListIssueCommentOptions) (issueCommentPage, error) {
	f.commentCalls++
	f.commentOptions = append(f.commentOptions, o)
	values := f.comments[o.Page]
	entryCount := len(values)
	if count, ok := f.commentEntryCounts[o.Page]; ok {
		entryCount = count
	}
	if f.afterCommentPage != nil {
		f.afterCommentPage(o.Page)
	}
	return issueCommentPage{entryCount: entryCount, comments: values}, f.commentErrors[o.Page]
}
func sdkIssue() *sdk.Issue {
	return &sdk.Issue{Index: 7, Poster: &sdk.User{ID: 2, UserName: "alice"}, HTMLURL: "https://g/o/r/issues/7", Title: "title", State: sdk.StateOpen, Created: time.Unix(1, 0), Updated: time.Unix(2, 0), Repository: &sdk.RepositoryMeta{Owner: "Owner", Name: "Repo", FullName: "Owner/Repo"}}
}
func issueResolved() policy.ResolvedRepository {
	return policy.ResolvedRepository{Repository: config.Repository{Owner: "Owner", Name: "Repo"}}
}
func listRequest() *repowolfv1.GiteaRequest {
	return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueList{IssueList: &repowolfv1.GiteaIssueListRequest{State: 1, Page: 2, Limit: 50, Fields: []repowolfv1.GiteaIssueField{1, 6}}}}
}
func TestIssueListMapsOptionsAndProjects(t *testing.T) {
	f := &fakeIssueAPI{issues: []*sdk.Issue{sdkIssue()}}
	a, _ := newIssueAdapter(f)
	r, e := a.Execute(context.Background(), issueResolved(), listRequest())
	if e != nil {
		t.Fatal(e)
	}
	if f.listCalls != 1 || f.option.Type != sdk.IssueTypeIssue || f.option.Page != 2 || len(r.GetIssueList().Issues) != 1 || r.GetIssueList().Issues[0].Title != "title" {
		t.Fatalf("%#v %#v", f.option, r)
	}
}
func TestIssueListOwnerMismatchSkipsProvider(t *testing.T) {
	f := &fakeIssueAPI{}
	a, _ := newIssueAdapter(f)
	q := listRequest()
	x := "other"
	q.GetIssueList().Owner = &x
	r, e := a.Execute(context.Background(), issueResolved(), q)
	if e != nil || f.listCalls != 0 || len(r.GetIssueList().Issues) != 0 {
		t.Fatalf("%#v %v", r, e)
	}
}
