package gitea

import (
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGiteaMutationProtocolDescriptor(t *testing.T) {
	request := (&repowolfv1.GiteaRequest{}).ProtoReflect().Descriptor().Oneofs().ByName("operation")
	response := (&repowolfv1.GiteaResponse{}).ProtoReflect().Descriptor().Oneofs().ByName("result")
	for i, name := range []string{"issue_create", "issue_comment", "issue_close", "issue_reopen"} {
		if field := request.Fields().ByName(protowireName(name)); field == nil || field.Number() != protoreflect.FieldNumber(13+i) {
			t.Fatalf("request %s descriptor = %v", name, field)
		}
		if field := response.Fields().ByName(protowireName(name)); field == nil || field.Number() != protoreflect.FieldNumber(13+i) {
			t.Fatalf("response %s descriptor = %v", name, field)
		}
	}
	field := (&repowolfv1.GiteaIssueCreateRequest{}).ProtoReflect().Descriptor().Fields().ByName("description")
	if field == nil || !field.HasPresence() {
		t.Fatal("description lacks presence")
	}
}

func protowireName(value string) protoreflect.Name { return protoreflect.Name(value) }

func TestValidateMutationAndOperation(t *testing.T) {
	requests := []struct {
		name      string
		operation any
		want      string
	}{
		{"create", &repowolfv1.GiteaRequest_IssueCreate{IssueCreate: &repowolfv1.GiteaIssueCreateRequest{Title: "title"}}, "gitea.issue_create"},
		{"comment", &repowolfv1.GiteaRequest_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentRequest{Index: 7, Body: "body"}}, "gitea.issue_comment"},
		{"close", &repowolfv1.GiteaRequest_IssueClose{IssueClose: &repowolfv1.GiteaIssueCloseRequest{Index: 7}}, "gitea.issue_close"},
		{"reopen", &repowolfv1.GiteaRequest_IssueReopen{IssueReopen: &repowolfv1.GiteaIssueReopenRequest{Index: 7}}, "gitea.issue_reopen"},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			r := &repowolfv1.GiteaRequest{}
			switch v := test.operation.(type) {
			case *repowolfv1.GiteaRequest_IssueCreate:
				r.Operation = v
			case *repowolfv1.GiteaRequest_IssueComment:
				r.Operation = v
			case *repowolfv1.GiteaRequest_IssueClose:
				r.Operation = v
			case *repowolfv1.GiteaRequest_IssueReopen:
				r.Operation = v
			}
			if err := ValidateRequest(r); err != nil {
				t.Fatal(err)
			}
			if c, _ := Capability(r); c != config.IssuesWrite {
				t.Fatalf("capability=%q", c)
			}
			if n, _ := OperationName(r); n != test.want {
				t.Fatalf("name=%q", n)
			}
		})
	}
	tooLong := strings.Repeat("x", 256)
	for _, r := range []*repowolfv1.GiteaRequest{
		{Operation: &repowolfv1.GiteaRequest_IssueCreate{IssueCreate: &repowolfv1.GiteaIssueCreateRequest{Title: ""}}},
		{Operation: &repowolfv1.GiteaRequest_IssueCreate{IssueCreate: &repowolfv1.GiteaIssueCreateRequest{Title: tooLong}}},
		{Operation: &repowolfv1.GiteaRequest_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentRequest{Index: 0, Body: "body"}}},
		{Operation: &repowolfv1.GiteaRequest_IssueComment{IssueComment: &repowolfv1.GiteaIssueCommentRequest{Index: 1, Body: ""}}},
	} {
		if ValidateRequest(r) == nil {
			t.Fatalf("accepted invalid request %#v", r)
		}
	}
}
