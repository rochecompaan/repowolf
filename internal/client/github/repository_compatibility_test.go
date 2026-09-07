package github

import (
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

// The base revision accepted repository; name must be additive, not a rename.
func TestRepositoryJSONBaseCompatibility(t *testing.T) {
	cwd := newGitHubRepository(t)
	response := &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewResult{Repository: &repowolfv1.GitHubRepositoryRecord{Repository: "repo", Owner: "owner", Url: "https://github.com/owner/repo"}}}}
	for _, test := range []struct{ fields, want string }{
		{"repository", `{"repository":"repo"}`},
		{"repository,owner,url", `{"repository":"repo","owner":"owner","url":"https://github.com/owner/repo"}`},
		{"name,repository", `{"name":"repo","repository":"repo"}`},
	} {
		t.Run(test.fields, func(t *testing.T) {
			parsed, err := parseArgs([]string{"repo", "view", "--repo", "owner/repo", "--json", test.fields}, cwd)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := render(parsed, response)
			if err != nil || string(raw) != test.want+"\n" {
				t.Fatalf("render = %q, %v; want %s", raw, err, test.want)
			}
		})
	}
}
