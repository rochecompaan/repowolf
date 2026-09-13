package gitea

import (
	"context"
	"reflect"
	"testing"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

type issueEditAPI struct {
	fakeIssueAPI
	reads                                               []*sdk.Issue
	read                                                int
	calls                                               []string
	users                                               []*sdk.User
	labels                                              []*sdk.Label
	editResult, deleteAssigneeResult, addAssigneeResult *sdk.Issue
	addLabelResult                                      []*sdk.Label
}

func (f *issueEditAPI) GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error) {
	value := f.reads[f.read]
	f.read++
	f.calls = append(f.calls, "get")
	return value, 200, nil
}
func (f *issueEditAPI) GetAssignees(context.Context, string, string) ([]*sdk.User, error) {
	f.calls = append(f.calls, "users")
	return f.users, nil
}
func (f *issueEditAPI) ListRepoLabels(context.Context, string, string, sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.calls = append(f.calls, "labels")
	return f.labels, nil
}
func (f *issueEditAPI) EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "text")
	return f.editResult, nil
}
func (f *issueEditAPI) DeleteIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "remove-assignees")
	return f.deleteAssigneeResult, nil
}
func (f *issueEditAPI) AddIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "add-assignees")
	return f.addAssigneeResult, nil
}
func (f *issueEditAPI) AddIssueLabels(context.Context, string, string, int64, sdk.IssueLabelsOption) ([]*sdk.Label, error) {
	f.calls = append(f.calls, "add-labels")
	return f.addLabelResult, nil
}
func (f *issueEditAPI) DeleteIssueLabel(context.Context, string, string, int64, int64) error {
	f.calls = append(f.calls, "remove-label")
	return nil
}

func editSDKIssue(title, body string, assignees []string, labels []string) *sdk.Issue {
	issue := sdkIssue()
	issue.Title, issue.Body = title, body
	for i, name := range assignees {
		issue.Assignees = append(issue.Assignees, &sdk.User{ID: int64(i + 10), UserName: name})
	}
	for i, name := range labels {
		issue.Labels = append(issue.Labels, &sdk.Label{ID: int64(i + 20), Name: name})
	}
	return issue
}

func TestIssueEditNoOp(t *testing.T) {
	issue := editSDKIssue("title", "body", nil, nil)
	api := &issueEditAPI{reads: []*sdk.Issue{issue}}
	adapter, _ := newRepositoryAdapter(api)
	title := "title"
	response, err := adapter.issueEdit(context.Background(), issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
	if err != nil || response.GetIssueEdit().GetIssue().Title != "title" || !reflect.DeepEqual(api.calls, []string{"get"}) {
		t.Fatalf("calls=%v err=%v response=%#v", api.calls, err, response)
	}
}

func TestIssueEditExecutionOrder(t *testing.T) {
	initial := editSDKIssue("old", "body", []string{"old"}, []string{"stale"})
	text := editSDKIssue("new", "body", []string{"old"}, []string{"stale"})
	removed := editSDKIssue("new", "body", nil, []string{"stale"})
	added := editSDKIssue("new", "body", []string{"alice"}, []string{"stale"})
	final := editSDKIssue("new", "body", []string{"alice"}, []string{"bug"})
	api := &issueEditAPI{reads: []*sdk.Issue{initial, final}, users: []*sdk.User{{ID: 1, UserName: "alice"}}, labels: []*sdk.Label{{ID: 20, Name: "stale"}, {ID: 21, Name: "bug"}}, editResult: text, deleteAssigneeResult: removed, addAssigneeResult: added, addLabelResult: []*sdk.Label{{ID: 20, Name: "stale"}, {ID: 21, Name: "bug"}}}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{SetAssignees: &repowolfv1.GiteaStringList{Values: []string{"alice"}}}, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: []string{"bug"}}}}
	response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	want := []string{"get", "users", "labels", "text", "remove-assignees", "add-assignees", "add-labels", "get"}
	if err != nil || !reflect.DeepEqual(api.calls, want) || response.GetIssueEdit().GetIssue().Title != "new" {
		t.Fatalf("calls=%v want=%v err=%v", api.calls, want, err)
	}
}
