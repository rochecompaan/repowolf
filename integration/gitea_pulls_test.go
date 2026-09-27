//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestRestrictedTeaPullAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.14.2", "172.29.14.0/24")
	// An organization repository is required for real team review actors.
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/orgs", fixture.token, map[string]any{"username": "CanonicalOrg", "full_name": "Canonical Organization"}, nil, "", "")
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/orgs/CanonicalOrg/repos", fixture.token, map[string]any{"name": "PullRepo", "private": true, "auto_init": true, "default_branch": "main"}, nil, "", "")
	for _, user := range []string{"RequestedUser", "ReviewUser", "TeamMember"} {
		dockerOutput(t, "exec", "--user", "git", fixture.container, "gitea", "admin", "user", "create", "--username", user, "--password", "correct-horse-battery-staple", "--email", user+"@example.invalid", "--must-change-password=false")
		giteaJSON(t, fixture.client, http.MethodPut, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/collaborators/"+user, fixture.token, map[string]any{"permission": "write"}, nil, "", "")
	}
	var reviewToken struct {
		SHA1 string `json:"sha1"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/users/ReviewUser/tokens", "", map[string]any{"name": "reviews", "scopes": []string{"write:repository", "write:issue"}}, &reviewToken, "ReviewUser", "correct-horse-battery-staple")
	var team struct {
		ID int64 `json:"id"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/orgs/CanonicalOrg/teams", fixture.token, map[string]any{"name": "reviewers", "permission": "write", "units": []string{"repo.code", "repo.pulls"}}, &team, "", "")
	giteaJSON(t, fixture.client, http.MethodPut, fixture.baseURL+"/api/v1/teams/"+strconv.FormatInt(team.ID, 10)+"/members/TeamMember", fixture.token, nil, nil, "", "")
	giteaJSON(t, fixture.client, http.MethodPut, fixture.baseURL+"/api/v1/teams/"+strconv.FormatInt(team.ID, 10)+"/repos/CanonicalOrg/PullRepo", fixture.token, nil, nil, "", "")
	var normalIssue struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/issues", fixture.token, map[string]any{"title": "normal issue"}, &normalIssue, "", "")
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/branches", fixture.token, map[string]any{"new_branch_name": "pull-read", "old_branch_name": "main"}, nil, "", "")
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/contents/pull.txt", fixture.token, map[string]any{"branch": "pull-read", "message": "seed pull", "content": base64.StdEncoding.EncodeToString([]byte("pull\n"))}, nil, "", "")
	var created struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls", fixture.token, map[string]any{"base": "main", "head": "pull-read", "title": "pull read", "body": "private pull body"}, &created, "", "")
	index := strconv.FormatInt(created.Index, 10)
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/branches", fixture.token, map[string]any{"new_branch_name": "closed-read", "old_branch_name": "main"}, nil, "", "")
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/contents/closed.txt", fixture.token, map[string]any{"branch": "closed-read", "message": "seed closed pull", "content": base64.StdEncoding.EncodeToString([]byte("closed\n"))}, nil, "", "")
	var closed struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls", fixture.token, map[string]any{"base": "main", "head": "closed-read", "title": "closed pull"}, &closed, "", "")
	giteaJSON(t, fixture.client, http.MethodPatch, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls/"+strconv.FormatInt(closed.Index, 10), fixture.token, map[string]any{"state": "closed"}, nil, "", "")
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls/"+index+"/requested_reviewers", fixture.token, map[string]any{"reviewers": []string{"RequestedUser"}, "team_reviewers": []string{"reviewers"}}, nil, "", "")
	for i := 1; i <= 51; i++ {
		giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/issues/"+index+"/comments", fixture.token, map[string]any{"body": "ordinary comment " + strconv.Itoa(i)}, nil, "", "")
	}
	for i := 1; i <= 49; i++ {
		giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls/"+index+"/reviews", reviewToken.SHA1, map[string]any{"event": "COMMENT", "body": "review " + strconv.Itoa(i)}, nil, "", "")
	}
	var firstReviewPage, secondReviewPage []map[string]any
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls/"+index+"/reviews?page=1&limit=50", fixture.token, nil, &firstReviewPage, "", "")
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls/"+index+"/reviews?page=2&limit=50", fixture.token, nil, &secondReviewPage, "", "")
	if len(firstReviewPage) != 50 || len(secondReviewPage) != 1 {
		t.Fatalf("review page sizes = %d, %d", len(firstReviewPage), len(secondReviewPage))
	}
	var rawPulls []map[string]json.RawMessage
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOrg/PullRepo/pulls?state=all&page=1&limit=2", fixture.token, nil, &rawPulls, "", "")
	if len(rawPulls) != 2 {
		t.Fatalf("raw pull count=%d", len(rawPulls))
	}
	// The TLS proxy makes the provider's optional-boolean wire cases deterministic
	// while preserving the real Gitea request/SDK/adapter path.
	rawPulls[0]["mergeable"] = json.RawMessage("false")
	delete(rawPulls[1], "mergeable")
	expectedList, sawFalse, sawUnset := expectedPullListJSON(t, rawPulls, created.Index, closed.Index)
	if !sawFalse || !sawUnset {
		t.Fatalf("fixture did not establish false and unavailable mergeable presence: false=%v unset=%v raw=%s", sawFalse, sawUnset, mustJSON(rawPulls))
	}
	service := fixture.startBrokerAt(t, fixture.startPullPresenceProxy(t))
	list := runTeaArgs(t, service.binaries.Tea, service.environment, "pulls", "--repo", "CanonicalOrg/PullRepo", "--state", "all", "--page", "1", "--limit", "2", "--fields", "index,title,state,mergeable,base,head,comments", "--output", "json")
	if string(list) != expectedList {
		t.Fatalf("list=%s want=%s", list, expectedList)
	}
	withoutComments := runTeaArgs(t, service.binaries.Tea, service.environment, "pulls", index, "--repo", "CanonicalOrg/PullRepo", "--output", "json")
	var without map[string]any
	if err := json.Unmarshal(withoutComments, &without); err != nil || without["comments"] != nil || len(without["reviews"].([]any)) != 51 {
		t.Fatalf("without comments=%s err=%v", withoutComments, err)
	}
	view := runTeaArgs(t, service.binaries.Tea, service.environment, "pulls", index, "--repo", "CanonicalOrg/PullRepo", "--comments", "--output", "json")
	var detail struct {
		Index    int64 `json:"index"`
		Comments []struct {
			ID int64 `json:"id"`
		} `json:"comments"`
		Reviews []struct {
			ID    int64          `json:"id"`
			Actor map[string]any `json:"actor"`
		} `json:"reviews"`
		Requested []map[string]any `json:"requested-reviewers"`
	}
	if err := json.Unmarshal(view, &detail); err != nil || detail.Index != created.Index || len(detail.Comments) != 51 || len(detail.Reviews) != 51 || len(detail.Requested) < 2 {
		t.Fatalf("view err=%v comments=%d reviews=%d requested=%d output=%s", err, len(detail.Comments), len(detail.Reviews), len(detail.Requested), view)
	}
	var userActor, teamActor bool
	for _, review := range detail.Reviews {
		if review.Actor["user"] != nil {
			userActor = true
		}
		if review.Actor["team"] != nil {
			teamActor = true
		}
	}
	if !userActor || !teamActor {
		t.Fatalf("missing typed review actors: user=%v team=%v", userActor, teamActor)
	}
	for i := 1; i < len(detail.Comments); i++ {
		if detail.Comments[i].ID <= detail.Comments[i-1].ID {
			t.Fatal("comments out of order")
		}
	}
	for i := 1; i < len(detail.Reviews); i++ {
		if detail.Reviews[i].ID <= detail.Reviews[i-1].ID {
			t.Fatal("reviews out of order")
		}
	}
	empty := runTeaArgs(t, service.binaries.Tea, service.environment, "pulls", "--repo", "CanonicalOrg/PullRepo", "--state", "all", "--page", "2", "--limit", "50", "--fields", "index", "--output", "json")
	if string(empty) != "[]\n" {
		t.Fatalf("empty=%q", empty)
	}
	kindOutput, kindDiagnostic := runTeaFailure(t, service.binaries.Tea, service.environment, "pulls", strconv.FormatInt(normalIssue.Index, 10), "--repo", "CanonicalOrg/PullRepo", "--output", "json")
	if len(kindOutput) != 0 || kindDiagnostic != "tea: index is an issue; use tea issues\n" {
		t.Fatalf("kind output=%q diagnostic=%q", kindOutput, kindDiagnostic)
	}
	deniedOutput, deniedDiagnostic := runTeaFailure(t, service.binaries.Tea, service.environment, "pulls", "--repo", "CanonicalOwner/OtherRepo", "--output", "json")
	if len(deniedOutput) != 0 || deniedDiagnostic != "tea: Gitea operation failed\n" {
		t.Fatalf("denied output=%q diagnostic=%q", deniedOutput, deniedDiagnostic)
	}
	service.server.Stop(t)
	audit := string(mustRead(service.server.AuditPath))
	for _, secret := range []string{fixture.token, "private pull body", "ordinary comment 1", "review 1"} {
		if strings.Contains(audit, secret) {
			t.Fatalf("audit contains secret marker")
		}
	}
	for _, operation := range []string{`"operation":"gitea.pull_list"`, `"operation":"gitea.pull_view"`} {
		if !strings.Contains(audit, operation) {
			t.Fatalf("audit missing %s: %s", operation, audit)
		}
	}
	logs := dockerOutput(t, "logs", fixture.container)
	timelinePath := "/api/v1/repos/CanonicalOrg/PullRepo/issues/" + index + "/timeline"
	if giteaLogCountBoundedGET(logs, timelinePath, 1, 50) != 1 || giteaLogCountBoundedGET(logs, timelinePath, 2, 50) != 1 {
		t.Fatalf("unexpected timeline pagination")
	}
	reviewPath := "/api/v1/repos/CanonicalOrg/PullRepo/pulls/" + index + "/reviews"
	if giteaLogCountBoundedGET(logs, reviewPath, 1, 50) != 3 || giteaLogCountBoundedGET(logs, reviewPath, 2, 50) != 3 {
		t.Fatalf("unexpected review pagination")
	}
	if strings.Contains(logs, "/CanonicalOwner/OtherRepo/pulls") {
		t.Fatalf("denied request reached provider")
	}

}

func expectedPullListJSON(t *testing.T, rawPulls []map[string]json.RawMessage, openIndex, closedIndex int64) (string, bool, bool) {
	t.Helper()
	var output bytes.Buffer
	output.WriteByte('[')
	sawFalse, sawUnset := false, false
	for i, raw := range rawPulls {
		var index int64
		if err := json.Unmarshal(raw["number"], &index); err != nil {
			t.Fatal(err)
		}
		title, state, head, comments := "pull read", "open", "pull-read", int64(51)
		if index == closedIndex {
			title, state, head, comments = "closed pull", "closed", "closed-read", 0
		} else if index != openIndex {
			t.Fatalf("unexpected pull index %d", index)
		}
		if i > 0 {
			output.WriteByte(',')
		}
		fmt.Fprintf(&output, `{"index":%d,"title":%q,"state":%q`, index, title, state)
		mergeable, ok := raw["mergeable"]
		if !ok || bytes.Equal(bytes.TrimSpace(mergeable), []byte("null")) {
			sawUnset = true
		} else {
			var value bool
			if err := json.Unmarshal(mergeable, &value); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&output, `,"mergeable":%t`, value)
			sawFalse = sawFalse || !value
		}
		fmt.Fprintf(&output, `,"base":"main","head":%q,"comments":%d}`, head, comments)
	}
	output.WriteString("]\n")
	return output.String(), sawFalse, sawUnset
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
