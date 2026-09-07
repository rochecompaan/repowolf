package github

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
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
		{"repo", "view", "owner/name", "extra"},
		{"repo", "view", "owner/name", "--repo", "other/repo"},
	} {
		if _, err := parseArgs(args, cwd); err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
}
