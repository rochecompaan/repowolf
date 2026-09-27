package gitea

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestParseIssueEditAcceptedGrammar(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		format     outputFormat
		assignees  []string
		labels     []string
		wantAction string
	}{
		{name: "text presence and default output", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", "title", "--description", ""}},
		{name: "edit alias empty exact replacement json", args: []string{"issues", "e", "7", "--repo", "Owner/Repo", "--set-assignees", "", "-o", "json"}, format: outputJSON, assignees: []string{}, wantAction: "set-assignees"},
		{name: "add assignees short alias table", args: []string{"issues", "edit", "7", "--output", "table", "-a", "alice,bob", "--repo", "Owner/Repo"}, format: outputTable, assignees: []string{"alice", "bob"}, wantAction: "add-assignees"},
		{name: "add assignees long alias simple", args: []string{"issues", "edit", "7", "--add-assignees", "alice", "--output", "simple", "--repo", "Owner/Repo"}, assignees: []string{"alice"}, wantAction: "add-assignees"},
		{name: "remove assignees", args: []string{"issues", "edit", "7", "--remove-assignees", "alice", "--repo", "Owner/Repo"}, assignees: []string{"alice"}, wantAction: "remove-assignees"},
		{name: "add labels short alias", args: []string{"issues", "edit", "7", "-L", "bug,urgent", "--repo", "Owner/Repo"}, labels: []string{"bug", "urgent"}, wantAction: "add-labels"},
		{name: "add labels long alias", args: []string{"issues", "edit", "7", "--add-labels", "bug", "--repo", "Owner/Repo"}, labels: []string{"bug"}, wantAction: "add-labels"},
		{name: "remove labels", args: []string{"issues", "edit", "7", "--remove-labels", "stale", "--repo", "Owner/Repo"}, labels: []string{"stale"}, wantAction: "remove-labels"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := Parse(test.args)
			if err != nil {
				t.Fatalf("Parse(%q): %v", test.args, err)
			}
			edit := parsed.request.GetIssueEdit()
			if !parsed.mutation || edit.GetIndex() != 7 || parsed.format != test.format || parsed.request.GetContext().GetRepository().GetOwner() != "Owner" {
				t.Fatalf("parsed=%#v", parsed)
			}
			if edit.Title != nil && edit.GetTitle() != "title" || edit.Description != nil && edit.GetDescription() != "" {
				t.Fatalf("text presence/value lost: %#v", edit)
			}
			var got []string
			switch test.wantAction {
			case "set-assignees":
				if edit.GetSetAssignees() == nil {
					t.Fatal("set action absent")
				}
				got = edit.GetSetAssignees().GetValues()
			case "add-assignees":
				got = edit.GetAddAssignees().GetValues()
			case "remove-assignees":
				got = edit.GetRemoveAssignees().GetValues()
			case "add-labels":
				got = edit.GetAddLabels().GetValues()
			case "remove-labels":
				got = edit.GetRemoveLabels().GetValues()
			}
			want := test.assignees
			if test.labels != nil {
				want = test.labels
			}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("action values=%q want=%q", got, want)
			}
		})
	}
}

func TestParseIssueEditValidatesBeforePrecedence(t *testing.T) {
	parsed, err := Parse([]string{"issues", "edit", "7", "-r", "Owner/Repo", "--set-assignees", "alice", "--add-assignees", "bob", "--remove-assignees", "carol", "--add-labels", "bug", "--remove-labels", "stale"})
	if err != nil {
		t.Fatal(err)
	}
	edit := parsed.request.GetIssueEdit()
	if got := edit.GetSetAssignees().GetValues(); len(got) != 1 || got[0] != "alice" || edit.GetAddAssignees() != nil || edit.GetRemoveAssignees() != nil {
		t.Fatalf("assignee precedence: %#v", edit.AssigneeAction)
	}
	if got := edit.GetAddLabels().GetValues(); len(got) != 1 || got[0] != "bug" || edit.GetRemoveLabels() != nil {
		t.Fatalf("label precedence: %#v", edit.LabelAction)
	}
	for _, args := range [][]string{
		{"issues", "edit", "7", "-r", "Owner/Repo", "--set-assignees", "alice", "--add-assignees", ""},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--add-labels", "bug", "--remove-labels", ""},
	} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("invalid lower-precedence value accepted: %q", args)
		}
	}
}

func TestParseIssueEditAcceptsExactBoundaries(t *testing.T) {
	name := strings.Repeat("界", 255)
	values := make([]string, 25)
	for i := range values {
		values[i] = strings.Repeat("x", i+1)
	}
	if _, err := parseIssueEdit([]string{"7", "-r", "Owner/Repo", "--description", strings.Repeat("x", 64<<10)}); err != nil {
		t.Fatalf("local description boundary rejected: %v", err)
	}
	for _, args := range [][]string{
		{"issues", "edit", "7", "-r", "Owner/Repo", "--title", name},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--add-assignees", name},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--add-labels", strings.Join(values, ",")},
	} {
		if _, err := Parse(args); err != nil {
			t.Fatalf("boundary Parse(%q): %v", args[:len(args)-1], err)
		}
	}
}

func TestParseIssueEditRejectsClosedGrammarAndBounds(t *testing.T) {
	invalidUTF8 := string([]byte{0xff})
	tooManyNames := make([]string, 26)
	for i := range tooManyNames {
		tooManyNames[i] = strings.Repeat("x", i+1)
	}
	tooManyArgs := []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", "title"}
	for len(tooManyArgs) <= maximumArguments {
		tooManyArgs = append(tooManyArgs, "--remove-labels", "stale")
	}
	tests := []struct {
		name string
		args []string
	}{
		{name: "no mutation", args: []string{"issues", "edit", "7", "-r", "Owner/Repo"}},
		{name: "zero index", args: []string{"issues", "edit", "0", "-r", "Owner/Repo", "--title", "x"}},
		{name: "negative index", args: []string{"issues", "edit", "-1", "-r", "Owner/Repo", "--title", "x"}},
		{name: "overflow index", args: []string{"issues", "edit", "9223372036854775808", "-r", "Owner/Repo", "--title", "x"}},
		{name: "extra positional", args: []string{"issues", "edit", "7", "8", "-r", "Owner/Repo", "--title", "x"}},
		{name: "missing repo", args: []string{"issues", "edit", "7", "--title", "x"}},
		{name: "invalid repo", args: []string{"issues", "edit", "7", "-r", "Owner", "--title", "x"}},
		{name: "duplicate repo aliases", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--repo", "Owner/Repo", "--title", "x"}},
		{name: "duplicate output aliases", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "-o", "json", "--output", "table", "--title", "x"}},
		{name: "duplicate action aliases", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "-a", "alice", "--add-assignees", "bob"}},
		{name: "equals flag", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title=x"}},
		{name: "title empty", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", ""}},
		{name: "title too long", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", strings.Repeat("界", 256)}},
		{name: "description too long", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--description", strings.Repeat("x", (64<<10)+1)}},
		{name: "invalid utf8", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", invalidUTF8}},
		{name: "nul", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", "x\x00y"}},
		{name: "malformed csv", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--add-assignees", "alice,,bob"}},
		{name: "duplicate csv", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--add-labels", "bug,bug"}},
		{name: "too many csv values", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--remove-labels", strings.Join(tooManyNames, ",")}},
		{name: "empty add", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--add-labels", ""}},
		{name: "unsupported short title", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "-t", "x"}},
		{name: "unsupported create flag", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--state", "open"}},
		{name: "unsupported milestone", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--milestone", "v1"}},
		{name: "unsupported deadline", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--deadline", "tomorrow"}},
		{name: "unsupported referenced version", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--ref", "main"}},
		{name: "unsupported pull request", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--pull-request", "true"}},
		{name: "unsupported stdin", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--description-file", "-"}},
		{name: "unsupported editor", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--editor", "vim"}},
		{name: "invalid output", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--title", "x", "-o", "yaml"}},
		{name: "too many args", args: tooManyArgs},
		{name: "argv too large", args: []string{"issues", "edit", "7", "-r", "Owner/Repo", "--description", strings.Repeat("x", maximumArgvBytes)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(test.args); err == nil {
				t.Fatalf("Parse(%q) succeeded", test.args)
			}
		})
	}
}

func TestRunRejectsIssueEditUsageBeforeClientSetup(t *testing.T) {
	t.Setenv("REPOWOLF_ENDPOINT", "")
	for _, args := range [][]string{
		{"issues", "edit", "7", "-r", "Owner/Repo"},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--set-assignees", "alice", "--add-assignees", ""},
		{"issues", "edit", "7", "-r", "Owner/Repo", "--title", strings.Repeat("x", 256)},
	} {
		var stdout, stderr bytes.Buffer
		if status := Run(context.Background(), args, &stdout, &stderr); status != 2 || stdout.Len() != 0 || stderr.String() != usage {
			t.Fatalf("Run(%q) status/output = %d/%q/%q", args, status, stdout.String(), stderr.String())
		}
	}
}
