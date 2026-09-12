//go:build linux && gitea_integration

package integration_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestRestrictedTeaReadOperationsAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.10.2", "172.29.10.0/24")
	baseURL, httpClient, container := fixture.baseURL, fixture.client, fixture.container
	token := struct {
		SHA1 string
	}{SHA1: fixture.token}
	type createdIssue struct {
		Index int64 `json:"number"`
	}
	type createdLabel struct {
		ID int64 `json:"id"`
	}
	var label createdLabel
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/labels", token.SHA1, map[string]any{"name": "bug", "color": "ee0701"}, &label, "", "")
	var open, closed createdIssue
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "open issue", "body": "private issue body", "labels": []int64{label.ID}}, &open, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", token.SHA1, map[string]any{"title": "closed issue", "body": "closed body"}, &closed, "", "")
	giteaJSON(t, httpClient, http.MethodPatch, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(closed.Index, 10), token.SHA1, map[string]any{"state": "closed"}, nil, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/branches", token.SHA1, map[string]any{"new_branch_name": "pull-fixture", "old_branch_name": "main"}, nil, "", "")
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/contents/pull-fixture.txt", token.SHA1, map[string]any{"branch": "pull-fixture", "message": "seed pull request", "content": base64.StdEncoding.EncodeToString([]byte("pull request fixture\n"))}, nil, "", "")
	var pull createdIssue
	giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/pulls", token.SHA1, map[string]any{"base": "main", "head": "pull-fixture", "title": "pull request"}, &pull, "", "")
	for i := 1; i <= 51; i++ {
		giteaJSON(t, httpClient, http.MethodPost, baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+strconv.FormatInt(open.Index, 10)+"/comments", token.SHA1, map[string]any{"body": "comment " + strconv.Itoa(i)}, nil, "", "")
	}
	commentURL := baseURL + "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(open.Index, 10) + "/timeline"
	type commentPageRecord struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	}
	var firstCommentPage, secondCommentPage []commentPageRecord
	giteaJSON(t, httpClient, http.MethodGet, commentURL+"?page=1&limit=50", token.SHA1, nil, &firstCommentPage, "", "")
	giteaJSON(t, httpClient, http.MethodGet, commentURL+"?page=2&limit=50", token.SHA1, nil, &secondCommentPage, "", "")
	if len(firstCommentPage) != 50 || len(secondCommentPage) != 2 || secondCommentPage[0].ID <= firstCommentPage[49].ID {
		t.Fatalf("Gitea timeline pagination returned page sizes %d and %d", len(firstCommentPage), len(secondCommentPage))
	}
	timelineTypes := map[string]int{}
	for _, entry := range append(firstCommentPage, secondCommentPage...) {
		timelineTypes[entry.Type]++
	}
	if timelineTypes["comment"] != 51 || timelineTypes["label"] != 1 {
		t.Fatalf("Gitea timeline types = %#v, want 51 comments and one label event", timelineTypes)
	}
	service := fixture.startBroker(t)
	binaries, broker, env := service.binaries, service.server, service.environment
	list := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "all", "--page", "1", "--limit", "2", "--fields", "index,title,state,comments", "--output", "json")
	var rows []map[string]any
	if err := json.Unmarshal(list, &rows); err != nil || len(rows) != 2 {
		t.Fatalf("list=%s err=%v", list, err)
	}
	wantRows := []struct {
		index    int64
		title    string
		state    string
		comments float64
	}{
		{index: closed.Index, title: "closed issue", state: "closed", comments: 0},
		{index: open.Index, title: "open issue", state: "open", comments: 51},
	}
	for i, row := range rows {
		if len(row) != 4 || row["index"] != float64(wantRows[i].index) || row["title"] != wantRows[i].title || row["state"] != wantRows[i].state || row["comments"] != wantRows[i].comments {
			t.Fatalf("row %d = %#v, want %#v", i, row, wantRows[i])
		}
	}
	openList := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "open", "--page", "1", "--limit", "50", "--fields", "index,state", "--output", "json")
	var openRows []map[string]any
	if err := json.Unmarshal(openList, &openRows); err != nil || len(openRows) != 1 || openRows[0]["index"] != float64(open.Index) || openRows[0]["state"] != "open" {
		t.Fatalf("open list=%s err=%v", openList, err)
	}
	emptyList := runTeaArgs(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/CanonicalRepo", "--state", "all", "--page", "2", "--limit", "50", "--fields", "index", "--output", "json")
	if string(emptyList) != "[]\n" {
		t.Fatalf("empty page = %q", emptyList)
	}
	viewCommand := exec.Command(binaries.Tea, "issues", strconv.FormatInt(open.Index, 10), "--repo", "CanonicalOwner/CanonicalRepo", "--comments", "--output", "json")
	viewCommand.Env = env
	var viewOutput, viewDiagnostic bytes.Buffer
	viewCommand.Stdout, viewCommand.Stderr = &viewOutput, &viewDiagnostic
	if err := viewCommand.Run(); err != nil {
		t.Fatalf("view: %v: %s; broker=%s; audit=%s", err, viewDiagnostic.String(), mustRead(broker.StderrPath), mustRead(broker.AuditPath))
	}
	view := viewOutput.Bytes()
	var detail struct {
		Index    int64    `json:"index"`
		Labels   []string `json:"labels"`
		Comments []struct {
			ID int64 `json:"id"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(view, &detail); err != nil || detail.Index != open.Index || len(detail.Labels) != 1 || detail.Labels[0] != "bug" || len(detail.Comments) != 51 {
		t.Fatalf("view=%s err=%v", view, err)
	}
	for i, c := range detail.Comments {
		if i > 0 && c.ID <= detail.Comments[i-1].ID {
			t.Fatal("comments out of order")
		}
	}
	deniedOutput, deniedDiagnostic := runTeaFailure(t, binaries.Tea, env, "issues", "--repo", "CanonicalOwner/OtherRepo", "--output", "json")
	if len(deniedOutput) != 0 || deniedDiagnostic != "tea: Gitea operation failed\n" {
		t.Fatalf("denied output=%q diagnostic=%q", deniedOutput, deniedDiagnostic)
	}
	pullOutput, pullDiagnostic := runTeaFailure(t, binaries.Tea, env, "issues", strconv.FormatInt(pull.Index, 10), "--repo", "CanonicalOwner/CanonicalRepo", "--comments", "--output", "json")
	if len(pullOutput) != 0 || pullDiagnostic != "tea: index is a pull request; use tea pulls\n" {
		t.Fatalf("pull output=%q diagnostic=%q broker=%s audit=%s", pullOutput, pullDiagnostic, mustRead(broker.StderrPath), mustRead(broker.AuditPath))
	}
	broker.Stop(t)
	audit := string(mustRead(broker.AuditPath))
	if !strings.Contains(audit, `"operation":"gitea.issue_list"`) || !strings.Contains(audit, `"operation":"gitea.issue_view"`) || strings.Contains(audit, "private issue body") {
		t.Fatalf("unexpected audit: %s", audit)
	}
	logs := dockerOutput(t, "logs", container)
	commentPath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(open.Index, 10) + "/timeline"
	if pageOneCalls, pageTwoCalls := giteaLogCountBoundedGET(logs, commentPath, 1, 50), giteaLogCountBoundedGET(logs, commentPath, 2, 50); pageOneCalls != 2 || pageTwoCalls != 2 {
		t.Fatalf("Gitea log contains %d page-1 and %d page-2 timeline requests for %s, want one setup proof and one restricted-client request each", pageOneCalls, pageTwoCalls, commentPath)
	}
	pullCommentPath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/" + strconv.FormatInt(pull.Index, 10) + "/timeline"
	if strings.Contains(logs, "GET "+pullCommentPath) || strings.Contains(logs, "/CanonicalOwner/OtherRepo/issues") {
		t.Fatalf("unexpected provider request in Gitea logs")
	}
}

func giteaLogCountBoundedGET(logs, path string, page, limit int) int {
	count := 0
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "GET "+path) && strings.Contains(line, "page="+strconv.Itoa(page)) && strings.Contains(line, "limit="+strconv.Itoa(limit)) {
			count++
		}
	}
	return count
}

func runTeaArgs(t *testing.T, binary string, env []string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("tea %v: %v: %s", args, err, stderr.String())
	}
	return stdout.Bytes()
}

func runTeaFailure(t *testing.T, binary string, env []string, args ...string) ([]byte, string) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatalf("tea %v unexpectedly succeeded: %s", args, stdout.String())
	}
	return stdout.Bytes(), stderr.String()
}
