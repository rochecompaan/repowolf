package github

import (
	"context"
	"fmt"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

// Malformed provider records must not become typed issue-list responses.
func TestIssueListRejectsMalformedGraphQLRecords(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"zero number", func(v map[string]any) { v["number"] = 0 }},
		{"invalid state", func(v map[string]any) { v["state"] = "open" }},
		{"title too long", func(v map[string]any) { v["title"] = strings.Repeat("x", maximumTitleBytes+1) }},
		{"body too long", func(v map[string]any) { v["body"] = strings.Repeat("x", maximumBodyBytes+1) }},
		{"invalid login", func(v map[string]any) { v["author"] = map[string]any{"login": "bad login"} }},
		{"login too long", func(v map[string]any) { v["author"] = map[string]any{"login": strings.Repeat("x", maximumNameBytes+1)} }},
		{"too many labels", func(v map[string]any) { v["labels"].(map[string]any)["nodes"] = graphQLLabelFixtures(101) }},
	}
	for _, field := range []string{"title", "state", "createdAt", "updatedAt", "url", "author", "label"} {
		tests = append(tests, struct {
			name   string
			mutate func(map[string]any)
		}{"empty " + field, func(v map[string]any) { setGraphQLString(v, field, "") }})
	}
	for _, field := range []string{"title", "body", "state", "createdAt", "updatedAt", "url", "author", "label"} {
		tests = append(tests, struct {
			name   string
			mutate func(map[string]any)
		}{"NUL " + field, func(v map[string]any) { setGraphQLString(v, field, "bad\x00text") }})
	}
	tests = append(tests, struct {
		name   string
		mutate func(map[string]any)
	}{"label too long", func(v map[string]any) { setGraphQLString(v, "label", strings.Repeat("x", maximumNameBytes+1)) }})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := graphQLIssueFixture(1)
			test.mutate(value)
			caller := &fakeCaller{results: []runner.Result{{Stdout: graphQLIssuePage([]map[string]any{value}, false, "last")}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueListRequest(repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN, 1))
			if response != nil || err == nil {
				t.Fatalf("malformed %s crossed typed boundary: error=%v response present=%v", test.name, err, response != nil)
			}
		})
	}
	// Inject invalid bytes after JSON marshaling (encoding/json otherwise repairs them).
	for _, field := range []string{"title", "body", "state", "createdAt", "updatedAt", "url", "author", "label"} {
		t.Run("UTF-8 "+field, func(t *testing.T) {
			value := graphQLIssueFixture(1)
			setGraphQLString(value, field, "invalid-marker")
			raw := graphQLIssuePage([]map[string]any{value}, false, "last")
			raw = []byte(strings.Replace(string(raw), "invalid-marker", "\xff", 1))
			if _, err := normalizeIssueGraphQLPage(raw, 1); err == nil {
				t.Fatal("accepted invalid UTF-8")
			}
		})
	}
}

func TestIssueListGraphQLRecordBoundaries(t *testing.T) {
	value := graphQLIssueFixture(1)
	value["title"] = strings.Repeat("é", maximumTitleBytes/2)
	value["body"] = strings.Repeat("é", maximumBodyBytes/2)
	value["labels"].(map[string]any)["nodes"] = graphQLLabelFixtures(100)
	value["labels"].(map[string]any)["nodes"].([]map[string]any)[0]["name"] = strings.Repeat("x", maximumNameBytes)
	value["url"] = "https://github.example/" + strings.Repeat("x", 256)
	for _, body := range []string{value["body"].(string), ""} {
		value["body"] = body
		page, err := normalizeIssueGraphQLPage(graphQLIssuePage([]map[string]any{value}, false, "last"), 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.records) != 1 || page.records[0].GetBody() != body || len(page.records[0].GetLabels()) != 100 {
			t.Fatal("lost valid boundary data")
		}
	}
}

func setGraphQLString(value map[string]any, field, text string) {
	switch field {
	case "author":
		value["author"] = map[string]any{"login": text}
	case "label":
		value["labels"].(map[string]any)["nodes"] = []map[string]any{{"name": text}}
	default:
		value[field] = text
	}
}

func graphQLLabelFixtures(count int) []map[string]any {
	values := make([]map[string]any, count)
	for index := range values {
		values[index] = map[string]any{"name": fmt.Sprintf("label-%d", index)}
	}
	return values
}

// Issue authors are GraphQL Actors, including bots, not only human accounts.
func TestIssueListGraphQLActorLogins(t *testing.T) {
	for _, test := range []struct {
		login string
		valid bool
	}{
		{"octocat", true}, {"dependabot[bot]", true}, {strings.Repeat("x", maximumNameBytes), true},
		{"bad\u2003actor", false}, {"bad\u0085actor", false}, {"bad\x1bactor", false},
	} {
		t.Run(test.login, func(t *testing.T) {
			value := graphQLIssueFixture(1)
			setGraphQLString(value, "author", test.login)
			page, err := normalizeIssueGraphQLPage(graphQLIssuePage([]map[string]any{value}, false, "last"), 1)
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				if page.records[0].GetAuthor() != test.login {
					t.Fatal("actor login changed")
				}
			} else if err == nil {
				t.Fatal("accepted invalid actor")
			}
		})
	}
}
