package gitea

import (
	"context"
	"errors"
	"testing"
	"time"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

type mutationWriteAPI struct {
	fakeIssueAPI
	comment                 *sdk.Comment
	commentCalls, editCalls int
	editResult              *sdk.Issue
	editOption              sdk.EditIssueOption
}

func (f *mutationWriteAPI) ListRepoLabels(context.Context, string, string, sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	return nil, nil
}
func (f *mutationWriteAPI) CreateIssue(context.Context, string, string, sdk.CreateIssueOption) (*sdk.Issue, error) {
	return nil, errors.New("unexpected")
}
func (f *mutationWriteAPI) CreateIssueComment(_ context.Context, _, _ string, _ int64, _ sdk.CreateIssueCommentOption) (*sdk.Comment, error) {
	f.commentCalls++
	return f.comment, nil
}
func (f *mutationWriteAPI) EditIssue(_ context.Context, _, _ string, _ int64, o sdk.EditIssueOption) (*sdk.Issue, error) {
	f.editCalls++
	f.editOption = o
	return f.editResult, nil
}
func mutationComment() *sdk.Comment {
	now := time.Unix(1, 0)
	return &sdk.Comment{ID: 9, Poster: &sdk.User{ID: 2, UserName: "alice"}, HTMLURL: "https://g/o/r/issues/7#issuecomment-9", Body: "body", Created: now, Updated: now.Add(time.Second)}
}

func TestIssueCommentUsesKindPreflightAndOneWrite(t *testing.T) {
	api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: sdkIssue()}, comment: mutationComment()}
	adapter, _ := newRepositoryAdapter(api)
	response, err := adapter.issueComment(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCommentRequest{Index: 7, Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if api.getCalls != 1 || api.commentCalls != 1 || response.GetIssueComment().GetComment().Id != 9 {
		t.Fatalf("calls=%d/%d response=%#v", api.getCalls, api.commentCalls, response)
	}
}
func TestIssueKindPreflightPreventsWrite(t *testing.T) {
	issue := sdkIssue()
	issue.PullRequest = &sdk.PullRequestMeta{}
	api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: issue}, comment: mutationComment()}
	adapter, _ := newRepositoryAdapter(api)
	_, err := adapter.issueComment(context.Background(), issueResolved(), &repowolfv1.GiteaIssueCommentRequest{Index: 7, Body: "body"})
	if !errors.Is(err, rpcstatus.ErrIssueKind) || api.commentCalls != 0 {
		t.Fatalf("err=%v writes=%d", err, api.commentCalls)
	}
}
func TestIssueStateEnsureTransition(t *testing.T) {
	for _, test := range []struct {
		name            string
		initial, target sdk.StateType
		writes          int
		transitioned    bool
	}{{"close open", sdk.StateOpen, sdk.StateClosed, 1, true}, {"close closed", sdk.StateClosed, sdk.StateClosed, 0, false}, {"reopen closed", sdk.StateClosed, sdk.StateOpen, 1, true}, {"reopen open", sdk.StateOpen, sdk.StateOpen, 0, false}} {
		t.Run(test.name, func(t *testing.T) {
			current := sdkIssue()
			current.State = test.initial
			result := sdkIssue()
			result.State = test.target
			api := &mutationWriteAPI{fakeIssueAPI: fakeIssueAPI{issue: current}, editResult: result}
			adapter, _ := newRepositoryAdapter(api)
			ctx, metadata := WithMutationMetadata(context.Background())
			_, err := adapter.issueState(ctx, issueResolved(), 7, test.target, test.target == sdk.StateClosed)
			if err != nil {
				t.Fatal(err)
			}
			if api.editCalls != test.writes || metadata.Transitioned == nil || *metadata.Transitioned != test.transitioned {
				t.Fatalf("writes=%d metadata=%#v", api.editCalls, metadata)
			}
			if test.writes == 1 && (api.editOption.State == nil || *api.editOption.State != test.target || api.editOption.Body != nil || api.editOption.Title != "") {
				t.Fatalf("option=%#v", api.editOption)
			}
		})
	}
}
