//go:build linux && gitea_integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
)

func TestRestrictedTeaIssueEditAgainstGitea(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.13.2", "172.29.13.0/24")
	fixture.createAssignableUser(t, "SecondAssignee")
	for _, name := range []string{"bug", "urgent"} {
		giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/labels", fixture.token, map[string]any{"name": name, "color": "ee0701"}, nil, "", "")
	}
	service := fixture.startBroker(t)
	defer service.server.Stop(t)
	created := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "create", "-r", "CanonicalOwner/CanonicalRepo", "-t", "before edit", "-d", "old body", "-a", "CanonicalOwner", "-L", "bug", "-o", "json")
	var output struct {
		Index int64  `json:"index"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(created, &output); err != nil {
		t.Fatal(err)
	}
	index := strconv.FormatInt(output.Index, 10)
	before := captureIssueEditProviderCounts(t, fixture, index)
	edited := runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--title", "edited title", "--description", "edited body", "--set-assignees", "CanonicalOwner", "-L", "urgent", "-o", "json")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	if err := json.Unmarshal(edited, &output); err != nil || output.Title != "edited title" {
		t.Fatalf("edit=%s err=%v", edited, err)
	}
	before = captureIssueEditProviderCounts(t, fixture, index)
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "e", index, "-r", "CanonicalOwner/CanonicalRepo", "--description", "", "--add-assignees", "SecondAssignee", "--remove-labels", "bug", "-o", "table")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	before = captureIssueEditProviderCounts(t, fixture, index)
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--remove-assignees", "CanonicalOwner", "-o", "simple")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	var issue struct {
		Title     string `json:"title"`
		Body      string `json:"body"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &issue, "", "")
	if issue.Title != "edited title" || issue.Body != "" || len(issue.Assignees) != 1 || issue.Assignees[0].Login != "SecondAssignee" || len(issue.Labels) != 1 || issue.Labels[0].Name != "urgent" {
		t.Fatalf("final issue=%#v", issue)
	}
}

func TestGiteaIssueEditConcurrentLabelReconciliation(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.14.2", "172.29.14.0/24")
	var label struct {
		ID int64 `json:"id"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/labels", fixture.token, map[string]any{"name": "stale", "color": "ee0701"}, &label, "", "")
	var issue struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", fixture.token, map[string]any{"title": "before"}, &issue, "", "")
	index, labelID := strconv.FormatInt(issue.Index, 10), strconv.FormatInt(label.ID, 10)
	proxyAddress := fixture.startIssueEditProxy(t, "concurrent-label", index, labelID)
	service := fixture.startBrokerAt(t, proxyAddress)
	defer service.server.Stop(t)

	before := captureIssueEditProviderCounts(t, fixture, index)
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--title", "after", "--remove-labels", "stale", "-o", "json")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	var final struct {
		Title  string `json:"title"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &final, "", "")
	if final.Title != "after" || len(final.Labels) != 0 {
		t.Fatalf("final issue=%#v", final)
	}
}

func TestGiteaIssueEditExactAssigneeReconciliation(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.16.2", "172.29.16.0/24")
	fixture.createAssignableUser(t, "SecondAssignee")
	var issue struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", fixture.token, map[string]any{"title": "before", "assignees": []string{"CanonicalOwner"}}, &issue, "", "")
	index := strconv.FormatInt(issue.Index, 10)
	proxyAddress := fixture.startIssueEditProxy(t, "concurrent-assignee", index, "")
	service := fixture.startBrokerAt(t, proxyAddress)
	defer service.server.Stop(t)

	before := captureIssueEditProviderCounts(t, fixture, index)
	runTeaArgs(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--title", "after", "--set-assignees", "CanonicalOwner", "-o", "json")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	var final struct {
		Title     string `json:"title"`
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &final, "", "")
	if final.Title != "after" || len(final.Assignees) != 1 || final.Assignees[0].Login != "CanonicalOwner" {
		t.Fatalf("final issue=%#v", final)
	}
}

func TestGiteaIssueEditUnknownIsNotRetried(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.17.2", "172.29.17.0/24")
	var issue struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", fixture.token, map[string]any{"title": "before"}, &issue, "", "")
	index := strconv.FormatInt(issue.Index, 10)
	proxyAddress := fixture.startIssueEditProxy(t, "fail-first-text-before-upstream", index, "")
	service := fixture.startBrokerAt(t, proxyAddress)
	defer service.server.Stop(t)

	before := captureIssueEditProviderCounts(t, fixture, index)
	stdout, stderr := runTeaFailure(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--title", "after", "-o", "json")
	assertIssueEditProviderBounds(t, before, captureIssueEditProviderCounts(t, fixture, index))
	if len(stdout) != 0 || stderr != "tea: write outcome unknown; inspect repository state before retrying\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	var final struct {
		Title string `json:"title"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &final, "", "")
	if final.Title != "before" {
		t.Fatalf("unexpected retry changed issue: %#v", final)
	}
	auditContents, err := os.ReadFile(service.server.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := parseAuditRecords(auditContents, []string{"before", "after", fixture.token, `"index"`})
	if err != nil || len(records) != 2 || records[1].event.Operation != "gitea.issue_edit" || records[1].event.Outcome != "unknown" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestGiteaIssueEditPartialAfterConfirmedWrite(t *testing.T) {
	fixture := newRestrictedGiteaFixture(t, "172.29.15.2", "172.29.15.0/24")
	var issue struct {
		Index int64 `json:"number"`
	}
	giteaJSON(t, fixture.client, http.MethodPost, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues", fixture.token, map[string]any{"title": "before"}, &issue, "", "")
	index := strconv.FormatInt(issue.Index, 10)
	proxyAddress := fixture.startIssueEditProxy(t, "fail-read-after-text", index, "")
	service := fixture.startBrokerAt(t, proxyAddress)
	defer service.server.Stop(t)

	stdout, stderr := runTeaFailure(t, service.binaries.Tea, service.environment, "issues", "edit", index, "-r", "CanonicalOwner/CanonicalRepo", "--title", "after", "-o", "json")
	if len(stdout) != 0 || stderr != "tea: issue edit partially applied; inspect issue state before retrying\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	var final struct {
		Title string `json:"title"`
	}
	giteaJSON(t, fixture.client, http.MethodGet, fixture.baseURL+"/api/v1/repos/CanonicalOwner/CanonicalRepo/issues/"+index, fixture.token, nil, &final, "", "")
	if final.Title != "after" {
		t.Fatalf("final issue=%#v", final)
	}
	auditContents, err := os.ReadFile(service.server.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	records, err := parseAuditRecords(auditContents, []string{"before", "after", fixture.token, `"index"`})
	if err != nil || len(records) != 2 || records[0].event.Operation != "gitea.issue_edit" || records[0].event.Outcome != "accepted" || records[1].event.Operation != "gitea.issue_edit" || records[1].event.Outcome != "partial" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

type issueEditProviderCounts map[string]int

func captureIssueEditProviderCounts(t *testing.T, fixture *restrictedGiteaFixture, index string) issueEditProviderCounts {
	t.Helper()
	repositoryPath := "/api/v1/repos/CanonicalOwner/CanonicalRepo"
	issuePath := repositoryPath + "/issues/" + index
	return issueEditProviderCounts{
		"issue reads":        fixture.requestCount(t, http.MethodGet, issuePath),
		"assignee catalogs":  fixture.requestCount(t, http.MethodGet, repositoryPath+"/assignees"),
		"label catalogs":     fixture.requestCount(t, http.MethodGet, repositoryPath+"/labels"),
		"text patches":       fixture.requestCount(t, http.MethodPatch, issuePath),
		"assignee removals":  fixture.requestCount(t, http.MethodDelete, issuePath+"/assignees"),
		"assignee additions": fixture.requestCount(t, http.MethodPost, issuePath+"/assignees"),
		"label additions":    fixture.requestCount(t, http.MethodPost, issuePath+"/labels"),
		"label removals":     fixture.requestCountPrefix(t, http.MethodDelete, issuePath+"/labels/", false),
	}
}

func assertIssueEditProviderBounds(t *testing.T, before, after issueEditProviderCounts) {
	t.Helper()
	limits := issueEditProviderCounts{
		"issue reads": 3, "assignee catalogs": 1, "label catalogs": 21,
		"text patches": 2, "assignee removals": 2, "assignee additions": 2,
		"label additions": 2, "label removals": 50,
	}
	for operation, limit := range limits {
		calls := after[operation] - before[operation]
		if calls < 0 || calls > limit {
			t.Fatalf("%s calls=%d, want 0..%d", operation, calls, limit)
		}
	}
	if calls := after["issue reads"] - before["issue reads"]; calls == 0 {
		t.Fatal("issue edit made no preflight read")
	}
}
