package github

import (
	"encoding/json"
	"net/url"
	"strconv"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const (
	apiVersion = "2022-11-28"
	apiAccept  = "application/vnd.github+json"
	miB        = 1 << 20
)

type commandPlan struct {
	command   runner.Command
	normalize string
}

func (adapter *Adapter) plan(repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (commandPlan, error) {
	base := "/repos/" + repository.Repository.Owner + "/" + repository.Repository.Name
	var method, endpoint, normalizer string
	var input any
	switch operation := request.Operation.(type) {
	case *repowolfv1.GitHubRequest_RepositoryView:
		method, endpoint, normalizer = "GET", base, "repository"
	case *repowolfv1.GitHubRequest_IssueList:
		query := "repo:" + repository.Repository.Owner + "/" + repository.Repository.Name + " is:issue"
		if operation.IssueList.State != repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL {
			query += " state:" + issueState(operation.IssueList.State)
		}
		method, endpoint, normalizer = "GET", "/search/issues?q="+url.QueryEscape(query)+"&per_page="+decimal(operation.IssueList.Limit), "issue_list"
	case *repowolfv1.GitHubRequest_IssueView:
		method, endpoint, normalizer = "GET", base+"/issues/"+decimal(operation.IssueView.Number), "issue"
	case *repowolfv1.GitHubRequest_IssueCreate:
		method, endpoint, normalizer = "POST", base+"/issues", "issue"
		input = createIssueBody(operation.IssueCreate)
	case *repowolfv1.GitHubRequest_IssueEdit:
		method, endpoint, normalizer = "PATCH", base+"/issues/"+decimal(operation.IssueEdit.Number), "issue"
		input = editIssueBody(operation.IssueEdit)
	case *repowolfv1.GitHubRequest_IssueComment:
		method, endpoint, normalizer = "POST", base+"/issues/"+decimal(operation.IssueComment.Number)+"/comments", "comment"
		input = map[string]any{"body": operation.IssueComment.Body}
	case *repowolfv1.GitHubRequest_IssueClose:
		method, endpoint, normalizer, input = "PATCH", base+"/issues/"+decimal(operation.IssueClose.Number), "issue", map[string]any{"state": "closed"}
	case *repowolfv1.GitHubRequest_IssueReopen:
		method, endpoint, normalizer, input = "PATCH", base+"/issues/"+decimal(operation.IssueReopen.Number), "issue", map[string]any{"state": "open"}
	case *repowolfv1.GitHubRequest_PullList:
		query := url.Values{"per_page": {decimal(operation.PullList.Limit)}, "state": {pullState(operation.PullList.State)}}
		if operation.PullList.Base != nil {
			query.Set("base", *operation.PullList.Base)
		}
		if operation.PullList.Head != nil {
			query.Set("head", *operation.PullList.Head)
		}
		method, endpoint, normalizer = "GET", base+"/pulls?"+query.Encode(), "pull_list"
	case *repowolfv1.GitHubRequest_PullView:
		method, endpoint, normalizer = "GET", base+"/pulls/"+decimal(operation.PullView.Number), "pull"
	case *repowolfv1.GitHubRequest_PullCreate:
		method, endpoint, normalizer = "POST", base+"/pulls", "pull"
		input = createPullBody(operation.PullCreate)
	case *repowolfv1.GitHubRequest_PullEdit:
		method, endpoint, normalizer = "PATCH", base+"/pulls/"+decimal(operation.PullEdit.Number), "pull"
		input = editPullBody(operation.PullEdit)
	case *repowolfv1.GitHubRequest_PullComment:
		method, endpoint, normalizer = "POST", base+"/issues/"+decimal(operation.PullComment.Number)+"/comments", "comment"
		input = map[string]any{"body": operation.PullComment.Body}
	case *repowolfv1.GitHubRequest_PullClose:
		method, endpoint, normalizer, input = "PATCH", base+"/pulls/"+decimal(operation.PullClose.Number), "pull", map[string]any{"state": "closed"}
	case *repowolfv1.GitHubRequest_PullReopen:
		method, endpoint, normalizer, input = "PATCH", base+"/pulls/"+decimal(operation.PullReopen.Number), "pull", map[string]any{"state": "open"}
	case *repowolfv1.GitHubRequest_RunList:
		query := url.Values{"per_page": {decimal(operation.RunList.Limit)}}
		if operation.RunList.Branch != nil {
			query.Set("branch", *operation.RunList.Branch)
		}
		if operation.RunList.Status != nil {
			query.Set("status", runStatus(*operation.RunList.Status))
		}
		method, endpoint, normalizer = "GET", base+"/actions/runs?"+query.Encode(), "run_list"
	case *repowolfv1.GitHubRequest_RunView:
		method, endpoint, normalizer = "GET", base+"/actions/runs/"+decimal(operation.RunView.RunId), "run"
	case *repowolfv1.GitHubRequest_StatusView:
		method, endpoint, normalizer = "GET", base+"/commits/"+operation.StatusView.ObjectId+"/status", "status"
	default:
		return commandPlan{}, ErrInvalidRequest
	}
	return commandPlan{command: adapter.apiCommand(repository.Provider.APIHost, method, endpoint, input, outputLimit(normalizer)), normalize: normalizer}, nil
}

func (adapter *Adapter) apiCommand(host, method, endpoint string, value any, stdoutLimit int) runner.Command {
	args := []string{"api", "--hostname", host, "--method", method, "-H", "Accept: " + apiAccept, "-H", "X-GitHub-Api-Version: " + apiVersion}
	var stdin []byte
	if value != nil {
		stdin, _ = json.Marshal(value)
		args = append(args, "--input", "-")
	}
	args = append(args, endpoint)
	return adapter.command(args, stdin, stdoutLimit)
}
func (adapter *Adapter) command(args []string, stdin []byte, stdoutLimit int) runner.Command {
	return runner.Command{Path: adapter.path, Args: args, Stdin: stdin, Env: adapter.environment, Timeout: adapter.timeout, StdinLimit: miB, StdoutLimit: stdoutLimit, StderrLimit: 64 << 10}
}
func createIssueBody(value *repowolfv1.GitHubIssueCreateRequest) map[string]any {
	body := map[string]any{"title": value.Title, "labels": value.Labels, "assignees": value.Assignees}
	if value.Body != nil {
		body["body"] = *value.Body
	}
	return body
}
func editIssueBody(value *repowolfv1.GitHubIssueEditRequest) map[string]any {
	body := map[string]any{}
	if value.Title != nil {
		body["title"] = *value.Title
	}
	if value.Body != nil {
		body["body"] = *value.Body
	}
	if value.Labels != nil {
		body["labels"] = value.Labels.Values
	}
	if value.Assignees != nil {
		body["assignees"] = value.Assignees.Values
	}
	return body
}
func createPullBody(value *repowolfv1.GitHubPullCreateRequest) map[string]any {
	body := map[string]any{"title": value.Title, "head": value.Head, "base": value.Base}
	if value.Body != nil {
		body["body"] = *value.Body
	}
	if value.Draft != nil {
		body["draft"] = *value.Draft
	}
	return body
}
func editPullBody(value *repowolfv1.GitHubPullEditRequest) map[string]any {
	body := map[string]any{}
	if value.Title != nil {
		body["title"] = *value.Title
	}
	if value.Body != nil {
		body["body"] = *value.Body
	}
	if value.Base != nil {
		body["base"] = *value.Base
	}
	return body
}
func decimal(value uint64) string { return strconv.FormatUint(value, 10) }
func issueState(value repowolfv1.GitHubIssueState) string {
	return map[repowolfv1.GitHubIssueState]string{repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN: "open", repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_CLOSED: "closed", repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL: "all"}[value]
}
func pullState(value repowolfv1.GitHubPullState) string {
	return map[repowolfv1.GitHubPullState]string{repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_OPEN: "open", repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_CLOSED: "closed", repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_ALL: "all"}[value]
}
func runStatus(value repowolfv1.GitHubRunStatus) string {
	return map[repowolfv1.GitHubRunStatus]string{1: "queued", 2: "in_progress", 3: "completed", 4: "success", 5: "failure", 6: "cancelled", 7: "skipped", 8: "timed_out", 9: "action_required", 10: "neutral", 11: "stale", 12: "startup_failure", 13: "requested", 14: "waiting", 15: "pending"}[value]
}
func outputLimit(normalizer string) int {
	if normalizer == "repository" {
		return miB
	}
	if normalizer == "issue" || normalizer == "pull" || normalizer == "comment" || normalizer == "run" {
		return 2 * miB
	}
	return 8 * miB
}
