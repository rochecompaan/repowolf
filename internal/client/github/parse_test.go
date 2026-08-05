package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestParseApprovedOperations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want any
	}{
		{"repository view", []string{"repo", "view"}, (*repowolfv1.GitHubRequest_RepositoryView)(nil)},
		{"issue list", []string{"issue", "list", "--state", "all", "--limit", "100"}, (*repowolfv1.GitHubRequest_IssueList)(nil)},
		{"issue view", []string{"issue", "view", "12"}, (*repowolfv1.GitHubRequest_IssueView)(nil)},
		{"issue create", []string{"issue", "create", "--title", "title", "--body", "body"}, (*repowolfv1.GitHubRequest_IssueCreate)(nil)},
		{"issue edit", []string{"issue", "edit", "12", "--title", "new"}, (*repowolfv1.GitHubRequest_IssueEdit)(nil)},
		{"issue comment", []string{"issue", "comment", "12", "--body", "hello"}, (*repowolfv1.GitHubRequest_IssueComment)(nil)},
		{"issue close", []string{"issue", "close", "12"}, (*repowolfv1.GitHubRequest_IssueClose)(nil)},
		{"issue reopen", []string{"issue", "reopen", "12"}, (*repowolfv1.GitHubRequest_IssueReopen)(nil)},
		{"pull list", []string{"pr", "list", "--state", "closed", "--base", "main", "--head", "topic", "--limit", "2"}, (*repowolfv1.GitHubRequest_PullList)(nil)},
		{"pull view", []string{"pr", "view", "2"}, (*repowolfv1.GitHubRequest_PullView)(nil)},
		{"pull create", []string{"pr", "create", "--head", "topic", "--base", "main", "--title", "title", "--draft"}, (*repowolfv1.GitHubRequest_PullCreate)(nil)},
		{"pull edit", []string{"pr", "edit", "9", "--base", "release", "--title", "new"}, (*repowolfv1.GitHubRequest_PullEdit)(nil)},
		{"pull comment", []string{"pr", "comment", "9", "--body", "ok"}, (*repowolfv1.GitHubRequest_PullComment)(nil)},
		{"pull close", []string{"pr", "close", "9"}, (*repowolfv1.GitHubRequest_PullClose)(nil)},
		{"pull reopen", []string{"pr", "reopen", "9"}, (*repowolfv1.GitHubRequest_PullReopen)(nil)},
		{"pull ready", []string{"pr", "ready", "9"}, (*repowolfv1.GitHubRequest_PullReady)(nil)},
		{"pull checks", []string{"pr", "checks", "9"}, (*repowolfv1.GitHubRequest_PullChecks)(nil)},
		{"run list", []string{"run", "list", "--branch", "topic", "--status", "success"}, (*repowolfv1.GitHubRequest_RunList)(nil)},
		{"run view", []string{"run", "view", "123"}, (*repowolfv1.GitHubRequest_RunView)(nil)},
		{"status", []string{"status", "get", "0123456789012345678901234567890123456789"}, (*repowolfv1.GitHubRequest_StatusView)(nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append(append([]string{}, test.args...), "--repo", "owner/repo")
			request, err := Parse(args, t.TempDir())
			if err != nil {
				t.Fatalf("Parse() = %v", err)
			}
			if reflect.TypeOf(request.Operation) != reflect.TypeOf(test.want) {
				t.Fatalf("operation = %T, want %T", request.Operation, test.want)
			}
			repository := request.GetContext().GetRepository()
			if repository.GetHost() != "github.com" || repository.GetOwner() != "owner" || repository.GetName() != "repo" {
				t.Fatalf("repository = %#v", repository)
			}
		})
	}
}

func TestParsePreservesTypedFieldsAndJSONSelection(t *testing.T) {
	parsed, err := parseArgs([]string{"issue", "list", "--state", "closed", "--limit", "7", "--json", "number,title", "--repo", "owner/repo"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	operation := parsed.request.GetIssueList()
	if operation.GetState() != repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_CLOSED || operation.GetLimit() != 7 {
		t.Fatalf("operation = %#v", operation)
	}
	if !reflect.DeepEqual(parsed.fields, []string{"number", "title"}) {
		t.Fatalf("fields = %#v", parsed.fields)
	}
}

func TestParseUsesGitRemoteOnlyAsRepositoryHint(t *testing.T) {
	cwd := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", cwd).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	config := "[remote \"origin\"]\n\turl = git@github.example:team/project.git\n"
	if err := os.WriteFile(filepath.Join(cwd, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // Repository hints do not execute local tools.
	request, err := Parse([]string{"repo", "view"}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	got := request.GetContext().GetRepository()
	if got.GetHost() != "github.example" || got.GetOwner() != "team" || got.GetName() != "project" {
		t.Fatalf("repository = %#v", got)
	}
}

func TestParseRejectsUnsupportedAndHostileInput(t *testing.T) {
	oid := "0123456789012345678901234567890123456789"
	tests := [][]string{
		nil, {"auth", "status"}, {"api", "/user"}, {"alias", "list"}, {"extension", "list"},
		{"repo", "list"}, {"repo", "create"}, {"repo", "view", "other"},
		{"issue", "view", "https://github.com/owner/repo/issues/1"}, {"issue", "view", "1", "tail"},
		{"issue", "list", "--limit", "0"}, {"issue", "list", "--limit", "101"}, {"issue", "list", "--limit"},
		{"issue", "list", "--state", "merged"}, {"issue", "list", "--state", "open", "--state", "closed"},
		{"issue", "create"}, {"issue", "create", "--title", ""}, {"issue", "create", "--title", "x", "--body-file", "secret"},
		{"issue", "create", "--title=x"}, {"issue", "edit", "1"}, {"issue", "edit", "1", "--state", "closed"},
		{"issue", "edit", "1", "--web"}, {"issue", "comment", "1"}, {"issue", "comment", "1", "--body-file", "-"},
		{"pr", "merge", "1"}, {"pr", "checkout", "1"}, {"pr", "create", "--title", "x", "--base", "main"},
		{"pr", "create", "--title", "x", "--base", "main", "--head", "topic", "--draft", "true"},
		{"pr", "edit", "1"}, {"pr", "edit", "1", "--state", "closed"}, {"pr", "diff", "1"},
		{"pr", "checks", "1", "--watch"}, {"workflow", "run"}, {"workflow", "disable"},
		{"run", "rerun", "1"}, {"run", "cancel", "1"}, {"run", "view", "1", "--log"},
		{"run", "list", "--limit", "101"}, {"run", "view", "18446744073709551616"},
		{"status", "get", "main"}, {"status", "get", oid, "--header", "x"},
		{"issue", "view", "1", "--repo", "one/repo", "--repo", "two/repo"},
		{"issue", "view", "1", "--repo", "https://github.com/owner/repo"},
		{"issue", "view", "1", "--json", "number,secret"}, {"issue", "view", "1", "--json", "number,number"},
		{"issue", "view", "1", "--jq", ".title"}, {"issue", "view", "1", "--template", "{{.title}}"},
		{"issue", "view", "1", "--web"}, {"issue", "create", "--editor"},
		{"issue", "view", "-1"}, {"issue", "view", "+1"}, {"issue", "view", "01"}, {"issue", "view", "1", "-R", "owner/repo"},
	}
	for _, args := range tests {
		t.Run(testName(args), func(t *testing.T) {
			withRepo := append(append([]string{}, args...), "--repo", "owner/repo")
			if _, err := Parse(withRepo, t.TempDir()); err == nil {
				t.Fatalf("accepted %#v", args)
			}
		})
	}
}

func TestParseRequiresRepositoryHint(t *testing.T) {
	if _, err := Parse([]string{"repo", "view"}, t.TempDir()); err == nil {
		t.Fatal("accepted command without repository hint")
	}
}

func testName(args []string) string {
	if len(args) == 0 {
		return "empty"
	}
	return args[0] + "-" + string(rune(len(args)))
}
