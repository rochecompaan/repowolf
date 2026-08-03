package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func TestTypedTextAndNameLimitsUseUTF8Bytes(t *testing.T) {
	pullState := repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_OPEN
	tests := []struct {
		name       string
		limit      int
		field      string
		queryField string
		build      func(string) *repowolfv1.GitHubRequest
		results    []runner.Result
	}{
		{"issue create title", 256, "title", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: value}})
		}, boundaryIssueResults(false)},
		{"issue edit title", 256, "title", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1, Title: &value}})
		}, boundaryIssueResults(true)},
		{"pull create title", 256, "title", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: value, Head: "topic", Base: "main"}})
		}, boundaryPullResults(false)},
		{"pull edit title", 256, "title", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullEdit{PullEdit: &repowolfv1.GitHubPullEditRequest{Number: 1, Title: &value}})
		}, boundaryPullResults(false)},

		{"issue create body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: "title", Body: &value}})
		}, boundaryIssueResults(false)},
		{"issue edit body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditRequest{Number: 1, Body: &value}})
		}, boundaryIssueResults(true)},
		{"issue comment body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: value}})
		}, boundaryCommentResults(false)},
		{"pull create body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: "title", Head: "topic", Base: "main", Body: &value}})
		}, boundaryPullResults(false)},
		{"pull edit body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullEdit{PullEdit: &repowolfv1.GitHubPullEditRequest{Number: 1, Body: &value}})
		}, boundaryPullResults(false)},
		{"pull comment body", 65_536, "body", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullComment{PullComment: &repowolfv1.GitHubPullCommentRequest{Number: 1, Body: value}})
		}, boundaryCommentResults(true)},

		{"pull list base", 255, "", "base", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullList{PullList: &repowolfv1.GitHubPullListRequest{State: pullState, Limit: 1, Base: &value}})
		}, []runner.Result{{Stdout: []byte(`[]`)}}},
		{"pull list head", 255, "", "head", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullList{PullList: &repowolfv1.GitHubPullListRequest{State: pullState, Limit: 1, Head: &value}})
		}, []runner.Result{{Stdout: []byte(`[]`)}}},
		{"pull create head", 255, "head", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: "title", Head: value, Base: "main"}})
		}, boundaryPullResults(false)},
		{"pull create base", 255, "base", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateRequest{Title: "title", Head: "topic", Base: value}})
		}, boundaryPullResults(false)},
		{"pull edit base", 255, "base", "", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_PullEdit{PullEdit: &repowolfv1.GitHubPullEditRequest{Number: 1, Base: &value}})
		}, boundaryPullResults(false)},
		{"run list branch", 255, "", "branch", func(value string) *repowolfv1.GitHubRequest {
			return request(&repowolfv1.GitHubRequest_RunList{RunList: &repowolfv1.GitHubRunListRequest{Limit: 1, Branch: &value}})
		}, []runner.Result{{Stdout: []byte(`{"workflow_runs":[]}`)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exact := utf8Boundary(test.limit)
			caller := &fakeCaller{results: append([]runner.Result(nil), test.results...)}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), test.build(exact))
			if err != nil || response == nil {
				t.Fatalf("exact Execute() = %#v, %v", response, err)
			}
			command := caller.commands[len(caller.commands)-1]
			if test.queryField != "" {
				endpoint, err := url.ParseRequestURI(command.Args[len(command.Args)-1])
				if err != nil {
					t.Fatal(err)
				}
				if got := endpoint.Query().Get(test.queryField); got != exact {
					t.Fatalf("query has %d bytes, want %d", len(got), test.limit)
				}
			} else {
				var input map[string]any
				if err := json.Unmarshal(command.Stdin, &input); err != nil {
					t.Fatal(err)
				}
				if got, ok := input[test.field].(string); !ok || got != exact {
					t.Fatalf("stdin field = %#v, want exact %d-byte value", input[test.field], test.limit)
				}
			}

			overCaller := &fakeCaller{}
			response, err = testAdapter(t, overCaller).Execute(context.Background(), repository(), test.build(utf8Boundary(test.limit+1)))
			if response != nil || !errors.Is(err, ErrInvalidRequest) || len(overCaller.commands) != 0 {
				t.Fatalf("one-byte-over Execute() = %#v, %v, calls = %d", response, err, len(overCaller.commands))
			}
		})
	}
}

func utf8Boundary(size int) string {
	return strings.Repeat("a", size-2) + "é"
}

func boundaryIssueResults(preflight bool) []runner.Result {
	results := []runner.Result{{Stdout: []byte(`{"number":1,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`)}}
	if preflight {
		results = append([]runner.Result{{Stdout: []byte(`{"number":1}`)}}, results...)
	}
	return results
}

func boundaryPullResults(_ bool) []runner.Result {
	return []runner.Result{{Stdout: []byte(`{"number":1,"title":"title","body":"body","state":"open","draft":false,"user":{"login":"me"},"head":{"ref":"topic","sha":"0123456789012345678901234567890123456789"},"base":{"ref":"main"},"html_url":"https://safe.example/1","created_at":"c","updated_at":"u","mergeable_state":"clean"}`)}}
}

func boundaryCommentResults(pull bool) []runner.Result {
	preflight := `{"number":1}`
	if pull {
		preflight = `{"head":{}}`
	}
	return []runner.Result{{Stdout: []byte(preflight)}, {Stdout: []byte(`{"id":1,"user":{"login":"me"},"body":"body","html_url":"https://safe.example/c","created_at":"c","updated_at":"u"}`)}}
}
