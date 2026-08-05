package github

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/runner"
)

type typedCommandContract struct {
	results  []runner.Result
	commands []runner.Command
}

func TestAllTypedOperationsProduceExactPinnedCommandContracts(t *testing.T) {
	contracts := allTypedCommandContracts()
	operations := validOperations()
	if len(operations) != 20 || len(contracts) != 20 {
		t.Fatalf("operations/contracts = %d/%d, want 20/20", len(operations), len(contracts))
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			contract, ok := contracts[operation.name]
			if !ok {
				t.Fatalf("missing command contract for %s", operation.name)
			}
			capability, err := Capability(operation.request)
			if err != nil || capability != operation.capability {
				t.Fatalf("Capability() = %q, %v, want %q", capability, err, operation.capability)
			}
			caller := &fakeCaller{results: contract.results}
			adapter := testAdapter(t, caller)
			response, err := adapter.Execute(context.Background(), repository(), operation.request)
			if err != nil || response == nil {
				t.Fatalf("Execute() = %#v, %v", response, err)
			}
			if !reflect.DeepEqual(caller.commands, contract.commands) {
				t.Fatalf("commands = %#v\nwant %#v", caller.commands, contract.commands)
			}
		})
	}
}

func allTypedCommandContracts() map[string]typedCommandContract {
	issue := `{"number":1,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`
	pull := `{"number":1,"title":"title","body":"body","state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean"}`
	comment := `{"id":1,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u"}`
	run := `{"id":1,"name":"CI","display_title":"run","status":"completed","conclusion":"success","event":"push","head_branch":"main","head_sha":"0123456789012345678901234567890123456789","html_url":"https://safe.example/r","created_at":"c","updated_at":"u","run_attempt":1,"jobs_url":"https://safe.example/jobs"}`
	repositoryJSON := `{"name":"repo","owner":{"login":"owner"},"full_name":"owner/repo","description":null,"private":false,"html_url":"https://safe.example/repo","default_branch":"main"}`
	preflight := `{"number":1}`
	head := `{"head":{"sha":"0123456789012345678901234567890123456789"}}`
	checks := `{"total_count":0,"check_runs":[]}`
	statuses := `{"total_count":0,"statuses":[]}`
	base := "/repos/owner/repo"
	result := func(values ...string) []runner.Result {
		results := make([]runner.Result, len(values))
		for index, value := range values {
			results[index] = runner.Result{Stdout: []byte(value)}
		}
		return results
	}
	return map[string]typedCommandContract{
		"repository_view": {result(repositoryJSON), []runner.Command{expectedAPI("GET", base, nil, miB)}},
		"issue_list":      {result(`{"items":[]}`), []runner.Command{expectedAPI("GET", "/search/issues?q=repo%3Aowner%2Frepo+is%3Aissue+state%3Aopen&per_page=1", nil, 8*miB)}},
		"issue_view":      {result(issue), []runner.Command{expectedAPI("GET", base+"/issues/1", nil, 2*miB)}},
		"issue_create":    {result(issue), []runner.Command{expectedAPI("POST", base+"/issues", []byte(`{"assignees":null,"body":"body","labels":null,"title":"title"}`), 2*miB)}},
		"issue_edit":      {result(preflight, issue), []runner.Command{expectedAPI("GET", base+"/issues/1", nil, 4*miB), expectedAPI("PATCH", base+"/issues/1", []byte(`{"title":"title"}`), 4*miB-len(preflight))}},
		"issue_comment":   {result(preflight, comment), []runner.Command{expectedAPI("GET", base+"/issues/1", nil, 4*miB), expectedAPI("POST", base+"/issues/1/comments", []byte(`{"body":"body"}`), 4*miB-len(preflight))}},
		"issue_close":     {result(preflight, issue), []runner.Command{expectedAPI("GET", base+"/issues/1", nil, 4*miB), expectedAPI("PATCH", base+"/issues/1", []byte(`{"state":"closed"}`), 4*miB-len(preflight))}},
		"issue_reopen":    {result(preflight, issue), []runner.Command{expectedAPI("GET", base+"/issues/1", nil, 4*miB), expectedAPI("PATCH", base+"/issues/1", []byte(`{"state":"open"}`), 4*miB-len(preflight))}},
		"pull_list":       {result(`[]`), []runner.Command{expectedAPI("GET", base+"/pulls?per_page=1&state=open", nil, 8*miB)}},
		"pull_view":       {result(pull), []runner.Command{expectedAPI("GET", base+"/pulls/1", nil, 2*miB)}},
		"pull_create":     {result(pull), []runner.Command{expectedAPI("POST", base+"/pulls", []byte(`{"base":"main","head":"topic","title":"title"}`), 2*miB)}},
		"pull_edit":       {result(pull), []runner.Command{expectedAPI("PATCH", base+"/pulls/1", []byte(`{"base":"main"}`), 2*miB)}},
		"pull_comment":    {result(pull, comment), []runner.Command{expectedAPI("GET", base+"/pulls/1", nil, 4*miB), expectedAPI("POST", base+"/issues/1/comments", []byte(`{"body":"body"}`), 4*miB-len(pull))}},
		"pull_close":      {result(pull), []runner.Command{expectedAPI("PATCH", base+"/pulls/1", []byte(`{"state":"closed"}`), 2*miB)}},
		"pull_reopen":     {result(pull), []runner.Command{expectedAPI("PATCH", base+"/pulls/1", []byte(`{"state":"open"}`), 2*miB)}},
		"pull_ready":      {result("ready\n", pull), []runner.Command{expectedNative([]string{"pr", "ready", "1", "--repo", "github.example/owner/repo"}, miB), expectedAPI("GET", base+"/pulls/1", nil, 2*miB)}},
		"pull_checks":     {result(head, checks, statuses), []runner.Command{expectedAPI("GET", base+"/pulls/1", nil, 8*miB), expectedAPI("GET", base+"/commits/0123456789012345678901234567890123456789/check-runs?page=1&per_page=100", nil, 8*miB-len(head)), expectedAPI("GET", base+"/commits/0123456789012345678901234567890123456789/status?page=1&per_page=100", nil, 8*miB-len(head)-len(checks))}},
		"run_list":        {result(`{"workflow_runs":[]}`), []runner.Command{expectedAPI("GET", base+"/actions/runs?per_page=1", nil, 8*miB)}},
		"run_view":        {result(run), []runner.Command{expectedAPI("GET", base+"/actions/runs/1", nil, 2*miB)}},
		"status_view":     {result(`{"state":"success","sha":"0123456789012345678901234567890123456789","statuses":[]}`), []runner.Command{expectedAPI("GET", base+"/commits/0123456789012345678901234567890123456789/status", nil, 8*miB)}},
	}
}

func expectedAPI(method, endpoint string, stdin []byte, stdoutLimit int) runner.Command {
	args := []string{"api", "--hostname", "github.example", "--method", method, "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	if stdin != nil {
		args = append(args, "--input", "-")
	}
	args = append(args, endpoint)
	command := expectedNative(args, stdoutLimit)
	command.Stdin = stdin
	return command
}

func expectedNative(args []string, stdoutLimit int) runner.Command {
	return runner.Command{
		Path: "/pinned/gh", Args: args, Env: []string{"GH_TOKEN=secret"}, Timeout: time.Minute,
		StdinLimit: miB, StdoutLimit: stdoutLimit, StderrLimit: 64 << 10,
	}
}
