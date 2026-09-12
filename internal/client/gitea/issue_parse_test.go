package gitea

import (
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"reflect"
	"strings"
	"testing"
)

func TestParseIssueList(t *testing.T) {
	parsed, err := Parse([]string{"issues", "list", "--repo", "Owner/Repo", "--state", "all", "-k", "needle", "-p", "2", "--lm", "50", "--fields", "title,index,comments", "-o", "json"})
	if err != nil {
		t.Fatal(err)
	}
	r := parsed.request.GetIssueList()
	if r.GetState() != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_ALL || r.GetPage() != 2 || r.GetLimit() != 50 || r.Keyword == nil || parsed.format != outputJSON || !reflect.DeepEqual(r.Fields, []repowolfv1.GiteaIssueField{6, 1, 14}) {
		t.Fatalf("parsed=%#v", parsed)
	}
}
func TestParseIssueView(t *testing.T) {
	parsed, err := Parse([]string{"i", "7", "-r", "Owner/Repo", "--comments", "-o", "json"})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.request.GetIssueView().GetIndex() != 7 || !parsed.request.GetIssueView().GetIncludeComments() {
		t.Fatalf("parsed=%#v", parsed)
	}
}
func TestParseIssueDefaultsAndAliases(t *testing.T) {
	for _, prefix := range [][]string{{"issues"}, {"issue", "ls"}, {"i", "list"}} {
		args := append(prefix, "--repo", "o/r")
		p, e := Parse(args)
		if e != nil {
			t.Fatal(e)
		}
		if p.request.GetIssueList().GetPage() != 1 || p.request.GetIssueList().GetLimit() != 30 || len(p.fields) != 8 {
			t.Fatalf("%#v", p)
		}
	}
}
func TestParseIssueRejectsClosedGrammar(t *testing.T) {
	cases := [][]string{{"issues", "--repo", "o/r", "--kind", "issue"}, {"issues", "--repo", "o/r", "--limit", "51"}, {"issues", "--repo", "o/r", "--fields", "title,title"}, {"issues", "0", "--repo", "o/r"}, {"issues", "7", "--repo", "o/r", "--comments", "--comments"}, {"issues", "--repo", "o/r", "--keyword", ""}, {"issues", "--repo", "o/r", "--keyword", strings.Repeat("x", 256)}, {"issues", "create", "--repo", "o/r"}, {"issues", "--repo=o/r"}}
	for _, args := range cases {
		if _, e := Parse(args); e == nil {
			t.Errorf("accepted %q", args)
		}
	}
}
