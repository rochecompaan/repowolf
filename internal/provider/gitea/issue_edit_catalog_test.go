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
	userErr, labelErr     error
	userCalls, labelCalls int
}

func (f *editCatalogAPI) GetAssignees(context.Context, string, string) ([]*sdk.User, error) {
	f.userCalls++
	return f.users, f.userErr
}
func (f *editCatalogAPI) ListRepoLabels(_ context.Context, _, _ string, option sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.labelCalls++
	return f.labels[option.Page], f.labelErr
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

func TestCollectEditCatalogsRetainsAllAddLabelsForReconciliation(t *testing.T) {
	title := "new"
	api := &editCatalogAPI{labels: map[int][]*sdk.Label{1: {{ID: 9, Name: "present"}, {ID: 10, Name: "missing"}}}}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{title: &title, labelMode: editAdd, labels: []string{"present", "missing"}}, &normalizedIssue{title: "old", labels: []string{"present"}, labelIDs: map[string]int64{"present": 9}})
	if err != nil || catalogs.labels["present"] != 9 || catalogs.labels["missing"] != 10 || api.labelCalls != 1 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsRetainsPresentAddLabelWithoutRead(t *testing.T) {
	title := "new"
	api := &editCatalogAPI{}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{title: &title, labelMode: editAdd, labels: []string{"present"}}, &normalizedIssue{title: "old", labels: []string{"present"}, labelIDs: map[string]int64{"present": 9}})
	if err != nil || catalogs.labels["present"] != 9 || api.labelCalls != 0 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsResolvesPresentRemoveLabels(t *testing.T) {
	title := "new"
	api := &editCatalogAPI{labels: map[int][]*sdk.Label{1: {{ID: 9, Name: "present"}}}}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{title: &title, labelMode: editRemove, labels: []string{"present", "absent"}}, &normalizedIssue{title: "old", labels: []string{"present"}, labelIDs: map[string]int64{"present": 9}})
	if err != nil || catalogs.labels["present"] != 9 || api.labelCalls != 1 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsSkipsAbsentRemoveLabels(t *testing.T) {
	api := &editCatalogAPI{}
	adapter, _ := newRepositoryAdapter(api)
	catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{labelMode: editRemove, labels: []string{"absent"}}, &normalizedIssue{})
	if err != nil || len(catalogs.labels) != 0 || api.labelCalls != 0 {
		t.Fatalf("catalogs=%#v calls=%d err=%v", catalogs, api.labelCalls, err)
	}
}

func TestCollectEditCatalogsRejectsInconsistentRemoveLabelIdentity(t *testing.T) {
	api := &editCatalogAPI{labels: map[int][]*sdk.Label{1: {{ID: 10, Name: "present"}}}}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{labelMode: editRemove, labels: []string{"present"}}, &normalizedIssue{labels: []string{"present"}, labelIDs: map[string]int64{"present": 9}})
	if !errors.Is(err, rpcstatus.ErrProviderFailure) || api.labelCalls != 1 {
		t.Fatalf("calls=%d err=%v", api.labelCalls, err)
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

func TestEditAssigneeCatalogRejectsMalformedCollections(t *testing.T) {
	tests := []struct {
		name  string
		users []*sdk.User
	}{
		{name: "nil user", users: []*sdk.User{nil}},
		{name: "non-positive id", users: []*sdk.User{{ID: 0, UserName: "alice"}}},
		{name: "empty username", users: []*sdk.User{{ID: 1}}},
		{name: "invalid username", users: []*sdk.User{{ID: 1, UserName: "alice\x00secret"}}},
		{name: "duplicate id", users: []*sdk.User{{ID: 1, UserName: "alice"}, {ID: 1, UserName: "bob"}}},
		{name: "duplicate username", users: []*sdk.User{{ID: 1, UserName: "alice"}, {ID: 2, UserName: "alice"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &editCatalogAPI{users: test.users}
			adapter, _ := newRepositoryAdapter(api)
			catalogs, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{assigneeMode: editAdd, assignees: []string{"alice"}}, &normalizedIssue{})
			if len(catalogs.assignees) != 0 || !errors.Is(err, rpcstatus.ErrProviderFailure) || api.userCalls != 1 || api.labelCalls != 0 {
				t.Fatalf("catalogs=%#v calls=%d/%d err=%v", catalogs, api.userCalls, api.labelCalls, err)
			}
		})
	}
}

func TestEditCatalogProviderFailuresAndCancellation(t *testing.T) {
	t.Run("assignee provider failure", func(t *testing.T) {
		api := &editCatalogAPI{userErr: errors.New("provider secret")}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{assigneeMode: editAdd, assignees: []string{"alice"}}, &normalizedIssue{})
		if !errors.Is(err, rpcstatus.ErrProviderFailure) || api.userCalls != 1 || api.labelCalls != 0 {
			t.Fatalf("calls=%d/%d err=%v", api.userCalls, api.labelCalls, err)
		}
	})
	t.Run("label provider failure", func(t *testing.T) {
		api := &editCatalogAPI{labelErr: errors.New("provider secret"), labels: map[int][]*sdk.Label{}}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.collectEditCatalogs(context.Background(), "Owner", "Repo", editIntent{labelMode: editAdd, labels: []string{"bug"}}, &normalizedIssue{})
		if !errors.Is(err, rpcstatus.ErrProviderFailure) || api.userCalls != 0 || api.labelCalls != 1 {
			t.Fatalf("calls=%d/%d err=%v", api.userCalls, api.labelCalls, err)
		}
	})
	t.Run("cancelled before collection", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		api := &editCatalogAPI{users: []*sdk.User{{ID: 1, UserName: "alice"}}}
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.collectEditCatalogs(ctx, "Owner", "Repo", editIntent{assigneeMode: editAdd, assignees: []string{"alice"}}, &normalizedIssue{})
		if !errors.Is(err, context.Canceled) || api.userCalls != 0 || api.labelCalls != 0 {
			t.Fatalf("calls=%d/%d err=%v", api.userCalls, api.labelCalls, err)
		}
	})
}
