package gitea

import (
	"context"
	"errors"
	"testing"

	sdk "gitea.dev/sdk"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type editCatalogAPI struct {
	fakeIssueAPI
	users                 []*sdk.User
	labels                map[int][]*sdk.Label
	userCalls, labelCalls int
}

func (f *editCatalogAPI) GetAssignees(context.Context, string, string) ([]*sdk.User, error) {
	f.userCalls++
	return f.users, nil
}
func (f *editCatalogAPI) ListRepoLabels(_ context.Context, _, _ string, option sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.labelCalls++
	return f.labels[option.Page], nil
}

func TestCollectEditCatalogsOnlyWhenNeeded(t *testing.T) {
	api := &editCatalogAPI{users: []*sdk.User{{ID: 1, UserName: "alice"}}, labels: map[int][]*sdk.Label{1: {{ID: 9, Name: "bug"}}}}
	adapter, _ := newRepositoryAdapter(api)
	issue := &normalizedIssue{labels: []string{"old"}}
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{assigneeMode: editAdd, assignees: []string{"alice"}, labelMode: editAdd, labels: []string{"bug"}}, issue)
	if err != nil || catalogs.assignees["alice"] != 1 || catalogs.labels["bug"] != 9 || api.userCalls != 1 || api.labelCalls != 1 {
		t.Fatalf("catalogs=%#v calls=%d/%d err=%v", catalogs, api.userCalls, api.labelCalls, err)
	}
	api.userCalls, api.labelCalls = 0, 0
	_, err = adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{assigneeMode: editRemove, assignees: []string{"absent"}, labelMode: editRemove, labels: []string{"absent"}}, issue)
	if err != nil || api.userCalls != 0 || api.labelCalls != 0 {
		t.Fatalf("unneeded reads=%d/%d err=%v", api.userCalls, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsRetainsRemoveLabelForReconciliation(t *testing.T) {
	title := "new"
	api := &editCatalogAPI{labels: map[int][]*sdk.Label{1: {{ID: 9, Name: "stale"}}}}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{title: &title, labelMode: editRemove, labels: []string{"stale"}}, &normalizedIssue{title: "old"})
	if err != nil || catalogs.labels["stale"] != 9 || api.labelCalls != 1 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsRetainsAllRemoveLabelsWhenOneMayWrite(t *testing.T) {
	api := &editCatalogAPI{labels: map[int][]*sdk.Label{1: {{ID: 9, Name: "present"}, {ID: 10, Name: "absent"}}}}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{labelMode: editRemove, labels: []string{"present", "absent"}}, &normalizedIssue{labels: []string{"present"}})
	if err != nil || catalogs.labels["present"] != 9 || catalogs.labels["absent"] != 10 || api.labelCalls != 1 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsRejectsMissingTarget(t *testing.T) {
	api := &editCatalogAPI{}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{assigneeMode: editAdd, assignees: []string{"alice"}}, &normalizedIssue{})
	if !errors.Is(err, rpcstatus.ErrFailedPrecondition) {
		t.Fatalf("err=%v", err)
	}
}
