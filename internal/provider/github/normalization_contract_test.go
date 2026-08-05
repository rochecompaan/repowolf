package github

import (
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const largeProviderID uint64 = 9_007_199_254_740_993

func TestNormalizationPreservesLargeUint64AndOperationAllowlists(t *testing.T) {
	issue := `{"number":9007199254740993,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","extra":"discard"}`
	pull := `{"number":9007199254740993,"title":"title","body":"body","state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean","extra":"discard"}`
	run := `{"id":9007199254740993,"name":"CI","display_title":"run","status":"completed","conclusion":"success","event":"push","head_branch":"main","head_sha":"0123456789012345678901234567890123456789","html_url":"https://safe.example/r","created_at":"c","updated_at":"u","run_attempt":2,"jobs_url":"https://safe.example/jobs","extra":"discard"}`

	issueList, err := normalize(request(&repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{}}), "issue_list", []byte(`{"items":[`+issue+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	listedIssue := issueList.GetIssueList().Issues[0]
	if listedIssue.Number != largeProviderID || listedIssue.Body != nil || len(listedIssue.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("listed issue = %#v", listedIssue)
	}
	issueView, err := normalize(request(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}}), "issue", []byte(issue))
	if err != nil {
		t.Fatal(err)
	}
	if got := issueView.GetIssueView().Issue; got.Number != largeProviderID || got.GetBody() != "body" || len(got.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("view issue = %#v", got)
	}

	commentResponse, err := normalize(request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: "body"}}), "comment", []byte(`{"id":9007199254740993,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u","extra":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := commentResponse.GetIssueComment().Comment; got.Id != largeProviderID || len(got.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("comment = %#v", got)
	}

	pullList, err := normalize(request(&repowolfv1.GitHubRequest_PullList{PullList: &repowolfv1.GitHubPullListRequest{}}), "pull_list", []byte(`[`+pull+`]`))
	if err != nil {
		t.Fatal(err)
	}
	listedPull := pullList.GetPullList().Pulls[0]
	if listedPull.Number != largeProviderID || listedPull.MergeableState != nil || len(listedPull.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("listed pull = %#v", listedPull)
	}
	pullView, err := normalize(request(&repowolfv1.GitHubRequest_PullView{PullView: &repowolfv1.GitHubPullViewRequest{Number: 1}}), "pull", []byte(pull))
	if err != nil {
		t.Fatal(err)
	}
	if got := pullView.GetPullView().Pull; got.Number != largeProviderID || got.GetMergeableState() != "clean" || len(got.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("view pull = %#v", got)
	}

	runList, err := normalize(request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{}}), "run_list", []byte(`{"workflow_runs":[`+run+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	listedRun := runList.GetRunList().Runs[0]
	if listedRun.Id != largeProviderID || listedRun.Name != "run" || listedRun.WorkflowName != "CI" || listedRun.Attempt != nil || listedRun.JobsUrl != nil || len(listedRun.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("listed run = %#v", listedRun)
	}
	runView, err := normalize(request(&repowolfv1.GitHubRequest_RunView{RunView: &repowolfv1.GitHubRunViewRequest{RunId: 1}}), "run", []byte(run))
	if err != nil {
		t.Fatal(err)
	}
	if got := runView.GetRunView().Run; got.Id != largeProviderID || got.GetAttempt() != 2 || got.GetJobsUrl() != "https://safe.example/jobs" || len(got.ProtoReflect().GetUnknown()) != 0 {
		t.Fatalf("view run = %#v", got)
	}
}

func TestNormalizationRejectsTrailingDocumentsAndWrongTypes(t *testing.T) {
	issue := `{"number":1,"title":"title","body":null,"state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`
	pull := `{"number":1,"title":"title","body":null,"state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean"}`
	run := `{"id":1,"name":"CI","display_title":"run","status":"completed","conclusion":null,"event":"push","head_branch":"main","head_sha":"0123456789012345678901234567890123456789","html_url":"https://safe.example/r","created_at":"c","updated_at":"u","run_attempt":1,"jobs_url":"https://safe.example/jobs"}`
	tests := []struct {
		name  string
		req   *repowolfv1.GitHubRequest
		kind  string
		valid string
		wrong string
	}{
		{"repository", request(&repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}), "repository", `{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","description":null,"private":false,"html_url":"https://safe.example/repo","default_branch":"main"}`, `{"name":5}`},
		{"issue list", request(&repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{}}), "issue_list", `{"items":[` + issue + `]}`, `[]`},
		{"issue", request(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}}), "issue", issue, strings.Replace(issue, `"number":1`, `"number":"1"`, 1)},
		{"comment", request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: "body"}}), "comment", `{"id":1,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u"}`, `{"id":"1"}`},
		{"pull list", request(&repowolfv1.GitHubRequest_PullList{PullList: &repowolfv1.GitHubPullListRequest{}}), "pull_list", `[` + pull + `]`, `{}`},
		{"pull", request(&repowolfv1.GitHubRequest_PullView{PullView: &repowolfv1.GitHubPullViewRequest{Number: 1}}), "pull", pull, strings.Replace(pull, `"draft":false`, `"draft":"false"`, 1)},
		{"run list", request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{}}), "run_list", `{"workflow_runs":[` + run + `]}`, `[]`},
		{"run", request(&repowolfv1.GitHubRequest_RunView{RunView: &repowolfv1.GitHubRunViewRequest{RunId: 1}}), "run", run, strings.Replace(run, `"id":1`, `"id":"1"`, 1)},
		{"status", request(&repowolfv1.GitHubRequest_StatusView{StatusView: &repowolfv1.GitHubStatusViewRequest{ObjectId: "0123456789012345678901234567890123456789"}}), "status", `{"state":"success","sha":"0123456789012345678901234567890123456789","statuses":[]}`, `{"state":5}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if response, err := normalize(test.req, test.kind, []byte(test.valid+` {}`)); err == nil || response != nil {
				t.Fatalf("trailing document normalize() = %#v, %v", response, err)
			}
			if response, err := normalize(test.req, test.kind, []byte(test.wrong)); err == nil || response != nil {
				t.Fatalf("wrong type normalize() = %#v, %v", response, err)
			}
		})
	}
}
