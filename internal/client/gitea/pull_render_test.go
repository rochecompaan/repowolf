package gitea

import (
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderPullListPresenceAndOrder(t *testing.T) {
	mergeable := false
	request := &repowolfv1.GiteaPullListRequest{State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Page: 1, Limit: 2, Fields: []repowolfv1.GiteaPullField{repowolfv1.GiteaPullField_GITEA_PULL_FIELD_INDEX, repowolfv1.GiteaPullField_GITEA_PULL_FIELD_MERGEABLE}}
	parsed := command{request: &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullList{PullList: request}}, format: outputJSON, pullFields: request.Fields}
	result := &repowolfv1.GiteaPullListResult{Pulls: []*repowolfv1.GiteaPullRecord{{Index: 2, State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Mergeable: &mergeable}, {Index: 1, State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED}}}
	got, err := renderPullList(parsed, result)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[{\"index\":2,\"mergeable\":false},{\"index\":1}]\n"; string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderPullViewTypedActors(t *testing.T) {
	now := timestamppb.New(time.Unix(1, 0).UTC())
	allow := false
	pull := &repowolfv1.GiteaPullRecord{Index: 1, State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, AuthorId: 2, Author: "alice", Url: "https://g/p/1", Title: "title", AllowMaintainerEdit: &allow, Base: "main", BaseCommit: "abc", Head: "alice:topic", Created: now, Updated: now, RequestedReviewers: []*repowolfv1.GiteaReviewActor{}, Reviews: []*repowolfv1.GiteaPullReviewRecord{}, Assignees: []string{}, Labels: []string{}}
	request := &repowolfv1.GiteaPullViewRequest{Index: 1}
	parsed := command{request: &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullView{PullView: request}}, format: outputJSON}
	got, err := renderPullView(parsed, &repowolfv1.GiteaPullViewResult{Pull: pull})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty output")
	}
}
