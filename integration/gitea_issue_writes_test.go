//go:build linux && gitea_integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

func TestRestrictedTeaIssueWritesAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.11.2", "172.29.11.0/24")
	type resource struct {
		ID int64 `json:"id"`
	}
	var label resource
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/labels", fixture.token, map[string]any{"name": "bug", "color": "ee0701"}, &label, "", "")
	service := fixture.startBroker(t)
	defer service.server.Stop(t)
	created := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "write integration", "-d", "private write body", "-a", "CanonicalOwner", "-L", "bug", "-o", "json")
	var issue struct {
		Index int64  `json:"index"`
		State string `json:"state"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(created, &issue); err != nil || issue.Index <= 0 || issue.State != "open" || issue.Title != "write integration" {
		t.Fatalf("create=%s err=%v", created, err)
	}
	index := strconv.FormatInt(issue.Index, 10)
	comment := runTeaArgs(t, service.binaries.Tea, service.environment, "comments", "add", index, "private comment body", "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	var commentValue struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(comment, &commentValue); err != nil || commentValue.ID <= 0 || commentValue.Body != "private comment body" {
		t.Fatalf("comment=%s err=%v", comment, err)
	}
	closed := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if err := json.Unmarshal(closed, &issue); err != nil || issue.State != "closed" {
		t.Fatalf("close=%s err=%v", closed, err)
	}
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	reopened := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "reopen", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if err := json.Unmarshal(reopened, &issue); err != nil || issue.State != "open" {
		t.Fatalf("reopen=%s err=%v", reopened, err)
	}
	var independent struct {
		State    string `json:"state"`
		Body     string `json:"body"`
		Comments int    `json:"comments"`
		Labels   []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &independent, "", "")
	if independent.State != "open" || independent.Body != "private write body" || independent.Comments != 1 || len(independent.Labels) != 1 || independent.Labels[0].Name != "bug" {
		t.Fatalf("independent=%#v", independent)
	}
}
