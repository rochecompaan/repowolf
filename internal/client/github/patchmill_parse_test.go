package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func newGitHubRepository(t *testing.T) string {
	t.Helper()
	cwd := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", cwd).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	config := "[remote \"origin\"]\n\turl = git@github.com:owner/repo.git\n"
	if err := os.WriteFile(filepath.Join(cwd, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestParsePatchmillIdentityCommands(t *testing.T) {
	cwd := newGitHubRepository(t)
	for _, test := range []struct {
		name string
		args []string
		kind operationKind
	}{
		{"auth status", []string{"auth", "status"}, operationAuthStatus},
		{"current login", []string{"api", "user", "--jq", ".login"}, operationCurrentUserLogin},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseArgs(test.args, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.kind != test.kind || parsed.request.GetCurrentUser() == nil {
				t.Fatalf("unexpected parse: %#v", parsed)
			}
		})
	}

	for _, args := range [][]string{
		{"api", "repos/x"},
		{"api", "user", "--jq", ".name"},
		{"auth", "status", "--hostname", "github.com"},
	} {
		if _, err := parseArgs(args, cwd); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}

func TestParsePatchmillRepositoryView(t *testing.T) {
	cwd := newGitHubRepository(t)
	parsed, err := parseArgs([]string{"repo", "view", "owner/name", "--json", "name,url,sshUrl"}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.request.GetContext().GetRepository().GetOwner() != "owner" || parsed.request.GetContext().GetRepository().GetName() != "name" {
		t.Fatalf("repository = %#v", parsed.request.GetContext().GetRepository())
	}
	if !reflect.DeepEqual(parsed.fields, []string{"name", "url", "sshUrl"}) {
		t.Fatalf("fields = %#v", parsed.fields)
	}
	for _, args := range [][]string{
		{"repo", "view", ""},
		{"repo", "view", "owner/name", "extra"},
		{"repo", "view", "owner/name", "--repo", "other/repo"},
	} {
		if _, err := parseArgs(args, cwd); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}

func TestParsePatchmillCommands(t *testing.T) {
	cwd := newGitHubRepository(t)
	for _, test := range []struct {
		args       []string
		kind       operationKind
		fields     []string
		limit      uint64
		repository string
		labels     []string
		comments   bool
	}{
		{[]string{"issue", "list", "--state", "open", "--limit", "1001", "--json", "number,title,body,state,labels,author,createdAt,updatedAt,url"}, operationIssueList, []string{"number", "title", "body", "state", "labels", "author", "createdAt", "updatedAt", "url"}, 1001, "owner/repo", nil, false},
		{[]string{"issue", "view", "18", "--json", "number,title,body,state,labels,author,createdAt,updatedAt,url,comments"}, operationIssueView, []string{"number", "title", "body", "state", "labels", "author", "createdAt", "updatedAt", "url", "comments"}, 0, "owner/repo", nil, true},
		{[]string{"label", "list", "--limit", "1000", "--json", "name"}, operationLabelList, []string{"name"}, 1000, "owner/repo", nil, false},
		{[]string{"label", "list", "--repo", "owner/name", "--limit", "1000", "--json", "name"}, operationLabelList, []string{"name"}, 1000, "owner/name", nil, false},
		{[]string{"label", "create", "patchmill:ready", "--repo", "owner/name", "--color", "1a2B3c", "--description", "Ready for work"}, operationLabelCreate, nil, 0, "owner/name", nil, false},
		{[]string{"issue", "edit", "18", "--add-label", "patchmill:ready,help wanted", "--remove-label", "patchmill:queued"}, operationIssueLabelChange, nil, 0, "owner/repo", []string{"patchmill:ready", "help wanted", "patchmill:queued"}, false},
		{[]string{"issue", "create", "--repo", "owner/name", "--title", "Title", "--body", "Body", "--label", "patchmill:ready,bug"}, operationIssueCreate, nil, 0, "owner/name", []string{"patchmill:ready", "bug"}, false},
		{[]string{"issue", "comment", "18", "--body", "Comment"}, operationIssueComment, nil, 0, "owner/repo", nil, false},
		{[]string{"pr", "view", "18", "--json", "body,url"}, operationPullView, []string{"body", "url"}, 0, "owner/repo", nil, false},
	} {
		t.Run(strings.Join(test.args[:2], " "), func(t *testing.T) {
			parsed, err := parseArgs(test.args, cwd)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.kind != test.kind {
				t.Fatalf("kind = %v, want %v", parsed.kind, test.kind)
			}
			if !reflect.DeepEqual(parsed.fields, test.fields) {
				t.Fatalf("fields = %#v, want %#v", parsed.fields, test.fields)
			}
			repository := parsed.request.GetContext().GetRepository()
			if repository.GetOwner()+"/"+repository.GetName() != test.repository {
				t.Fatalf("repository = %s/%s, want %s", repository.GetOwner(), repository.GetName(), test.repository)
			}
			switch test.kind {
			case operationIssueList:
				if parsed.request.GetIssueList().GetLimit() != test.limit {
					t.Fatalf("limit = %d, want %d", parsed.request.GetIssueList().GetLimit(), test.limit)
				}
			case operationLabelList:
				if parsed.request.GetLabelList().GetLimit() != test.limit {
					t.Fatalf("limit = %d, want %d", parsed.request.GetLabelList().GetLimit(), test.limit)
				}
			case operationIssueView:
				if parsed.request.GetIssueView().GetIncludeComments() != test.comments {
					t.Fatalf("include comments = %t, want %t", parsed.request.GetIssueView().GetIncludeComments(), test.comments)
				}
			case operationIssueCreate:
				if !reflect.DeepEqual(parsed.request.GetIssueCreate().GetLabels(), test.labels) {
					t.Fatalf("labels = %#v, want %#v", parsed.request.GetIssueCreate().GetLabels(), test.labels)
				}
			case operationIssueLabelChange:
				change := parsed.request.GetIssueLabelChange()
				if !reflect.DeepEqual(append(change.GetAddLabels(), change.GetRemoveLabels()...), test.labels) {
					t.Fatalf("labels = %#v, want %#v", append(change.GetAddLabels(), change.GetRemoveLabels()...), test.labels)
				}
			}
		})
	}
}

func TestRejectPatchmillNearMisses(t *testing.T) {
	cwd := newGitHubRepository(t)
	tooLongName := strings.Repeat("界", 51)
	tooLongDescription := strings.Repeat("界", 101)
	for _, args := range [][]string{
		{"issue", "list", "--limit", "1002"}, {"label", "list", "--limit", "1001", "--json", "name"},
		{"issue", "list", "--json", "number,unknown"}, {"issue", "list", "--json", "number,number"}, {"issue", "create", "--title", "x", "--json", "comments"},
		{"issue", "create", "--title", "x", "--label", "a,,b"}, {"issue", "create", "--title", "x", "--label", ",a"}, {"issue", "create", "--title", "x", "--label", "a,"},
		{"issue", "create", "--title", "x", "--label", "a, a"}, {"issue", "edit", "1", "--add-label", "a", "--remove-label", "a"},
		{"issue", "edit", "1", "--title", "x", "--add-label", "a"}, {"issue", "edit", "1", "--body", "x", "--remove-label", "a"},
		{"label", "create", "--color", "abcdef", "--description", "x"}, {"label", "create", "name", "--description", "x"}, {"label", "create", "name", "--color", "abcdef"},
		{"label", "create", "name", "--color", "#abcdef", "--description", "x"}, {"label", "create", "name", "--color", "abcde", "--description", "x"}, {"label", "create", "name", "--color", "abcdef0", "--description", "x"}, {"label", "create", "name", "--color", "abcdeg", "--description", "x"},
		{"label", "create", tooLongName, "--color", "abcdef", "--description", "x"}, {"label", "create", "name", "--color", "abcdef", "--description", tooLongDescription},
		{"label", "create", "name", "extra", "--color", "abcdef", "--description", "x"}, {"issue", "view", "1", "extra"},
		{"repo", "create", "owner/name"}, {"repo", "delete", "owner/name"}, {"repo", "clone", "owner/name"},
		{"api", "graphql"}, {"api", "user", "--template", "{{.login}}"}, {"api", "user", "--jq", ".name"}, {"api", "user", "--paginate"}, {"alias", "set"}, {"extension", "install", "x"}, {"issue", "list", "--jq", ".[]"}, {"issue", "list", "--template", "{{.}}"}, {"issue", "list", "--shell"},
		{"label", "list", "--limit", "1"}, {"label", "list", "--limit", "1", "--json", "name,title"}, {"label", "list", "--limit", "1", "--json", "name,name"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := parseArgs(args, cwd); err == nil {
				t.Fatalf("accepted %#v", args)
			}
		})
	}
}

func TestPatchmillJSONRendering(t *testing.T) {
	issue := &repowolfv1.GitHubIssueRecord{Author: "octocat", Labels: []string{"bug"}, Comments: []*repowolfv1.GitHubCommentRecord{{Author: "reviewer", Body: "done", CreatedAt: "2026-09-07T00:00:00Z", Url: "ignored", UpdatedAt: "ignored"}}}
	for _, test := range []struct {
		name     string
		parsed   command
		response *repowolfv1.GitHubResponse
		want     string
	}{
		{"issue list objects preserve selected order", command{kind: operationIssueList, fields: []string{"labels", "author"}}, &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueList{IssueList: &repowolfv1.GitHubIssueListResult{Issues: []*repowolfv1.GitHubIssueRecord{issue}}}}, "[{\"labels\":[{\"name\":\"bug\"}],\"author\":{\"login\":\"octocat\"}}]\n"},
		{"issue view comments", command{kind: operationIssueView, fields: []string{"comments"}}, &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueView{IssueView: &repowolfv1.GitHubIssueViewResult{Issue: issue}}}, "{\"comments\":[{\"author\":{\"login\":\"reviewer\"},\"body\":\"done\",\"createdAt\":\"2026-09-07T00:00:00Z\"}]}\n"},
		{"label list", command{kind: operationLabelList, fields: []string{"name"}}, &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_LabelList{LabelList: &repowolfv1.GitHubLabelListResult{Labels: []*repowolfv1.GitHubLabelRecord{{Name: "bug"}, {Name: "patchmill:ready"}}}}}, "[{\"name\":\"bug\"},{\"name\":\"patchmill:ready\"}]\n"},
		{"label create native output", command{kind: operationLabelCreate}, &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateResult{Label: &repowolfv1.GitHubLabelRecord{Name: "bug"}}}}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := render(test.parsed, test.response)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("render() = %q, want %q", got, test.want)
			}
		})
	}
}
