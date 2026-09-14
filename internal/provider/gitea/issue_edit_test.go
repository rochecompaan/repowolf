package gitea

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type issueEditAPI struct {
	fakeIssueAPI
	reads                                               []*sdk.Issue
	readErrors                                          []error
	read                                                int
	calls                                               []string
	users                                               []*sdk.User
	labels                                              []*sdk.Label
	labelPages                                          map[int][]*sdk.Label
	editResult, deleteAssigneeResult, addAssigneeResult *sdk.Issue
	deleteAssigneeResults                               []*sdk.Issue
	deleteAssigneeCalls                                 int
	editErr, deleteAssigneeErr, addAssigneeErr          error
	editHook                                            func()
	addLabelResult                                      []*sdk.Label
	addLabelErr, deleteLabelErr                         error
	deletedAssignees                                    [][]string
	addedAssignees                                      [][]string
	addedLabelIDs, deletedLabelIDs                      []int64
}

func (f *issueEditAPI) GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error) {
	value := f.reads[f.read]
	var err error
	if f.read < len(f.readErrors) {
		err = f.readErrors[f.read]
	}
	f.read++
	f.calls = append(f.calls, "get")
	return value, 200, err
}
func (f *issueEditAPI) GetAssignees(context.Context, string, string) ([]*sdk.User, error) {
	f.calls = append(f.calls, "users")
	return f.users, nil
}
func (f *issueEditAPI) ListRepoLabels(_ context.Context, _, _ string, option sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.calls = append(f.calls, "labels")
	if f.labelPages != nil {
		return f.labelPages[option.Page], nil
	}
	return f.labels, nil
}
func (f *issueEditAPI) EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "text")
	if f.editHook != nil {
		f.editHook()
	}
	return f.editResult, f.editErr
}
func (f *issueEditAPI) DeleteIssueAssignees(_ context.Context, _, _ string, _ int64, option sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "remove-assignees")
	f.deletedAssignees = append(f.deletedAssignees, append([]string(nil), option.Assignees...))
	if f.deleteAssigneeErr != nil {
		return nil, f.deleteAssigneeErr
	}
	if f.deleteAssigneeCalls < len(f.deleteAssigneeResults) {
		result := f.deleteAssigneeResults[f.deleteAssigneeCalls]
		f.deleteAssigneeCalls++
		return result, nil
	}
	return f.deleteAssigneeResult, nil
}
func (f *issueEditAPI) AddIssueAssignees(_ context.Context, _, _ string, _ int64, option sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	f.calls = append(f.calls, "add-assignees")
	f.addedAssignees = append(f.addedAssignees, append([]string(nil), option.Assignees...))
	return f.addAssigneeResult, f.addAssigneeErr
}
func (f *issueEditAPI) AddIssueLabels(_ context.Context, _, _ string, _ int64, option sdk.IssueLabelsOption) ([]*sdk.Label, error) {
	f.calls = append(f.calls, "add-labels")
	f.addedLabelIDs = append(f.addedLabelIDs, option.Labels...)
	return f.addLabelResult, f.addLabelErr
}
func (f *issueEditAPI) DeleteIssueLabel(_ context.Context, _, _ string, _ int64, labelID int64) error {
	f.calls = append(f.calls, "remove-label")
	f.deletedLabelIDs = append(f.deletedLabelIDs, labelID)
	return f.deleteLabelErr
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

func TestIssueEditReconcilesOnce(t *testing.T) {
	old := editSDKIssue("old", "body", nil, nil)
	updated := editSDKIssue("new", "body", nil, nil)
	api := &issueEditAPI{reads: []*sdk.Issue{old, old, updated}, editResult: updated}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	_, err := adapter.issueEdit(context.Background(), issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
	want := []string{"get", "text", "get", "text", "get"}
	if err != nil || !reflect.DeepEqual(api.calls, want) {
		t.Fatalf("calls=%v err=%v", api.calls, err)
	}
}

func TestIssueEditClassifiesPartialAndUnknown(t *testing.T) {
	old := editSDKIssue("old", "body", nil, nil)
	updated := editSDKIssue("new", "body", nil, nil)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title}
	t.Run("confirmed then final read failure is partial", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, nil}, readErrors: []error{nil, errors.New("read")}, editResult: updated}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		if !errors.Is(err, rpcstatus.ErrEditPartial) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("uncertain call remains unknown when recovery is unsatisfied", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, old}, editErr: errors.New("write")}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || !reflect.DeepEqual(api.calls, []string{"get", "text", "get"}) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
	t.Run("recovery read may prove success", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, updated}, editErr: errors.New("write")}
		adapter, _ := newRepositoryAdapter(api)
		response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		if err != nil || response.GetIssueEdit().GetIssue().Title != "new" {
			t.Fatalf("response=%#v err=%v", response, err)
		}
	})
}

func TestIssueEditReconcilesConcurrentLabelRemoval(t *testing.T) {
	old := editSDKIssue("old", "body", nil, nil)
	textEdited := editSDKIssue("new", "body", nil, nil)
	concurrent := editSDKIssue("new", "body", nil, []string{"stale"})
	final := editSDKIssue("new", "body", nil, nil)
	api := &issueEditAPI{
		reads:      []*sdk.Issue{old, concurrent, final},
		editResult: textEdited,
	}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{"stale"}}}}
	response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	want := []string{"get", "text", "get", "remove-label", "get"}
	if err != nil || !reflect.DeepEqual(api.calls, want) || !reflect.DeepEqual(api.deletedLabelIDs, []int64{20}) || len(response.GetIssueEdit().GetIssue().Labels) != 0 {
		t.Fatalf("calls=%v deleted=%v response=%#v err=%v", api.calls, api.deletedLabelIDs, response, err)
	}
}

func TestIssueEditReconciliationUsesFreshLabelIdentity(t *testing.T) {
	initial := editSDKIssue("old", "body", nil, []string{"stale"})
	textEdited := editSDKIssue("new", "body", nil, []string{"stale"})
	concurrent := editSDKIssue("new", "body", nil, []string{"stale"})
	concurrent.Labels[0].ID = 42
	final := editSDKIssue("new", "body", nil, nil)
	api := &issueEditAPI{
		reads:      []*sdk.Issue{initial, concurrent, final},
		labels:     []*sdk.Label{{ID: 20, Name: "stale"}},
		editResult: textEdited,
	}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{"stale"}}}}
	response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	wantCalls := []string{"get", "labels", "text", "remove-label", "get", "remove-label", "get"}
	if err != nil || !reflect.DeepEqual(api.calls, wantCalls) || !reflect.DeepEqual(api.deletedLabelIDs, []int64{20, 42}) || len(response.GetIssueEdit().GetIssue().Labels) != 0 {
		t.Fatalf("calls=%v deleted=%v response=%#v err=%v", api.calls, api.deletedLabelIDs, response, err)
	}
}

func TestIssueEditReconcilesConcurrentRemovalOfRequestedAddLabel(t *testing.T) {
	initial := editSDKIssue("old", "body", nil, []string{"bug"})
	textEdited := editSDKIssue("new", "body", nil, []string{"bug"})
	concurrent := editSDKIssue("new", "body", nil, nil)
	final := editSDKIssue("new", "body", nil, []string{"bug"})
	api := &issueEditAPI{
		reads:          []*sdk.Issue{initial, concurrent, final},
		editResult:     textEdited,
		addLabelResult: []*sdk.Label{{ID: 20, Name: "bug"}},
	}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: []string{"bug"}}}}
	response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	want := []string{"get", "text", "get", "add-labels", "get"}
	if err != nil || !reflect.DeepEqual(api.calls, want) || !reflect.DeepEqual(api.addedLabelIDs, []int64{20}) || !reflect.DeepEqual(response.GetIssueEdit().GetIssue().Labels, []string{"bug"}) {
		t.Fatalf("calls=%v added=%v response=%#v err=%v", api.calls, api.addedLabelIDs, response, err)
	}
}

func TestIssueEditTitleWithAbsentRemovedLabelDoesNotReadCatalog(t *testing.T) {
	initial := editSDKIssue("old", "body", nil, nil)
	final := editSDKIssue("new", "body", nil, nil)
	api := &issueEditAPI{reads: []*sdk.Issue{initial, final}, editResult: final}
	adapter, _ := newRepositoryAdapter(api)
	title := "new"
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{"does-not-exist"}}}}
	response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	if err != nil || !reflect.DeepEqual(api.calls, []string{"get", "text", "get"}) || response.GetIssueEdit().GetIssue().Title != "new" {
		t.Fatalf("calls=%v response=%#v err=%v", api.calls, response, err)
	}
}

func TestIssueEditConcurrentCollectionSemantics(t *testing.T) {
	t.Run("add assignee preserves unrelated", func(t *testing.T) {
		initial := editSDKIssue("title", "body", []string{"alice"}, nil)
		final := editSDKIssue("title", "body", []string{"alice", "bob"}, nil)
		api := &issueEditAPI{reads: []*sdk.Issue{initial, final}, users: []*sdk.User{{ID: 1, UserName: "bob"}}, addAssigneeResult: final}
		adapter, _ := newRepositoryAdapter(api)
		request := &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_AddAssignees{AddAssignees: &repowolfv1.GiteaStringList{Values: []string{"bob"}}}}
		response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		got := response.GetIssueEdit().GetIssue().Assignees
		if err != nil || len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
			t.Fatalf("response=%#v err=%v", response, err)
		}
	})
	t.Run("remove label preserves unrelated concurrent label", func(t *testing.T) {
		initial := editSDKIssue("title", "body", nil, []string{"stale", "keep"})
		final := editSDKIssue("title", "body", nil, []string{"keep", "concurrent"})
		api := &issueEditAPI{reads: []*sdk.Issue{initial, final}, labels: []*sdk.Label{{ID: 20, Name: "stale"}, {ID: 21, Name: "keep"}, {ID: 22, Name: "concurrent"}}}
		adapter, _ := newRepositoryAdapter(api)
		request := &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{"stale"}}}}
		response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		got := response.GetIssueEdit().GetIssue().Labels
		wantCalls := []string{"get", "labels", "remove-label", "get"}
		if err != nil || !reflect.DeepEqual(api.calls, wantCalls) || len(got) != 2 || got[0] != "keep" || got[1] != "concurrent" {
			t.Fatalf("calls=%v response=%#v err=%v", api.calls, response, err)
		}
	})
	t.Run("set assignees removes concurrent member", func(t *testing.T) {
		initial := editSDKIssue("title", "body", []string{"old"}, nil)
		removed := editSDKIssue("title", "body", nil, nil)
		added := editSDKIssue("title", "body", []string{"alice"}, nil)
		concurrent := editSDKIssue("title", "body", []string{"alice", "carol"}, nil)
		final := editSDKIssue("title", "body", []string{"alice"}, nil)
		api := &issueEditAPI{
			reads:                 []*sdk.Issue{initial, concurrent, final},
			users:                 []*sdk.User{{ID: 1, UserName: "alice"}},
			deleteAssigneeResults: []*sdk.Issue{removed, final},
			addAssigneeResult:     added,
		}
		adapter, _ := newRepositoryAdapter(api)
		request := &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{SetAssignees: &repowolfv1.GiteaStringList{Values: []string{"alice"}}}}
		response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		got := response.GetIssueEdit().GetIssue().Assignees
		if err != nil || !reflect.DeepEqual(api.deletedAssignees, [][]string{{"old"}, {"carol"}}) || len(got) != 1 || got[0] != "alice" {
			t.Fatalf("removed=%v response=%#v err=%v", api.deletedAssignees, response, err)
		}
	})
}

func TestIssueEditRemovalBounds(t *testing.T) {
	names := make([]string, 25)
	catalogPages := make(map[int][]*sdk.Label, 21)
	for page := 1; page <= 20; page++ {
		values := make([]*sdk.Label, 50)
		for offset := range values {
			id := int64((page-1)*50 + offset + 1)
			name := fmt.Sprintf("catalog-%04d", id)
			if page == 1 && offset < len(names) {
				name = fmt.Sprintf("remove-%02d", offset)
				names[offset] = name
			}
			values[offset] = &sdk.Label{ID: id, Name: name}
		}
		catalogPages[page] = values
	}
	initial := editSDKIssue("title", "body", nil, names)
	for i := range initial.Labels {
		initial.Labels[i].ID = int64(i + 1)
	}
	final := editSDKIssue("title", "body", nil, nil)
	api := &issueEditAPI{reads: []*sdk.Issue{initial, initial, final}, labelPages: catalogPages}
	adapter, _ := newRepositoryAdapter(api)
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: names}}}
	_, err := adapter.issueEdit(context.Background(), issueResolved(), request)
	counts := map[string]int{}
	for _, call := range api.calls {
		counts[call]++
	}
	if err != nil || counts["get"] != 3 || counts["labels"] != 21 || counts["remove-label"] != 50 || len(api.calls) != 74 {
		t.Fatalf("counts=%v total=%d err=%v", counts, len(api.calls), err)
	}
}

func TestIssueEditRemoveLabelCatalogFailuresMakeZeroWrites(t *testing.T) {
	request := &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{"stale"}}}}
	tests := []struct {
		name   string
		labels []*sdk.Label
	}{
		{name: "malformed unrelated entry", labels: []*sdk.Label{{ID: 20, Name: "stale"}, {ID: 0, Name: "broken"}}},
		{name: "catalog identity disagrees with issue", labels: []*sdk.Label{{ID: 21, Name: "stale"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := editSDKIssue("title", "body", nil, []string{"stale"})
			api := &issueEditAPI{reads: []*sdk.Issue{initial}, labels: test.labels}
			adapter, _ := newRepositoryAdapter(api)
			response, err := adapter.issueEdit(context.Background(), issueResolved(), request)
			if response != nil || !errors.Is(err, rpcstatus.ErrProviderFailure) || !reflect.DeepEqual(api.calls, []string{"get", "labels"}) || len(api.deletedLabelIDs) != 0 {
				t.Fatalf("calls=%v deleted=%v response=%#v err=%v", api.calls, api.deletedLabelIDs, response, err)
			}
		})
	}
}

func TestIssueEditFailureBoundaries(t *testing.T) {
	old := editSDKIssue("old", "body", nil, nil)
	updated := editSDKIssue("new", "body", nil, nil)
	title := "new"
	t.Run("preflight failure performs no write", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{nil}, readErrors: []error{errors.New("secret")}}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(context.Background(), issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
		if err == nil || !reflect.DeepEqual(api.calls, []string{"get"}) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
	t.Run("invalid write response is unknown and not retried", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, old}}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(context.Background(), issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || !reflect.DeepEqual(api.calls, []string{"get", "text", "get"}) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
	t.Run("later uncertain write takes precedence over confirmed effect", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, old}, labels: []*sdk.Label{{ID: 20, Name: "bug"}}, editResult: updated, addLabelErr: errors.New("secret")}
		adapter, _ := newRepositoryAdapter(api)
		request := &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: []string{"bug"}}}}
		_, err := adapter.issueEdit(context.Background(), issueResolved(), request)
		want := []string{"get", "labels", "text", "add-labels", "get"}
		if !errors.Is(err, rpcstatus.ErrWriteOutcomeUnknown) || !reflect.DeepEqual(api.calls, want) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
	t.Run("cancellation after confirmed write is partial", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		api := &issueEditAPI{reads: []*sdk.Issue{old}, editResult: updated, editHook: cancel}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(ctx, issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
		if !errors.Is(err, rpcstatus.ErrEditPartial) || !reflect.DeepEqual(api.calls, []string{"get", "text"}) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
	t.Run("persistent contention stops after second final read", func(t *testing.T) {
		api := &issueEditAPI{reads: []*sdk.Issue{old, old, old}, editResult: updated}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueEdit(context.Background(), issueResolved(), &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: &title})
		want := []string{"get", "text", "get", "text", "get"}
		if !errors.Is(err, rpcstatus.ErrEditPartial) || !reflect.DeepEqual(api.calls, want) {
			t.Fatalf("calls=%v err=%v", api.calls, err)
		}
	})
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
