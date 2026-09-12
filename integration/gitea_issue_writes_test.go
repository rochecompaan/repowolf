//go:build linux && gitea_integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
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
	issuePath := "/api/v1/repos/CanonicalOwner/CanonicalRepo/issues"
	createBefore := fixture.requestCount(t, http.MethodPost, issuePath)
	created := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "write integration", "-d", "private write body", "-a", "CanonicalOwner", "-L", "bug", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPost, issuePath) - createBefore; got != 1 {
		t.Fatalf("create request count=%d", got)
	}
	var issue struct {
		Index int64  `json:"index"`
		State string `json:"state"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(created, &issue); err != nil || issue.Index <= 0 || issue.State != "open" || issue.Title != "write integration" {
		t.Fatalf("create=%s err=%v", created, err)
	}
	index := strconv.FormatInt(issue.Index, 10)
	commentPath := issuePath + "/" + index + "/comments"
	commentBefore := fixture.requestCount(t, http.MethodPost, commentPath)
	comment := runTeaArgs(t, service.binaries.Tea, service.environment, "comments", "add", index, "private comment body", "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPost, commentPath) - commentBefore; got != 1 {
		t.Fatalf("comment request count=%d", got)
	}
	var commentValue struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(comment, &commentValue); err != nil || commentValue.ID <= 0 || commentValue.Body != "private comment body" {
		t.Fatalf("comment=%s err=%v", comment, err)
	}
	statePath := issuePath + "/" + index
	stateBefore := fixture.requestCount(t, http.MethodPatch, statePath)
	closed := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if err := json.Unmarshal(closed, &issue); err != nil || issue.State != "closed" {
		t.Fatalf("close=%s err=%v", closed, err)
	}
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 1 {
		t.Fatalf("first close PATCH count=%d", got)
	}
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "close", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 1 {
		t.Fatalf("no-op close changed PATCH count to %d", got)
	}
	reopened := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "reopen", index, "-r", "CanonicalOwner/CanonicalRepo", "-o", "json")
	if got := fixture.requestCount(t, http.MethodPatch, statePath) - stateBefore; got != 2 {
		t.Fatalf("reopen total PATCH count=%d", got)
	}
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

	auditContents, err := os.ReadFile(service.server.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := parseAuditRecords(auditContents, []string{"private write body", "private comment body", "bug", "CanonicalOwner", fixture.token, `"index"`})
	if err != nil {
		t.Fatal(err)
	}
	operations := []string{"gitea.issue_create", "gitea.issue_comment", "gitea.issue_close", "gitea.issue_close", "gitea.issue_reopen"}
	if len(records) != len(operations)*2 {
		t.Fatalf("audit record count=%d", len(records))
	}
	wantTransitions := []*bool{nil, nil, boolPointer(true), boolPointer(false), boolPointer(true)}
	for invocation, operation := range operations {
		accepted, terminal := records[invocation*2], records[invocation*2+1]
		if accepted.event.RequestID == "" || accepted.event.RequestID != terminal.event.RequestID || accepted.event.Operation != operation || terminal.event.Operation != operation || accepted.event.Outcome != "accepted" || terminal.event.Outcome != "completed" || terminal.event.Reason != "OK" {
			t.Fatalf("audit invocation %d: accepted=%#v terminal=%#v", invocation, accepted.event, terminal.event)
		}
		want := wantTransitions[invocation]
		if (want == nil) != (terminal.event.Transitioned == nil) || want != nil && *want != *terminal.event.Transitioned {
			t.Fatalf("audit invocation %d transitioned=%v want=%v", invocation, terminal.event.Transitioned, want)
		}
	}
}

func boolPointer(value bool) *bool { return &value }
