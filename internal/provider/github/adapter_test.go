package github

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
)

type fakeCaller struct {
	commands []runner.Command
	results  []runner.Result
	err      error
}

func (fake *fakeCaller) Call(_ context.Context, command runner.Command) (runner.Result, error) {
	fake.commands = append(fake.commands, command)
	if fake.err != nil {
		return runner.Result{}, fake.err
	}
	result := fake.results[0]
	fake.results = fake.results[1:]
	return result, nil
}

func repository() policy.ResolvedRepository {
	return policy.ResolvedRepository{
		ID:         "project",
		Repository: config.Repository{Owner: "owner", Name: "repo"},
		Provider:   config.Provider{Kind: config.ProviderGitHub, APIHost: "github.example"},
	}
}

func request(operation any) *repowolfv1.GitHubRequest {
	req := &repowolfv1.GitHubRequest{}
	switch value := operation.(type) {
	case *repowolfv1.GitHubRequest_RepositoryView:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueList:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueView:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueCreate:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueEdit:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueComment:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueClose:
		req.Operation = value
	case *repowolfv1.GitHubRequest_IssueReopen:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullList:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullView:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullCreate:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullEdit:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullComment:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullClose:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullReopen:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullReady:
		req.Operation = value
	case *repowolfv1.GitHubRequest_PullChecks:
		req.Operation = value
	case *repowolfv1.GitHubRequest_RunList:
		req.Operation = value
	case *repowolfv1.GitHubRequest_RunView:
		req.Operation = value
	case *repowolfv1.GitHubRequest_StatusView:
		req.Operation = value
	default:
		panic("unsupported test operation")
	}
	return req
}

func TestOperationCapabilityMatrix(t *testing.T) {
	for _, test := range validOperations() {
		t.Run(test.name, func(t *testing.T) {
			got, err := Capability(test.request)
			if err != nil || got != test.capability {
				t.Fatalf("Capability() = %q, %v, want %q", got, err, test.capability)
			}
			if err := ValidateGitHubRequest(test.request); err != nil {
				t.Fatalf("ValidateGitHubRequest() = %v", err)
			}
		})
	}
}

func TestValidateAcceptsGitHubLabelNamesWithSpaces(t *testing.T) {
	req := request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: "title", Labels: []string{"good first issue"}}})
	if err := ValidateGitHubRequest(req); err != nil {
		t.Fatalf("ValidateGitHubRequest()=%v", err)
	}
}

func TestValidateRejectsInvalidRequests(t *testing.T) {
	longTitle := make([]byte, 257)
	for i := range longTitle {
		longTitle[i] = 'x'
	}
	for _, req := range []*repowolfv1.GitHubRequest{
		nil,
		{},
		request(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{}}),
		request(&repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN}}),
		request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: string(longTitle)}}),
		request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1}}),
		request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1}}),
		request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: "title", Head: "bad\nhead", Base: "main"}}),
		request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{Limit: 101}}),
		request(&repowolfv1.GitHubRequest_StatusView{StatusView: &repowolfv1.GitHubStatusViewRequest{ObjectId: "main"}}),
	} {
		if err := ValidateGitHubRequest(req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("ValidateGitHubRequest(%T) = %v, want ErrInvalidRequest", operationOf(req), err)
		}
	}
}

func TestRunListUsesOnlyKnownStatusEnumAsGeneratedQuery(t *testing.T) {
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(`{"workflow_runs":[]}`)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	status := repowolfv1.GitHubRunStatus_GIT_HUB_RUN_STATUS_PENDING
	req := request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{Limit: 1, Status: &status}})
	if _, err := adapter.Execute(context.Background(), repository(), req); err != nil {
		t.Fatal(err)
	}
	if got := caller.commands[0].Args[len(caller.commands[0].Args)-1]; got != "/repos/owner/repo/actions/runs?per_page=1&status=pending" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestIssueMutationPreflightsResourceTypeBeforeWrite(t *testing.T) {
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(`{"number":1,"pull_request":{}}`)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	title := "new"
	req := request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1, Title: &title}})
	if _, err := adapter.Execute(context.Background(), repository(), req); err == nil {
		t.Fatal("pull request accepted as issue")
	}
	if len(caller.commands) != 1 || caller.commands[0].Args[3] != "--method" || caller.commands[0].Args[4] != "GET" {
		t.Fatalf("commands = %#v", caller.commands)
	}
}

func TestPullReadyUsesFixedNativeCommandThenNormalizesView(t *testing.T) {
	pull := `{"number":1,"title":"title","body":null,"state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean"}`
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte("ready\n")}, {Stdout: []byte(pull)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_PullReady{PullReady: &repowolfv1.GitHubPullReadyRequest{Number: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	if response.GetPullReady().GetPull().GetNumber() != 1 {
		t.Fatalf("response = %#v", response)
	}
	want := []string{"pr", "ready", "1", "--repo", "github.example/owner/repo"}
	if len(caller.commands) != 2 || !reflect.DeepEqual(caller.commands[0].Args, want) {
		t.Fatalf("commands = %#v", caller.commands)
	}
}

func TestPullChecksPaginatesChecksAndStatusesIntoOneTypedList(t *testing.T) {
	head := `{"head":{"sha":"0123456789012345678901234567890123456789"}}`
	checks := `{"total_count":1,"check_runs":[{"name":"ci","status":"completed","conclusion":"success","details_url":null,"output":null}]}`
	statuses := `{"total_count":1,"statuses":[{"context":"policy","state":"success","target_url":null}]}`
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(head)}, {Stdout: []byte(checks)}, {Stdout: []byte(statuses)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksRequest{Number: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetPullChecks().GetChecks()) != 2 || len(caller.commands) != 3 {
		t.Fatalf("response/commands = %#v/%d", response, len(caller.commands))
	}
}

func TestPullChecksEnforcesCombinedRecordBudget(t *testing.T) {
	for _, test := range []struct {
		name        string
		statusTotal int
		wantErr     bool
	}{
		{"exactly one thousand", 500, false}, {"one thousand one", 501, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			results := []runner.Result{{Stdout: []byte(`{"head":{"sha":"0123456789012345678901234567890123456789"}}`)}}
			for range 5 {
				results = append(results, runner.Result{Stdout: []byte(checkPageJSON("check_runs", 500, 100))})
			}
			for range 5 {
				results = append(results, runner.Result{Stdout: []byte(checkPageJSON("statuses", test.statusTotal, 100))})
			}
			caller := &fakeCaller{results: results}
			adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
			if err != nil {
				t.Fatal(err)
			}
			response, err := adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksRequest{Number: 1}}))
			if test.wantErr {
				if !errors.Is(err, runner.ErrOutputLimit) || response != nil {
					t.Fatalf("Execute()=%#v,%v", response, err)
				}
				return
			}
			if err != nil || len(response.GetPullChecks().Checks) != 1000 {
				t.Fatalf("Execute()=%#v,%v commands=%d", response, err, len(caller.commands))
			}
		})
	}
}

func checkPageJSON(field string, total, count int) string {
	entry := `{"name":"check","status":"completed"}`
	if field == "statuses" {
		entry = `{"context":"status","state":"success"}`
	}
	values := ""
	for index := 0; index < count; index++ {
		if index > 0 {
			values += ","
		}
		values += entry
	}
	return `{"total_count":` + strconv.Itoa(total) + `,"` + field + `":[` + values + `]}`
}

func TestAllTypedOperationsExecuteWithoutClientCommandSurface(t *testing.T) {
	issue := `{"number":1,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`
	pull := `{"number":1,"title":"title","body":"body","state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean"}`
	comment := `{"id":1,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u"}`
	run := `{"id":1,"name":"CI","display_title":"run","status":"completed","conclusion":"success","event":"push","head_branch":"main","head_sha":"0123456789012345678901234567890123456789","html_url":"https://safe.example/r","created_at":"c","updated_at":"u","run_attempt":1,"jobs_url":"https://safe.example/jobs"}`
	responses := map[string][]string{
		"repository_view": {`{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","description":null,"private":false,"html_url":"https://safe.example/repo","default_branch":"main"}`},
		"issue_list":      {`{"items":[]}`}, "issue_view": {issue}, "issue_create": {issue},
		"issue_edit": {`{"number":1}`, issue}, "issue_comment": {`{"number":1}`, comment}, "issue_close": {`{"number":1}`, issue}, "issue_reopen": {`{"number":1}`, issue},
		"pull_list": {`[]`}, "pull_view": {pull}, "pull_create": {pull}, "pull_edit": {pull}, "pull_comment": {pull, comment}, "pull_close": {pull}, "pull_reopen": {pull},
		"run_list": {`{"workflow_runs":[]}`}, "run_view": {run}, "status_view": {`{"state":"success","sha":"0123456789012345678901234567890123456789","statuses":[]}`},
	}
	for _, test := range validOperations() {
		if test.name == "pull_ready" || test.name == "pull_checks" {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			values := responses[test.name]
			results := make([]runner.Result, len(values))
			for i, value := range values {
				results[i] = runner.Result{Stdout: []byte(value)}
			}
			caller := &fakeCaller{results: results}
			adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
			if err != nil {
				t.Fatal(err)
			}
			response, err := adapter.Execute(context.Background(), repository(), test.request)
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}
			if response == nil {
				t.Fatal("nil response")
			}
			for _, command := range caller.commands {
				if command.Path != "/pinned/gh" || command.Args[0] != "api" {
					t.Fatalf("command = %#v", command)
				}
			}
		})
	}
}

func TestAdapterRejectsRawOutputAboveCommandBudget(t *testing.T) {
	raw := []byte(`{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","description":null,"private":false,"html_url":"https://safe.example/repo","default_branch":"main"}`)
	padding := miB - len(raw) + 1
	raw = append(raw, make([]byte, padding)...)
	for i := len(raw) - padding; i < len(raw); i++ {
		raw[i] = ' '
	}
	caller := &fakeCaller{results: []runner.Result{{Stdout: raw}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}))
	if !errors.Is(err, runner.ErrOutputLimit) {
		t.Fatalf("Execute()=%v", err)
	}
}

func TestAdapterBuildsPinnedRepositoryCommandAndNormalizes(t *testing.T) {
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(`{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","description":null,"private":false,"html_url":"https://safe.example/repo","default_branch":"main"}`)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{"GH_TOKEN=secret"}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}))
	if err != nil {
		t.Fatal(err)
	}
	if response.GetRepositoryView().GetRepository().GetNameWithOwner() != "owner/repo" {
		t.Fatalf("response = %#v", response)
	}
	if len(caller.commands) != 1 {
		t.Fatalf("commands = %d", len(caller.commands))
	}
	got := caller.commands[0]
	wantArgs := []string{"api", "--hostname", "github.example", "--method", "GET", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28", "/repos/owner/repo"}
	if got.Path != "/pinned/gh" || !reflect.DeepEqual(got.Args, wantArgs) || !reflect.DeepEqual(got.Env, []string{"GH_TOKEN=secret"}) {
		t.Fatalf("command = %#v", got)
	}
}

type operationCase struct {
	name       string
	request    *repowolfv1.GitHubRequest
	capability config.Capability
}

func validOperations() []operationCase {
	issueOpen := repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN
	pullOpen := repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_OPEN
	body, title, base := "body", "title", "main"
	return []operationCase{
		{"repository_view", request(&repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}), config.RepositoryRead},
		{"issue_list", request(&repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: issueOpen, Limit: 1}}), config.IssuesRead},
		{"issue_view", request(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1}}), config.IssuesRead},
		{"issue_create", request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: "title", Body: &body}}), config.IssuesWrite},
		{"issue_edit", request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1, Title: &title}}), config.IssuesWrite},
		{"issue_comment", request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: "body"}}), config.IssuesWrite},
		{"issue_close", request(&repowolfv1.GitHubRequest_IssueClose{IssueClose: &repowolfv1.GitHubIssueCloseRequest{Number: 1}}), config.IssuesWrite},
		{"issue_reopen", request(&repowolfv1.GitHubRequest_IssueReopen{IssueReopen: &repowolfv1.GitHubIssueReopenRequest{Number: 1}}), config.IssuesWrite},
		{"pull_list", request(&repowolfv1.GitHubRequest_PullList{PullList: &repowolfv1.GitHubPullListRequest{State: pullOpen, Limit: 1}}), config.PullRequestsRead},
		{"pull_view", request(&repowolfv1.GitHubRequest_PullView{PullView: &repowolfv1.GitHubPullViewRequest{Number: 1}}), config.PullRequestsRead},
		{"pull_create", request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: "title", Head: "topic", Base: "main"}}), config.PullRequestsWrite},
		{"pull_edit", request(&repowolfv1.GitHubRequest_PullEdit{PullEdit: &repowolfv1.GitHubPullEditRequest{Number: 1, Base: &base}}), config.PullRequestsWrite},
		{"pull_comment", request(&repowolfv1.GitHubRequest_PullComment{PullComment: &repowolfv1.GitHubPullCommentRequest{Number: 1, Body: "body"}}), config.PullRequestsWrite},
		{"pull_close", request(&repowolfv1.GitHubRequest_PullClose{PullClose: &repowolfv1.GitHubPullCloseRequest{Number: 1}}), config.PullRequestsWrite},
		{"pull_reopen", request(&repowolfv1.GitHubRequest_PullReopen{PullReopen: &repowolfv1.GitHubPullReopenRequest{Number: 1}}), config.PullRequestsWrite},
		{"pull_ready", request(&repowolfv1.GitHubRequest_PullReady{PullReady: &repowolfv1.GitHubPullReadyRequest{Number: 1}}), config.PullRequestsWrite},
		{"pull_checks", request(&repowolfv1.GitHubRequest_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksRequest{Number: 1}}), config.StatusesRead},
		{"run_list", request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{Limit: 1}}), config.ActionsRead},
		{"run_view", request(&repowolfv1.GitHubRequest_RunView{RunView: &repowolfv1.GitHubRunViewRequest{RunId: 1}}), config.ActionsRead},
		{"status_view", request(&repowolfv1.GitHubRequest_StatusView{StatusView: &repowolfv1.GitHubStatusViewRequest{ObjectId: "0123456789012345678901234567890123456789"}}), config.StatusesRead},
	}
}

func operationOf(req *repowolfv1.GitHubRequest) any {
	if req == nil {
		return nil
	}
	return req.Operation
}
