package github

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const mutationAggregateLimit = 4 * miB

func TestPreflightedMutationsShareExactAggregateStdoutBudget(t *testing.T) {
	issue := `{"number":1,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`
	comment := `{"id":1,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u"}`
	title := "new"

	tests := []struct {
		name      string
		request   *repowolfv1.GitHubRequest
		preflight string
		mutation  string
	}{
		{"issue edit", request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1, Title: &title}}), `{"number":1}`, issue},
		{"issue comment", request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: "body"}}), `{"number":1}`, comment},
		{"issue close", request(&repowolfv1.GitHubRequest_IssueClose{IssueClose: &repowolfv1.GitHubIssueCloseRequest{Number: 1}}), `{"number":1}`, issue},
		{"issue reopen", request(&repowolfv1.GitHubRequest_IssueReopen{IssueReopen: &repowolfv1.GitHubIssueReopenRequest{Number: 1}}), `{"number":1}`, issue},
		{"pull comment", request(&repowolfv1.GitHubRequest_PullComment{PullComment: &repowolfv1.GitHubPullCommentRequest{Number: 1, Body: "body"}}), `{"head":{}}`, comment},
	}

	for _, test := range tests {
		t.Run(test.name+" exact", func(t *testing.T) {
			preflight := paddedJSON(test.preflight, mutationAggregateLimit-len(test.mutation))
			caller := &fakeCaller{results: []runner.Result{{Stdout: preflight}, {Stdout: []byte(test.mutation)}}}
			adapter := testAdapter(t, caller)

			response, err := adapter.Execute(context.Background(), repository(), test.request)
			if err != nil || response == nil {
				t.Fatalf("Execute() = %#v, %v", response, err)
			}
			if len(caller.commands) != 2 {
				t.Fatalf("commands = %d, want preflight and mutation", len(caller.commands))
			}
			if got := caller.commands[1].StdoutLimit; got != len(test.mutation) {
				t.Fatalf("mutation stdout limit = %d, want remaining %d", got, len(test.mutation))
			}
		})

		t.Run(test.name+" exhausted", func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: paddedJSON(test.preflight, mutationAggregateLimit)}}}
			adapter := testAdapter(t, caller)

			response, err := adapter.Execute(context.Background(), repository(), test.request)
			if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
				t.Fatalf("Execute() = %#v, %v, want no result and output limit", response, err)
			}
			if len(caller.commands) != 1 {
				t.Fatalf("commands = %d, want no mutation write", len(caller.commands))
			}
		})

		t.Run(test.name+" one byte over", func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: paddedJSON(test.preflight, mutationAggregateLimit+1)}}}
			adapter := testAdapter(t, caller)

			response, err := adapter.Execute(context.Background(), repository(), test.request)
			if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
				t.Fatalf("Execute() = %#v, %v, want no result and output limit", response, err)
			}
			if len(caller.commands) != 1 {
				t.Fatalf("commands = %d, want no mutation write", len(caller.commands))
			}
		})
	}
}

func testAdapter(t *testing.T, caller Caller) *Adapter {
	t.Helper()
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{"GH_TOKEN=secret"}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func paddedJSON(value string, size int) []byte {
	if len(value) > size {
		panic("fixture exceeds requested size")
	}
	return []byte(value + strings.Repeat(" ", size-len(value)))
}
