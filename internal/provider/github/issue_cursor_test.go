package github

import (
	"context"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func TestIssueListTerminalCursorValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		cursor any
		valid  bool
	}{
		{"repeated terminal cursor", "cursor-1", false},
		{"new terminal cursor", "cursor-2", true},
		{"null terminal cursor", nil, true},
		{"empty terminal cursor", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			terminal := mustJSON(map[string]any{"data": map[string]any{"repository": map[string]any{"issues": map[string]any{
				"nodes": []map[string]any{graphQLIssueFixture(2)}, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": test.cursor},
			}}}})
			caller := &fakeCaller{results: []runner.Result{
				{Stdout: graphQLIssuePage([]map[string]any{graphQLIssueFixture(1)}, true, "cursor-1")}, {Stdout: terminal},
			}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN, 2))
			if test.valid {
				if err != nil || len(response.GetIssueList().GetIssues()) != 2 {
					t.Fatalf("Execute() = %v, %v", response, err)
				}
			} else if response != nil || err == nil {
				t.Fatal("repeated terminal cursor accepted")
			}
			if len(caller.commands) != 2 {
				t.Fatalf("calls = %d", len(caller.commands))
			}
		})
	}
}
