package gitea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type fakeWriteAPI struct {
	fakeIssueAPI
	pages                  map[int][]*sdk.Label
	listErr                error
	createResult           *sdk.Issue
	createErr              error
	listCalls, createCalls int
	listOptions            []sdk.ListLabelsOptions
	listOwner, listRepo    string
	createOption           sdk.CreateIssueOption
}

func (f *fakeWriteAPI) ListRepoLabels(_ context.Context, owner, repo string, o sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.listCalls++
	f.listOwner, f.listRepo = owner, repo
	f.listOptions = append(f.listOptions, o)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.pages[o.Page], nil
}
func (f *fakeWriteAPI) CreateIssue(_ context.Context, _, _ string, o sdk.CreateIssueOption) (*sdk.Issue, error) {
	f.createCalls++
	f.createOption = o
	return f.createResult, f.createErr
}
func (f *fakeWriteAPI) CreateIssueComment(context.Context, string, string, int64, sdk.CreateIssueCommentOption) (*sdk.Comment, error) {
	panic("unexpected")
}
func (f *fakeWriteAPI) EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, error) {
	panic("unexpected")
}

func labelPage(start, count int) []*sdk.Label {
	labels := make([]*sdk.Label, count)
	for i := range labels {
		id := int64(start + i)
		labels[i] = &sdk.Label{ID: id, Name: fmt.Sprintf("label-%04d", id)}
	}
	return labels
}

func TestResolveLabelIDsBoundedPaging(t *testing.T) {
	t.Run("no requested labels skips lookup", func(t *testing.T) {
		api := &fakeWriteAPI{}
		adapter, _ := newRepositoryAdapter(api)
		ids, err := adapter.resolveLabelIDs(context.Background(), "Owner", "Repo", nil)
		if err != nil || len(ids) != 0 || api.listCalls != 0 {
			t.Fatalf("ids=%v err=%v calls=%d", ids, err, api.listCalls)
		}
	})

	t.Run("two pages preserve requested order and canonical repository", func(t *testing.T) {
		api := &fakeWriteAPI{pages: map[int][]*sdk.Label{1: labelPage(1, 50), 2: labelPage(51, 2)}}
		adapter, _ := newRepositoryAdapter(api)
		ids, err := adapter.resolveLabelIDs(context.Background(), "Owner", "Repo", []string{"label-0052", "label-0001"})
		if err != nil || len(ids) != 2 || ids[0] != 52 || ids[1] != 1 || api.listCalls != 2 || api.listOwner != "Owner" || api.listRepo != "Repo" {
			t.Fatalf("ids=%v err=%v calls=%d repo=%s/%s", ids, err, api.listCalls, api.listOwner, api.listRepo)
		}
		for page, option := range api.listOptions {
			if option.Page != page+1 || option.PageSize != 50 {
				t.Fatalf("option[%d]=%#v", page, option)
			}
		}
	})

	t.Run("one thousand labels require empty overflow probe", func(t *testing.T) {
		pages := make(map[int][]*sdk.Label)
		for page := 1; page <= 20; page++ {
			pages[page] = labelPage((page-1)*50+1, 50)
		}
		pages[21] = []*sdk.Label{}
		api := &fakeWriteAPI{pages: pages}
		adapter, _ := newRepositoryAdapter(api)
		ids, err := adapter.resolveLabelIDs(context.Background(), "Owner", "Repo", []string{"label-1000"})
		if err != nil || len(ids) != 1 || ids[0] != 1000 || api.listCalls != 21 {
			t.Fatalf("ids=%v err=%v calls=%d", ids, err, api.listCalls)
		}
		pages[21] = []*sdk.Label{{ID: 1001, Name: "overflow"}}
		api = &fakeWriteAPI{pages: pages}
		adapter, _ = newRepositoryAdapter(api)
		if _, err := adapter.resolveLabelIDs(context.Background(), "Owner", "Repo", []string{"label-1000"}); !errors.Is(err, rpcstatus.ErrFailedPrecondition) || api.listCalls != 21 {
			t.Fatalf("overflow err=%v calls=%d", err, api.listCalls)
		}
	})
}

func TestResolveLabelIDsRejectsMalformedProviderData(t *testing.T) {
	for _, test := range []struct {
		name   string
		labels []*sdk.Label
	}{
		{"nil label", []*sdk.Label{nil}},
		{"zero id", []*sdk.Label{{ID: 0, Name: "bug"}}},
		{"duplicate id", []*sdk.Label{{ID: 1, Name: "bug"}, {ID: 1, Name: "urgent"}}},
		{"empty name", []*sdk.Label{{ID: 1, Name: ""}}},
		{"duplicate name", []*sdk.Label{{ID: 1, Name: "bug"}, {ID: 2, Name: "bug"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &fakeWriteAPI{pages: map[int][]*sdk.Label{1: test.labels}}
			adapter, _ := newRepositoryAdapter(api)
			if _, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title", Labels: []string{"bug"}}); !errors.Is(err, rpcstatus.ErrProviderFailure) || api.createCalls != 0 {
				t.Fatalf("err=%v createCalls=%d", err, api.createCalls)
			}
		})
	}
}

func TestResolveLabelIDsRejectsOversizedPageAndProviderFailure(t *testing.T) {
	for _, api := range []*fakeWriteAPI{
		{pages: map[int][]*sdk.Label{1: labelPage(1, 51)}},
		{listErr: errors.New("provider secret")},
	} {
		adapter, _ := newRepositoryAdapter(api)
		_, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title", Labels: []string{"bug"}})
		if !errors.Is(err, rpcstatus.ErrProviderFailure) || api.listCalls != 1 || api.createCalls != 0 || strings.Contains(err.Error(), "provider secret") {
			t.Fatalf("err=%v listCalls=%d createCalls=%d", err, api.listCalls, api.createCalls)
		}
	}
}

func TestResolveLabelIDsIsCaseSensitiveAndHonorsCancellation(t *testing.T) {
	api := &fakeWriteAPI{pages: map[int][]*sdk.Label{1: {{ID: 1, Name: "Bug"}}}}
	adapter, _ := newRepositoryAdapter(api)
	if _, err := adapter.resolveLabelIDs(context.Background(), "Owner", "Repo", []string{"bug"}); !errors.Is(err, rpcstatus.ErrFailedPrecondition) {
		t.Fatalf("case mismatch err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api = &fakeWriteAPI{pages: map[int][]*sdk.Label{}}
	adapter, _ = newRepositoryAdapter(api)
	if _, err := adapter.resolveLabelIDs(ctx, "Owner", "Repo", []string{"bug"}); !errors.Is(err, context.Canceled) || api.listCalls != 0 {
		t.Fatalf("cancel err=%v calls=%d", err, api.listCalls)
	}
}

func TestResolveLabelIDsAndIssueCreate(t *testing.T) {
	api := &fakeWriteAPI{pages: map[int][]*sdk.Label{1: {&sdk.Label{ID: 2, Name: "bug"}, &sdk.Label{ID: 3, Name: "urgent"}}}, createResult: sdkIssue()}
	adapter, _ := newRepositoryAdapter(api)
	description := "body"
	request := &repowolfv1.GiteaIssueCreateRequest{Title: "title", Description: &description, Assignees: []string{"alice"}, Labels: []string{"urgent", "bug"}}
	response, err := adapter.issueCreate(context.Background(), issueResolved(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.GetIssueCreate().GetIssue().Index != 7 || api.listCalls != 1 || api.createCalls != 1 {
		t.Fatalf("response=%#v calls=%d/%d", response, api.listCalls, api.createCalls)
	}
	if got := api.createOption.Labels; len(got) != 2 || got[0] != 3 || got[1] != 2 || api.createOption.Body != "body" || api.createOption.Assignees[0] != "alice" {
		t.Fatalf("option=%#v", api.createOption)
	}
}
func TestIssueCreatePreconditionDoesNotWrite(t *testing.T) {
	api := &fakeWriteAPI{pages: map[int][]*sdk.Label{1: {&sdk.Label{ID: 2, Name: "bug"}}}, createResult: sdkIssue()}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.issueCreate(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCreateRequest{Title: "title", Labels: []string{"missing"}})
	if !errors.Is(err, rpcstatus.ErrFailedPrecondition) || api.createCalls != 0 {
		t.Fatalf("err=%v calls=%d", err, api.createCalls)
	}
}
