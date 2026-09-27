package gitea

import (
	"strings"
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

func TestNormalizePullRejectsMalformedNestedRecords(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*sdk.PullRequest)
	}{
		{name: "nil author", mutate: func(value *sdk.PullRequest) { value.Poster = nil }},
		{name: "wrong base casing", mutate: func(value *sdk.PullRequest) { value.Base.Repository.Owner.UserName = "owner" }},
		{name: "invalid head repository", mutate: func(value *sdk.PullRequest) { value.Head.Repository.FullName = "other/repo" }},
		{name: "duplicate assignee", mutate: func(value *sdk.PullRequest) {
			user := &sdk.User{ID: 9, UserName: "same"}
			value.Assignees = []*sdk.User{user, user}
		}},
		{name: "duplicate label", mutate: func(value *sdk.PullRequest) {
			label := &sdk.Label{ID: 9, Name: "same"}
			value.Labels = []*sdk.Label{label, label}
		}},
		{name: "duplicate reviewer", mutate: func(value *sdk.PullRequest) {
			user := &sdk.User{ID: 9, UserName: "same"}
			value.RequestedReviewers = []*sdk.User{user, user}
		}},
		{name: "invalid text", mutate: func(value *sdk.PullRequest) { value.Body = "bad\x00" }},
		{name: "updated before created", mutate: func(value *sdk.PullRequest) { earlier := value.Created.Add(-time.Second); value.Updated = &earlier }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := validSDKPull()
			test.mutate(value)
			if normalized, err := normalizePull(value, pullPresence{}, "Owner", "Repo"); err == nil || normalized != nil {
				t.Fatalf("normalized=%#v err=%v", normalized, err)
			}
		})
	}
}

func TestNormalizePullAllowsCrossRepositoryHeadAndCopiesCollections(t *testing.T) {
	value := validSDKPull()
	value.Assignees = []*sdk.User{{ID: 4, UserName: "first"}, {ID: 5, UserName: "second"}}
	value.Labels = []*sdk.Label{{ID: 6, Name: "bug"}}
	value.RequestedReviewers = []*sdk.User{{ID: 7, UserName: "reviewer"}}
	value.RequestedReviewersTeams = []*sdk.Team{{ID: 8, Name: "team"}}
	normalized, err := normalizePull(value, pullPresence{}, "Owner", "Repo")
	if err != nil {
		t.Fatal(err)
	}
	value.Assignees[0].UserName = strings.Repeat("x", 10)
	if normalized.head != "alice:topic" || normalized.assignees[0] != "first" || normalized.labels[0] != "bug" || len(normalized.requestedReviewers) != 2 || normalized.requestedReviewers[1].GetTeam().GetName() != "team" {
		t.Fatalf("normalized=%#v", normalized)
	}
}
