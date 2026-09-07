package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

// Regression: issue views must fetch the fixed issue endpoint exactly once and
// fetch the bounded comments endpoint only when comments were requested.
func TestIssueViewCommentsRequestedOnly(t *testing.T) {
	t.Run("comments excluded", func(t *testing.T) {
		caller := &fakeCaller{results: []runner.Result{{Stdout: issueViewFixture()}}}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(false))
		if err != nil {
			t.Fatal(err)
		}
		if response.GetIssueView().GetIssue().GetComments() != nil {
			t.Fatalf("comments = %#v, want nil", response.GetIssueView().GetIssue().GetComments())
		}
		assertIssueViewEndpoint(t, caller.commands, 0)
		if len(caller.commands) != 1 {
			t.Fatalf("commands = %d, want issue request only", len(caller.commands))
		}
	})

	t.Run("short comment page", func(t *testing.T) {
		comment := commentFixture(1)
		caller := &fakeCaller{results: []runner.Result{{Stdout: issueViewFixture()}, {Stdout: commentPage(t, comment)}}}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(true))
		if err != nil {
			t.Fatal(err)
		}
		comments := response.GetIssueView().GetIssue().GetComments()
		if len(comments) != 1 {
			t.Fatalf("comments = %d, want 1", len(comments))
		}
		if got := comments[0]; got.GetId() != 1 || got.GetAuthor() != "reviewer" || got.GetBody() != "comment 1" || got.GetUrl() != "https://github.example/owner/repo/issues/1#issuecomment-1" || got.GetCreatedAt() != "2026-09-01T00:00:00Z" || got.GetUpdatedAt() != "2026-09-02T00:00:00Z" {
			t.Fatalf("comment = %#v", got)
		}
		assertIssueViewEndpoint(t, caller.commands, 0)
		assertCommentEndpoint(t, caller.commands, 1, 1, 100)
		if len(caller.commands) != 2 {
			t.Fatalf("commands = %d, want issue and one comment page", len(caller.commands))
		}
	})
}

// Regression: a short page before page ten is complete and must not trigger an
// overflow probe or any later comment request.
func TestIssueViewCommentsStopOnShortPage(t *testing.T) {
	caller := &fakeCaller{results: []runner.Result{
		{Stdout: issueViewFixture()},
		{Stdout: commentFixtures(t, 1, 100)},
		{Stdout: commentFixtures(t, 101, 1)},
	}}
	response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(true))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(response.GetIssueView().GetIssue().GetComments()); got != 101 {
		t.Fatalf("comments = %d, want 101", got)
	}
	if len(caller.commands) != 3 {
		t.Fatalf("commands = %d, want issue and two comment pages", len(caller.commands))
	}
	assertCommentEndpoint(t, caller.commands, 2, 2, 100)
}

// Regression: exactly 1,000 comments are allowed only after an empty bounded
// probe, while a nonempty probe proves truncation and fails closed.
func TestIssueViewCommentOverflow(t *testing.T) {
	results := []runner.Result{{Stdout: issueViewFixture()}}
	for page := 0; page < 10; page++ {
		results = append(results, runner.Result{Stdout: commentFixtures(t, page*100+1, 100)})
	}

	t.Run("empty probe permits exactly one thousand", func(t *testing.T) {
		caller := &fakeCaller{results: append(append([]runner.Result{}, results...), runner.Result{Stdout: []byte(`[]`)})}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(true))
		if err != nil {
			t.Fatal(err)
		}
		if got := len(response.GetIssueView().GetIssue().GetComments()); got != 1_000 {
			t.Fatalf("comments = %d, want 1000", got)
		}
		if len(caller.commands) != 12 {
			t.Fatalf("commands = %d, want issue, ten pages, and probe", len(caller.commands))
		}
		for page := 1; page <= 10; page++ {
			assertCommentEndpoint(t, caller.commands, page, page, 100)
		}
		assertCommentEndpoint(t, caller.commands, 11, 11, 1)
	})

	t.Run("nonempty probe rejects truncation", func(t *testing.T) {
		caller := &fakeCaller{results: append(append([]runner.Result{}, results...), runner.Result{Stdout: commentPage(t, commentFixture(1_001))})}
		response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(true))
		if response != nil || !errors.Is(err, runner.ErrOutputLimit) {
			t.Fatalf("Execute() = %#v, %v, want output limit", response, err)
		}
		if len(caller.commands) != 12 {
			t.Fatalf("commands = %d, want bounded probe", len(caller.commands))
		}
	})
}

// Regression: issue/comment JSON and every required typed comment field must
// be validated before any provider data is returned.
func TestIssueViewCommentsRejectInvalidResponses(t *testing.T) {
	validIssue := issueViewFixture()
	validComment := commentFixture(1)
	tests := []struct {
		name    string
		results []runner.Result
	}{
		{"pull request marker", []runner.Result{{Stdout: []byte(strings.Replace(string(validIssue), `"updated_at":"2026-09-02T00:00:00Z"`, `"updated_at":"2026-09-02T00:00:00Z","pull_request":{}`, 1))}}},
		{"malformed issue JSON", []runner.Result{{Stdout: []byte(`{`)}}},
		{"malformed comment JSON", []runner.Result{{Stdout: validIssue}, {Stdout: []byte(`[`)}}},
	}
	for _, field := range []string{"user", "body", "id", "html_url", "created_at", "updated_at"} {
		comment := cloneCommentFixture(t, validComment)
		delete(comment, field)
		tests = append(tests, struct {
			name    string
			results []runner.Result
		}{"missing comment " + field, []runner.Result{{Stdout: validIssue}, {Stdout: commentPage(t, comment)}}})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: test.results}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueViewRequest(true))
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v, want rejection", response, err)
			}
		})
	}
}

// Regression: cancellation between comment pages must prevent a later provider
// call and all issue-view calls must retain the original context.
func TestIssueViewCommentsStopOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	caller := &scriptedIssueViewCaller{call: func(callCtx context.Context, _ runner.Command, index int) (runner.Result, error) {
		if callCtx != ctx {
			t.Fatal("provider call did not receive original context")
		}
		switch index {
		case 0:
			return runner.Result{Stdout: issueViewFixture()}, nil
		case 1:
			cancel()
			return runner.Result{Stdout: commentFixtures(t, 1, 100)}, nil
		default:
			return runner.Result{}, fmt.Errorf("unexpected provider call %d", index+1)
		}
	}}
	response, err := testAdapter(t, caller).Execute(ctx, repository(), issueViewRequest(true))
	if response != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() = %#v, %v, want context canceled", response, err)
	}
	if len(caller.commands) != 2 {
		t.Fatalf("commands = %d, want issue and first comment page", len(caller.commands))
	}
}

type scriptedIssueViewCaller struct {
	commands []runner.Command
	call     func(context.Context, runner.Command, int) (runner.Result, error)
}

func (caller *scriptedIssueViewCaller) Call(ctx context.Context, command runner.Command) (runner.Result, error) {
	caller.commands = append(caller.commands, command)
	return caller.call(ctx, command, len(caller.commands)-1)
}

func issueViewRequest(includeComments bool) *repowolfv1.GitHubRequest {
	return request(&repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: 1, IncludeComments: includeComments}})
}

func issueViewFixture() []byte {
	return []byte(`{"number":1,"title":"Issue 1","body":"Body","state":"open","user":{"login":"octocat"},"assignees":[],"labels":[{"name":"bug"}],"html_url":"https://github.example/owner/repo/issues/1","created_at":"2026-09-01T00:00:00Z","updated_at":"2026-09-02T00:00:00Z"}`)
}

func commentFixture(id int) map[string]any {
	return map[string]any{
		"id": id, "user": map[string]any{"login": "reviewer"}, "body": fmt.Sprintf("comment %d", id),
		"html_url":   fmt.Sprintf("https://github.example/owner/repo/issues/1#issuecomment-%d", id),
		"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-02T00:00:00Z",
	}
}

func cloneCommentFixture(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func commentFixtures(t *testing.T, first, count int) []byte {
	t.Helper()
	comments := make([]map[string]any, count)
	for index := range comments {
		comments[index] = commentFixture(first + index)
	}
	return commentPage(t, comments...)
}

func commentPage(t *testing.T, comments ...map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(comments)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertIssueViewEndpoint(t *testing.T, commands []runner.Command, index int) {
	t.Helper()
	if len(commands) <= index {
		t.Fatalf("commands = %d, missing command %d", len(commands), index+1)
	}
	command := commands[index]
	if got := command.Args[len(command.Args)-1]; got != "/repos/owner/repo/issues/1" {
		t.Fatalf("command %d endpoint = %q", index+1, got)
	}
	if command.Args[3] != "--method" || command.Args[4] != "GET" || len(command.Stdin) != 0 {
		t.Fatalf("command %d = %#v", index+1, command)
	}
}

func assertCommentEndpoint(t *testing.T, commands []runner.Command, index, page, perPage int) {
	t.Helper()
	if len(commands) <= index {
		t.Fatalf("commands = %d, missing command %d", len(commands), index+1)
	}
	want := fmt.Sprintf("/repos/owner/repo/issues/1/comments?page=%d&per_page=%d", page, perPage)
	if got := commands[index].Args[len(commands[index].Args)-1]; got != want {
		t.Fatalf("command %d endpoint = %q, want %q", index+1, got, want)
	}
	if commands[index].Args[3] != "--method" || commands[index].Args[4] != "GET" || len(commands[index].Stdin) != 0 {
		t.Fatalf("command %d = %#v", index+1, commands[index])
	}
}
