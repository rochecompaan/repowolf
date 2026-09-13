package gitea

import (
	"strings"
	"testing"
)

func TestParseIssueMutation(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		branch string
		format outputFormat
	}{
		{"create", []string{"issues", "create", "--repo", "Owner/Repo", "--title", "title"}, "create", outputSimple},
		{"create alias", []string{"issues", "c", "-r", "Owner/Repo", "-t", "title", "-d", "", "-a", "alice,bob", "-L", "bug,urgent", "-o", "json"}, "create", outputJSON},
		{"comment", []string{"comments", "add", "7", "body", "--repo", "Owner/Repo"}, "comment", outputSimple},
		{"comment description", []string{"comments", "a", "7", "--description", "body", "-r", "Owner/Repo"}, "comment", outputSimple},
		{"comment direct plural", []string{"comments", "7", "body", "-r", "Owner/Repo"}, "comment", outputSimple},
		{"comment singular", []string{"comment", "7", "body", "-r", "Owner/Repo"}, "comment", outputSimple},
		{"comment c", []string{"c", "7", "body", "-r", "Owner/Repo"}, "comment", outputSimple},
		{"close", []string{"issues", "close", "7", "-r", "Owner/Repo", "-o", "table"}, "close", outputTable},
		{"reopen", []string{"issues", "open", "7", "-r", "Owner/Repo", "-o", "json"}, "reopen", outputJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, err := Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if !cmd.mutation || cmd.format != test.format {
				t.Fatalf("command=%#v", cmd)
			}
			switch test.branch {
			case "create":
				if cmd.request.GetIssueCreate() == nil {
					t.Fatal("wrong branch")
				}
			case "comment":
				if cmd.request.GetIssueComment() == nil {
					t.Fatal("wrong branch")
				}
			case "close":
				if cmd.request.GetIssueClose() == nil {
					t.Fatal("wrong branch")
				}
			case "reopen":
				if cmd.request.GetIssueReopen() == nil {
					t.Fatal("wrong branch")
				}
			}
		})
	}
}

func TestIssueMutationRejectsSingularIssueWriteAliases(t *testing.T) {
	for _, prefix := range []string{"issue", "i"} {
		for _, operation := range []string{"create", "c", "close", "reopen", "open"} {
			args := []string{prefix, operation, "7", "-r", "Owner/Repo"}
			if operation == "create" || operation == "c" {
				args = []string{prefix, operation, "-r", "Owner/Repo", "-t", "title"}
			}
			if _, err := Parse(args); err == nil {
				t.Fatalf("accepted undocumented mutation alias %q", args)
			}
		}
	}
}

func TestIssueCommentRejectsUndocumentedAliases(t *testing.T) {
	for _, args := range [][]string{
		{"comment", "add", "7", "body", "-r", "Owner/Repo"},
		{"comment", "a", "7", "body", "-r", "Owner/Repo"},
		{"c", "add", "7", "body", "-r", "Owner/Repo"},
		{"c", "a", "7", "body", "-r", "Owner/Repo"},
		{"comments", "comment", "7", "body", "-r", "Owner/Repo"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("accepted undocumented comment alias %q", args)
		}
	}
}

func TestIssueMutationBoundaries(t *testing.T) {
	if _, err := Parse([]string{"issues", "create", "-r", "o/r", "-t", strings.Repeat("界", 255)}); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]string{"comments", "7", strings.Repeat("x", maximumArgvBytes-20), "-r", "o/r"}); err != nil {
		t.Fatal(err)
	}
	rejected := [][]string{
		{"issues", "create", "-r", "o/r", "-t", ""},
		{"issues", "create", "-r", "o/r", "-t", strings.Repeat("界", 256)},
		{"issues", "create", "-r", "o/r", "-t", "t", "-a", "a,,b"},
		{"issues", "create", "-r", "o/r", "-t", "t", "--title", "again"},
		{"comments", "7", "body", "-d", "again", "-r", "o/r"},
		{"comments", "7", "", "-r", "o/r"},
		{"issues", "close", "-1", "-r", "o/r"},
		{"issues", "open", "7", "-r=o/r"},
	}
	for _, args := range rejected {
		if _, err := Parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}
