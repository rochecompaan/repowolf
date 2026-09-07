package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const expectedIssueListQuery = `query IssueList($owner: String!, $name: String!, $states: [IssueState!], $cursor: String, $first: Int!) {
  repository(owner: $owner, name: $name) {
    issues(first: $first, after: $cursor, states: $states, orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        title
        body
        state
        labels(first: 100) { nodes { name } pageInfo { hasNextPage } }
        author { login }
        createdAt
        updatedAt
        url
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

type scriptedIssueListCaller struct {
	commands []runner.Command
	call     func(context.Context, runner.Command, int) (runner.Result, error)
}

func (caller *scriptedIssueListCaller) Call(ctx context.Context, command runner.Command) (runner.Result, error) {
	caller.commands = append(caller.commands, command)
	return caller.call(ctx, command, len(caller.commands)-1)
}

// Regression: Patchmill's 1,001-record probe must use bounded, oldest-first
// GraphQL pages rather than the REST search endpoint's 1,000-result window.
func TestIssueListGraphQLPagination(t *testing.T) {
	pages := make([][]byte, 0, 11)
	for page := 0; page < 11; page++ {
		count := 100
		if page == 10 {
			count = 1
		}
		nodes := make([]map[string]any, count)
		for index := range nodes {
			nodes[index] = graphQLIssueFixture(page*100 + index + 1)
		}
		pages = append(pages, graphQLIssuePage(nodes, page < 10, fmt.Sprintf("cursor-%d", page+1)))
	}
	caller := &scriptedIssueListCaller{call: func(_ context.Context, _ runner.Command, index int) (runner.Result, error) {
		return runner.Result{Stdout: pages[index]}, nil
	}}
	adapter := testAdapter(t, caller)
	request := issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN, 1_001)

	response, err := adapter.Execute(context.Background(), repository(), request)
	if err != nil {
		t.Fatal(err)
	}
	issues := response.GetIssueList().GetIssues()
	if len(issues) != 1_001 {
		t.Fatalf("issues = %d, want 1001", len(issues))
	}
	for index, issue := range issues {
		if issue.GetNumber() != uint64(index+1) {
			t.Fatalf("issues[%d].number = %d, want %d", index, issue.GetNumber(), index+1)
		}
	}
	if got := issues[0]; got.GetTitle() != "Issue 1" || got.GetBody() != "Body" || got.GetState() != "open" || got.GetAuthor() != "octocat" || !reflect.DeepEqual(got.GetLabels(), []string{"bug"}) || got.GetAssignees() == nil {
		t.Fatalf("normalized first issue = %#v", got)
	}

	if len(caller.commands) != 11 {
		t.Fatalf("commands = %d, want 11", len(caller.commands))
	}
	for index, command := range caller.commands {
		body := issueListCommandBody(t, command)
		wantFirst := float64(100)
		if index == 10 {
			wantFirst = 1
		}
		if body.Query != expectedIssueListQuery || body.Variables["first"] != wantFirst {
			t.Fatalf("command %d query/first = %q/%v", index+1, body.Query, body.Variables["first"])
		}
		if index == 0 && body.Variables["cursor"] != nil {
			t.Fatalf("first cursor = %#v, want null", body.Variables["cursor"])
		}
		if index > 0 && body.Variables["cursor"] != fmt.Sprintf("cursor-%d", index) {
			t.Fatalf("command %d cursor = %#v", index+1, body.Variables["cursor"])
		}
		if body.Variables["owner"] != "owner" || body.Variables["name"] != "repo" || !reflect.DeepEqual(body.Variables["states"], []any{"OPEN"}) {
			t.Fatalf("command %d variables = %#v", index+1, body.Variables)
		}
	}
}

// Regression: incomplete or contradictory GraphQL connection metadata must
// fail closed rather than truncate, reorder, or overrun the requested list.
func TestIssueListRejectsPaginationMetadata(t *testing.T) {
	validIssue := string(mustJSON(graphQLIssueFixture(1)))
	validLabels := `"labels":{"nodes":[{"name":"bug"}],"pageInfo":{"hasNextPage":false}}`
	validScalars := `"number":1,"title":"Issue 1","body":"Body","state":"OPEN",` + validLabels + `,"author":{"login":"octocat"},"createdAt":"2026-09-01T00:00:00Z","updatedAt":"2026-09-02T00:00:00Z","url":"https://github.example/owner/repo/issues/1"`
	page := func(nodes, pageInfo string) string {
		return `{"data":{"repository":{"issues":{"nodes":` + nodes + `,"pageInfo":` + pageInfo + `}}}}`
	}
	validPageInfo := `{"hasNextPage":false,"endCursor":"cursor-1"}`
	tests := []struct {
		name    string
		results []string
		limit   uint64
	}{
		{"GraphQL errors", []string{`{"errors":[{"message":"denied"}]}`}, 1},
		{"missing data", []string{`{}`}, 1},
		{"missing repository", []string{`{"data":{}}`}, 1},
		{"missing issues", []string{`{"data":{"repository":{}}}`}, 1},
		{"missing nodes", []string{`{"data":{"repository":{"issues":{"pageInfo":` + validPageInfo + `}}}}`}, 1},
		{"missing page info", []string{`{"data":{"repository":{"issues":{"nodes":[]}}}}`}, 1},
		{"missing has next page", []string{page(`[]`, `{"endCursor":"cursor-1"}`)}, 1},
		{"missing author", []string{page(`[{`+strings.Replace(validScalars, `,"author":{"login":"octocat"}`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing author login", []string{page(`[{`+strings.Replace(validScalars, `"author":{"login":"octocat"}`, `"author":{}`, 1)+`}]`, validPageInfo)}, 1},
		{"missing labels", []string{page(`[{`+strings.Replace(validScalars, `,`+validLabels, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing label nodes", []string{page(`[{`+strings.Replace(validScalars, validLabels, `"labels":{"pageInfo":{"hasNextPage":false}}`, 1)+`}]`, validPageInfo)}, 1},
		{"missing label page info", []string{page(`[{`+strings.Replace(validScalars, validLabels, `"labels":{"nodes":[{"name":"bug"}]}`, 1)+`}]`, validPageInfo)}, 1},
		{"missing label has next page", []string{page(`[{`+strings.Replace(validScalars, validLabels, `"labels":{"nodes":[{"name":"bug"}],"pageInfo":{}}`, 1)+`}]`, validPageInfo)}, 1},
		{"missing label name", []string{page(`[{`+strings.Replace(validScalars, `{"name":"bug"}`, `{}`, 1)+`}]`, validPageInfo)}, 1},
		{"paginated labels", []string{page(`[{`+strings.Replace(validScalars, `"hasNextPage":false`, `"hasNextPage":true`, 1)+`}]`, validPageInfo)}, 1},
		{"missing number", []string{page(`[{`+strings.Replace(validScalars, `"number":1,`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing title", []string{page(`[{`+strings.Replace(validScalars, `"title":"Issue 1",`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing body", []string{page(`[{`+strings.Replace(validScalars, `"body":"Body",`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing state", []string{page(`[{`+strings.Replace(validScalars, `"state":"OPEN",`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing created at", []string{page(`[{`+strings.Replace(validScalars, `"createdAt":"2026-09-01T00:00:00Z",`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing updated at", []string{page(`[{`+strings.Replace(validScalars, `"updatedAt":"2026-09-02T00:00:00Z",`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"missing URL", []string{page(`[{`+strings.Replace(validScalars, `,"url":"https://github.example/owner/repo/issues/1"`, ``, 1)+`}]`, validPageInfo)}, 1},
		{"empty next cursor", []string{page(`[`+validIssue+`]`, `{"hasNextPage":true,"endCursor":""}`)}, 2},
		{"missing next cursor", []string{page(`[`+validIssue+`]`, `{"hasNextPage":true}`)}, 2},
		{"empty nodes with next page", []string{page(`[]`, `{"hasNextPage":true,"endCursor":"cursor-1"}`)}, 2},
		{"too many nodes", []string{page(`[`+validIssue+`,`+validIssue+`]`, validPageInfo)}, 1},
		{"repeated cursor", []string{page(`[`+validIssue+`]`, `{"hasNextPage":true,"endCursor":"same"}`), page(`[`+validIssue+`]`, `{"hasNextPage":true,"endCursor":"same"}`)}, 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{}
			for _, result := range test.results {
				caller.results = append(caller.results, runner.Result{Stdout: []byte(result)})
			}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL, test.limit))
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v, want rejection", response, err)
			}
		})
	}
}

// Regression: paginated reads share one exact 8 MiB raw-output budget even
// when their normalized protobuf is much smaller.
func TestIssueListAggregateBudget(t *testing.T) {
	firstPage := graphQLIssuePage([]map[string]any{graphQLIssueFixture(1)}, true, "cursor-1")
	secondPage := graphQLIssuePage([]map[string]any{graphQLIssueFixture(2)}, false, "cursor-2")
	firstSize := 4 * (1 << 20)
	firstPage = append(firstPage, []byte(strings.Repeat(" ", firstSize-len(firstPage)))...)
	for _, test := range []struct {
		name string
		size int
		want error
	}{
		{"exact limit", 8 * (1 << 20), nil},
		{"one byte over", 8*(1<<20) + 1, runner.ErrOutputLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			last := append(append([]byte{}, secondPage...), []byte(strings.Repeat(" ", test.size-firstSize-len(secondPage)))...)
			caller := &fakeCaller{results: []runner.Result{{Stdout: firstPage}, {Stdout: last}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL, 2))
			if test.want == nil {
				if err != nil || len(response.GetIssueList().GetIssues()) != 2 {
					t.Fatalf("Execute() = %#v, %v", response, err)
				}
				if body := issueListCommandBody(t, caller.commands[0]); body.Variables["states"] != nil {
					t.Fatalf("all-state variable = %#v, want null", body.Variables["states"])
				}
				if caller.commands[1].StdoutLimit != test.size-firstSize {
					t.Fatalf("second stdout limit = %d, want %d", caller.commands[1].StdoutLimit, test.size-firstSize)
				}
				return
			}
			if response != nil || !errors.Is(err, test.want) {
				t.Fatalf("Execute() = %#v, %v, want %v", response, err, test.want)
			}
		})
	}
}

// Regression: cancellation between GraphQL pages must prevent another
// provider invocation.
func TestIssueListStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	caller := &scriptedIssueListCaller{call: func(_ context.Context, command runner.Command, index int) (runner.Result, error) {
		if index > 0 {
			return runner.Result{}, errors.New("unexpected second call")
		}
		if states := issueListCommandBody(t, command).Variables["states"]; !reflect.DeepEqual(states, []any{"CLOSED"}) {
			t.Fatalf("closed-state variable = %#v", states)
		}
		cancel()
		return runner.Result{Stdout: graphQLIssuePage([]map[string]any{graphQLIssueFixture(1)}, true, "cursor-1")}, nil
	}}

	response, err := testAdapter(t, caller).Execute(ctx, repository(), issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_CLOSED, 2))
	if response != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() = %#v, %v, want context canceled", response, err)
	}
	if len(caller.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(caller.commands))
	}
}

func issueListRequest(state repowolfv1.GitHubIssueState, limit uint64) *repowolfv1.GitHubRequest {
	return request(&repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: state, Limit: limit}})
}

func graphQLIssueFixture(number int) map[string]any {
	return map[string]any{
		"number": number, "title": fmt.Sprintf("Issue %d", number), "body": "Body", "state": "OPEN",
		"labels": map[string]any{"nodes": []map[string]any{{"name": "bug"}}, "pageInfo": map[string]any{"hasNextPage": false}},
		"author": map[string]any{"login": "octocat"}, "createdAt": "2026-09-01T00:00:00Z", "updatedAt": "2026-09-02T00:00:00Z",
		"url": fmt.Sprintf("https://github.example/owner/repo/issues/%d", number),
	}
}

func graphQLIssuePage(nodes []map[string]any, hasNext bool, cursor string) []byte {
	return mustJSON(map[string]any{"data": map[string]any{"repository": map[string]any{"issues": map[string]any{
		"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cursor},
	}}}})
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func issueListCommandBody(t *testing.T, command runner.Command) struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
} {
	t.Helper()
	if command.Args[len(command.Args)-1] != "graphql" || command.Args[4] != "POST" || strings.Contains(string(command.Stdin), "/search/issues") {
		t.Fatalf("command = %#v", command)
	}
	var body struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(command.Stdin, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Variables) != 5 {
		t.Fatalf("variables = %#v, want only five typed variables", body.Variables)
	}
	return body
}
