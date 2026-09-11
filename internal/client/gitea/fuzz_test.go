package gitea

import (
	"strings"
	"testing"
)

func FuzzParse(f *testing.F) {
	for _, seed := range []string{"repos\x00Owner/Repo\x00-r\x00owner/repo", "repo\x00o/r\x00--repo\x00o/r\x00-o\x00json", "login"} {
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
		r := parsed.request.GetContext().GetRepository()
		if parsed.request.GetRepositoryView() == nil || r == nil || r.Host != "" || r.SshPort != 0 || parsed.format > outputJSON {
			t.Fatalf("invalid successful parse: %#v", parsed)
		}
	})
}
