package gitea

import (
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func issueFixture() *repowolfv1.GiteaIssueRecord {
	m := "v1"
	return &repowolfv1.GiteaIssueRecord{Index: 7, State: 1, Kind: 1, AuthorId: 2, Author: "alice", Url: "https://g/o/r/issues/7", Title: "A title", Body: "body", Repo: "r", Owner: "o", Created: timestamppb.New(time.Unix(1, 0)), Updated: timestamppb.New(time.Unix(2, 0)), Milestone: &m, Labels: []string{"bug"}, CommentCount: 2}
}
func TestRenderIssueListFormats(t *testing.T) {
	fields := []repowolfv1.GiteaIssueField{1, 6, 2, 14}
	request := &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueList{IssueList: &repowolfv1.GiteaIssueListRequest{Limit: 30, Fields: fields}}}
	response := &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: "r"}, Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: []*repowolfv1.GiteaIssueRecord{issueFixture()}}}}
	for _, tc := range []struct {
		format outputFormat
		want   string
	}{{outputTable, "index\ttitle\tstate\tcomments\n7\tA title\topen\t2\n"}, {outputSimple, "7 A title open 2\n"}, {outputJSON, "[{\"index\":7,\"title\":\"A title\",\"state\":\"open\",\"comments\":2}]\n"}} {
		got, err := render(command{request: request, format: tc.format, fields: fields}, response)
		if err != nil || string(got) != tc.want {
			t.Fatalf("got %q %v", got, err)
		}
	}
}
func TestRenderIssueViewOmitsComments(t *testing.T) {
	request := &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueView{IssueView: &repowolfv1.GiteaIssueViewRequest{Index: 7}}}
	response := &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: "r"}, Result: &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: issueFixture()}}}
	if _, err := render(command{request: request, format: outputJSON}, response); err != nil {
		t.Fatal(err)
	}
}

func TestRenderIssueViewPreservesSimpleCommentBody(t *testing.T) {
	issue := issueFixture()
	issue.CommentCount = 1
	issue.Comments = []*repowolfv1.GiteaCommentRecord{{
		Id: 1, AuthorId: 2, Author: "alice", Url: "https://g/o/r/issues/7#issuecomment-1",
		Body: "first line\nsecond\tline", Created: timestamppb.New(time.Unix(3, 0)), Updated: timestamppb.New(time.Unix(4, 0)),
	}}
	request := &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueView{IssueView: &repowolfv1.GiteaIssueViewRequest{Index: 7, IncludeComments: true}}}
	response := &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: "r"}, Result: &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: issue}}}
	got, err := render(command{request: request, format: outputSimple}, response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "  body: first line\nsecond\tline\n") {
		t.Fatalf("comment body was not preserved: %q", got)
	}
}
