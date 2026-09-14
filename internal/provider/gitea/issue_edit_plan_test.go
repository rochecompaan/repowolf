package gitea

import (
	"reflect"
	"testing"
)

func TestEditSatisfied(t *testing.T) {
	title, body := "new", ""
	intent := editIntent{title: &title, body: &body, assigneeMode: editSet, assignees: []string{"bob", "alice"}, labelMode: editAdd, labels: []string{"bug"}}
	issue := &normalizedIssue{title: "new", body: "", assignees: []string{"alice", "bob"}, labels: []string{"other", "bug"}}
	if !editSatisfied(intent, issue) {
		t.Fatal("satisfying snapshot rejected")
	}
	issue.assignees = append(issue.assignees, "carol")
	if editSatisfied(intent, issue) {
		t.Fatal("set accepted unrelated assignee")
	}
}

func TestPlanIssueEdit(t *testing.T) {
	body := "body"
	intent := editIntent{body: &body, assigneeMode: editSet, assignees: []string{"bob", "carol"}, labelMode: editRemove, labels: []string{"stale"}}
	issue := &normalizedIssue{title: "title", body: "old", assignees: []string{"dave", "bob", "alice"}, labels: []string{"stale", "keep"}, labelIDs: map[string]int64{"stale": 9, "keep": 10}}
	plan, err := planIssueEdit(intent, issue, editCatalogs{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.text == nil || plan.text.title != "title" || plan.text.body == nil || *plan.text.body != "body" {
		t.Fatalf("text: %#v", plan.text)
	}
	if !reflect.DeepEqual(plan.removeAssignees, []string{"alice", "dave"}) || !reflect.DeepEqual(plan.addAssignees, []string{"carol"}) || !reflect.DeepEqual(plan.removeLabelIDs, []int64{9}) {
		t.Fatalf("plan: %#v", plan)
	}
}

func TestPlanIssueEditUsesCatalogIdentityForInitialLabelRemoval(t *testing.T) {
	intent := editIntent{labelMode: editRemove, labels: []string{"stale"}}
	issue := &normalizedIssue{labels: []string{"stale"}, labelIDs: map[string]int64{"stale": 9}}
	plan, err := planIssueEdit(intent, issue, editCatalogs{labels: map[string]int64{"stale": 10}})
	if err != nil || !reflect.DeepEqual(plan.removeLabelIDs, []int64{10}) {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}

func TestPlanIssueEditNoOp(t *testing.T) {
	title := "title"
	plan, err := planIssueEdit(editIntent{title: &title}, &normalizedIssue{title: "title"}, editCatalogs{})
	if err != nil || !plan.empty() {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
}
