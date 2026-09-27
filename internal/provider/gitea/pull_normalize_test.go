package gitea

import (
	"testing"
	"time"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func validSDKPull() *sdk.PullRequest {
	now := time.Now().UTC()
	return &sdk.PullRequest{Index: 1, Poster: &sdk.User{ID: 2, UserName: "alice"}, HTMLURL: "https://g/Owner/Repo/pulls/1", Title: "title", State: sdk.StateOpen, Created: &now, Updated: &now, Base: &sdk.PRBranchInfo{Name: "Owner:main", Ref: "main", Sha: "abc", Repository: &sdk.Repository{Owner: &sdk.User{ID: 3, UserName: "Owner"}, Name: "Repo", FullName: "Owner/Repo"}}, Head: &sdk.PRBranchInfo{Name: "alice:topic", Sha: "def", Repository: &sdk.Repository{Owner: &sdk.User{ID: 2, UserName: "alice"}, Name: "fork", FullName: "alice/fork"}}, RequestedReviewers: []*sdk.User{}, RequestedReviewersTeams: []*sdk.Team{}}
}
func TestNormalizePullPresenceAndProjection(t *testing.T) {
	mergeable := false
	allow := true
	value := validSDKPull()
	value.Mergeable = false
	value.AllowMaintainerEdit = true
	n, err := normalizePull(value, pullPresence{mergeable: &mergeable, allowMaintainerEdit: &allow}, "Owner", "Repo")
	if err != nil {
		t.Fatal(err)
	}
	record := projectPull(n, []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_TITLE, repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE})
	if record.Title != "title" || record.Mergeable == nil || *record.Mergeable || record.Author != "" || record.Index != 1 {
		t.Fatalf("unexpected record %#v", record)
	}
}
func TestNormalizePullRejectsPresenceMismatch(t *testing.T) {
	value := validSDKPull()
	value.Mergeable = true
	if _, err := normalizePull(value, pullPresence{}, "Owner", "Repo"); err == nil {
		t.Fatal("accepted mismatch")
	}
}
