package gitea

import (
	"strings"
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

func validProjectedPullRecord() *repowolfv1.GiteaPullRecord {
	now := timestamppb.New(time.Unix(1, 0).UTC())
	return &repowolfv1.GiteaPullRecord{Index: 1, State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, AuthorId: 2, Author: "alice", Url: "https://g/p/1", Title: "title", Base: "main", BaseCommit: "abc", Head: "alice:topic", Created: now, Updated: now, Assignees: []string{}, Labels: []string{}, Comments: []*repowolfv1.GiteaCommentRecord{}, RequestedReviewers: []*repowolfv1.GiteaReviewActor{}, Reviews: []*repowolfv1.GiteaPullReviewRecord{}}
}

func TestRenderPullViewProducesExactTypedNestedJSON(t *testing.T) {
	pull := validProjectedPullRecord()
	pull.RequestedReviewers = []*repowolfv1.GiteaReviewActor{
		{Actor: &repowolfv1.GiteaReviewActor_User{User: &repowolfv1.GiteaReviewUser{Id: 7, Login: "alice"}}},
		{Actor: &repowolfv1.GiteaReviewActor_Team{Team: &repowolfv1.GiteaReviewTeam{Id: 9, Name: "reviewers"}}},
	}
	pull.Reviews = []*repowolfv1.GiteaPullReviewRecord{{Id: 11, Actor: pull.RequestedReviewers[0], State: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_APPROVED, Submitted: pull.Created, Url: "https://g/review/11"}}
	request := &repowolfv1.GiteaPullViewRequest{Index: 1}
	got, err := renderPullView(command{request: &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullView{PullView: request}}, format: outputJSON}, &repowolfv1.GiteaPullViewResult{Pull: pull})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"requested-reviewers":[{"user":{"id":7,"login":"alice"}},{"team":{"id":9,"name":"reviewers"}}]`, `"reviews":[{"id":11,"actor":{"user":{"id":7,"login":"alice"}},"state":"approved"`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("output missing %q: %s", want, got)
		}
	}
}

func TestRenderPullRejectsMalformedHydrationAtomically(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*repowolfv1.GiteaPullRecord)
	}{
		{name: "negative comment count", mutate: func(p *repowolfv1.GiteaPullRecord) { p.CommentCount = -1 }},
		{name: "out of order reviews", mutate: func(p *repowolfv1.GiteaPullRecord) {
			actor := &repowolfv1.GiteaReviewActor{Actor: &repowolfv1.GiteaReviewActor_User{User: &repowolfv1.GiteaReviewUser{Id: 2, Login: "alice"}}}
			p.Reviews = []*repowolfv1.GiteaPullReviewRecord{{Id: 2, Actor: actor, State: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_COMMENT, Submitted: p.Created}, {Id: 1, Actor: actor, State: repowolfv1.GiteaPullReviewState_GITEA_PULL_REVIEW_STATE_COMMENT, Submitted: p.Created}}
		}},
		{name: "invalid actor", mutate: func(p *repowolfv1.GiteaPullRecord) { p.RequestedReviewers = []*repowolfv1.GiteaReviewActor{{}} }},
		{name: "invalid timestamp order", mutate: func(p *repowolfv1.GiteaPullRecord) { p.Updated = timestamppb.New(time.Unix(0, 0)) }},
		{name: "unrequested comments", mutate: func(p *repowolfv1.GiteaPullRecord) { p.Comments = []*repowolfv1.GiteaCommentRecord{{Id: 1}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			pull := validProjectedPullRecord()
			test.mutate(pull)
			request := &repowolfv1.GiteaPullViewRequest{Index: 1}
			output, err := renderPullView(command{request: &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullView{PullView: request}}, format: outputJSON}, &repowolfv1.GiteaPullViewResult{Pull: pull})
			if err == nil || output != nil {
				t.Fatalf("output=%q err=%v", output, err)
			}
		})
	}
}

func TestRenderPullEnforcesAggregateOutputLimitWithoutPartialBytes(t *testing.T) {
	pull := validProjectedPullRecord()
	request := &repowolfv1.GiteaPullViewRequest{Index: 1}
	parsed := command{request: &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_PullView{PullView: request}}, format: outputJSON}
	response := &repowolfv1.GiteaResponse{Meta: &repowolfv1.ResponseMeta{RequestId: "request"}, Result: &repowolfv1.GiteaResponse_PullView{PullView: &repowolfv1.GiteaPullViewResult{Pull: pull}}}
	pull.Body = strings.Repeat("x", maxRenderedBytes-1024)
	for range 8 {
		output, err := render(parsed, response)
		if err != nil {
			t.Fatal(err)
		}
		delta := maxRenderedBytes - len(output)
		if delta == 0 {
			break
		}
		pull.Body += strings.Repeat("x", delta)
	}
	output, err := render(parsed, response)
	if err != nil || len(output) != maxRenderedBytes {
		t.Fatalf("exact limit=%d err=%v", len(output), err)
	}
	pull.Body += "x"
	output, err = render(parsed, response)
	if err == nil || output != nil {
		t.Fatalf("over limit output=%d err=%v", len(output), err)
	}
}
