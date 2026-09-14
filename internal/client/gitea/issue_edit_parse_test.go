package gitea

import "testing"

func TestParseIssueEdit(t *testing.T) {
	tests := []struct {
		args  []string
		check func(*testing.T, command)
	}{
		{[]string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", "title", "--description", ""}, func(t *testing.T, c command) {
			if c.request.GetIssueEdit().GetTitle() != "title" || c.request.GetIssueEdit().Description == nil {
				t.Fatal("text presence lost")
			}
		}},
		{[]string{"issues", "e", "7", "--repo", "Owner/Repo", "--set-assignees", "", "-o", "json"}, func(t *testing.T, c command) {
			if c.request.GetIssueEdit().GetSetAssignees() == nil || len(c.request.GetIssueEdit().GetSetAssignees().Values) != 0 {
				t.Fatal("empty set-assignees lost")
			}
		}},
	}
	for _, tt := range tests {
		c, err := Parse(tt.args)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.args, err)
		}
		if !c.mutation || c.request.GetIssueEdit().Index != 7 {
			t.Fatal("missing edit")
		}
		tt.check(t, c)
	}
}

func TestParseIssueEditPrecedence(t *testing.T) {
	c, err := Parse([]string{"issues", "edit", "7", "-r", "Owner/Repo", "--set-assignees", "alice", "--add-assignees", "bob", "--remove-assignees", "carol", "--add-labels", "bug", "--remove-labels", "stale"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.request.GetIssueEdit().GetSetAssignees().Values; len(got) != 1 || got[0] != "alice" {
		t.Fatalf("set precedence: %v", got)
	}
	if got := c.request.GetIssueEdit().GetAddLabels().Values; len(got) != 1 || got[0] != "bug" {
		t.Fatalf("add precedence: %v", got)
	}
}

func TestParseIssueEditRejectsInvalid(t *testing.T) {
	for _, args := range [][]string{
		{"issues", "edit", "7", "-r", "Owner/Repo"},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--add-labels", ""},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--title", ""},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--title=x"},
		{"issues", "edit", "7", "-r", "Owner/Repo", "-t", "title"},
		{"issues", "edit", "7", "-r", "Owner/Repo", "-d", "body"},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("Parse(%q) succeeded", args)
		}
	}
}
