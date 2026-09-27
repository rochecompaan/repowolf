package gitea

import (
	"reflect"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	providergitea "github.com/rochecompaan/repowolf/internal/provider/gitea"
)

func TestParsePullListTypedRequest(t *testing.T) {
	parsed, err := Parse([]string{"pr", "list", "-r", "Owner/Repo", "--state", "all", "-p", "2", "--lm", "50", "--fields", "title,index,mergeable", "-o", "json"})
	if err != nil {
		t.Fatal(err)
	}
	request := parsed.request.GetPullList()
	if request == nil || request.State != repowolfv1.GiteaPullState_GITEA_PULL_STATE_ALL || request.Page != 2 || request.Limit != 50 || parsed.format != outputJSON || parsed.mutation {
		t.Fatalf("unexpected parse: %#v", parsed)
	}
	want := []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE, repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX, repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE}
	if !reflect.DeepEqual(request.Fields, want) {
		t.Fatalf("fields = %v, want %v", request.Fields, want)
	}
	if err := providergitea.ValidateRequest(parsed.request); err != nil {
		t.Fatal(err)
	}
}

func TestParsePullAliasesAndView(t *testing.T) {
	for _, alias := range []string{"pulls", "pull", "pr"} {
		parsed, err := Parse([]string{alias, "7", "--repo", "Owner/Repo", "--comments"})
		if err != nil {
			t.Fatalf("%s: %v", alias, err)
		}
		if got := parsed.request.GetPullView(); got == nil || got.Index != 7 || !got.IncludeComments || parsed.format != outputSimple {
			t.Fatalf("%s: %#v", alias, parsed)
		}
	}
}

func TestParsePullRejectsClosedGrammar(t *testing.T) {
	cases := [][]string{
		{"pulls", "--repo", "Owner/Repo", "--limit", "51"},
		{"pulls", "--repo", "Owner/Repo", "--comments"},
		{"pulls", "1", "--repo", "Owner/Repo", "--fields", "title"},
		{"pulls", "merge", "--repo", "Owner/Repo"},
		{"pulls", "--repo=Owner/Repo"},
	}
	for _, args := range cases {
		if _, err := Parse(args); err == nil {
			t.Errorf("Parse(%v) succeeded", args)
		}
	}
}
