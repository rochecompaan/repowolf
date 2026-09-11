package gitea

import (
	"strings"
	"testing"
)

func TestParseRepositoryView(t *testing.T) {
	for _, args := range [][]string{
		{"repos", "Group/Repo", "--repo", "group/repo"},
		{"repo", "Group/Repo", "-r", "group/repo", "-o", "json"},
	} {
		parsed, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%q): %v", args, err)
		}
		selector := parsed.request.GetContext().GetRepository()
		if selector.GetOwner() != "Group" || selector.GetName() != "Repo" || selector.GetHost() != "" || selector.GetSshPort() != 0 || parsed.request.GetRepositoryView() == nil {
			t.Fatalf("parsed = %#v", parsed)
		}
	}
}

func TestParseRejectsClosedGrammar(t *testing.T) {
	cases := [][]string{
		{"repos", "o/r", "--repo", "o/x"}, {"repos", "o/r"}, {"repos", "o/r", "-r", "o/r", "--repo", "o/r"},
		{"repos", "o/r.git", "-r", "o/r.git"}, {"repos", "o/r", "--repo=o/r"}, {"login", "o/r", "-r", "o/r"},
		{"repos", "o/r", "-r", "o/r", "-o", "yaml"}, {"repos", "o/r", "-r", "o/r", "extra"},
	}
	for _, args := range cases {
		if _, err := Parse(args); err == nil {
			t.Errorf("Parse(%q) succeeded", args)
		}
	}
	tooMany := make([]string, 65)
	for i := range tooMany {
		tooMany[i] = "x"
	}
	if _, err := Parse(tooMany); err == nil {
		t.Error("65 arguments accepted")
	}
	if _, err := Parse([]string{"repos", "o/r", "-r", "o/" + strings.Repeat("r", 64<<10)}); err == nil {
		t.Error("oversize accepted")
	}
	if _, err := Parse([]string{"repos", "o/r", "-r", "o/r\x00"}); err == nil {
		t.Error("NUL accepted")
	}
}
