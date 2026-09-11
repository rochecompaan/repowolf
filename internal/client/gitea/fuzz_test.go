package gitea

import (
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"repos\x00Owner/Repo\x00-r\x00owner/repo",
		"repo\x00o/r\x00--repo\x00o/r\x00-o\x00json",
		"issues\x00--repo\x00o/r",
		"issues\x00list\x00--repo\x00o/r\x00--fields\x00title,index\x00--limit\x0050",
		"issue\x007\x00--repo\x00o/r\x00--comments",
		"login",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		args := strings.Split(input, "\x00")
		if len(args) > maximumArguments {
			args = args[:maximumArguments]
		}
		parsed, err := Parse(args)
		if err != nil {
			return
		}
		selector := parsed.request.GetContext().GetRepository()
		if selector == nil || selector.Host != "" || selector.SshPort != 0 || parsed.format > outputJSON {
			t.Fatalf("invalid successful parse: %#v", parsed)
		}
		if _, _, ok := parseSlug(selector.Owner + "/" + selector.Name); !ok {
			t.Fatalf("invalid successful selector: %#v", parsed)
		}
		switch {
		case parsed.request.GetRepositoryView() != nil:
			if len(parsed.fields) != 0 {
				t.Fatalf("repository parse has issue fields: %#v", parsed)
			}
		case parsed.request.GetIssueList() != nil:
			validateFuzzedIssueList(t, parsed)
		case parsed.request.GetIssueView() != nil:
			if parsed.request.GetIssueView().GetIndex() <= 0 || len(parsed.fields) != 0 {
				t.Fatalf("invalid successful issue view: %#v", parsed)
			}
		default:
			t.Fatalf("unexpected successful operation: %#v", parsed)
		}
	})
}

func validateFuzzedIssueList(t *testing.T, parsed command) {
	t.Helper()
	request := parsed.request.GetIssueList()
	if request.State != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN &&
		request.State != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED &&
		request.State != repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_ALL ||
		request.Page <= 0 || request.Limit <= 0 || request.Limit > 50 || len(request.Fields) == 0 || len(parsed.fields) != len(request.Fields) {
		t.Fatalf("invalid successful issue list: %#v", parsed)
	}
	seen := make(map[repowolfv1.GiteaIssueField]bool, len(request.Fields))
	for _, field := range request.Fields {
		if field == repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_UNSPECIFIED || field > repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_KIND || seen[field] {
			t.Fatalf("invalid successful issue fields: %#v", parsed)
		}
		seen[field] = true
	}
	for _, value := range []*string{request.Keyword, request.Author, request.Assignee, request.Mentions, request.Owner} {
		if value != nil && validateFilter(*value) != nil {
			t.Fatalf("invalid successful issue filter: %#v", parsed)
		}
	}
	if request.From != nil && !request.From.IsValid() || request.Until != nil && !request.Until.IsValid() || request.From != nil && request.Until != nil && request.From.AsTime().After(request.Until.AsTime()) {
		t.Fatalf("invalid successful issue time range: %#v", parsed)
	}
}
