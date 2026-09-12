package gitea

import (
	"context"
	"errors"
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
	createOption           sdk.CreateIssueOption
}

func (f *fakeWriteAPI) ListRepoLabels(_ context.Context, _, _ string, o sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	f.listCalls++
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
