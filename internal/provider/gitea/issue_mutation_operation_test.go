package gitea

import (
	"fmt"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"google.golang.org/protobuf/proto"
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

func TestValidateIssueEditAndOperation(t *testing.T) {
	request := func(edit *repowolfv1.GiteaIssueEditRequest) *repowolfv1.GiteaRequest {
		return &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueEdit{IssueEdit: edit}}
	}

	valid := []struct {
		name string
		edit *repowolfv1.GiteaIssueEditRequest
	}{
		{
			name: "empty set remains an explicit mutation",
			edit: &repowolfv1.GiteaIssueEditRequest{
				Index: 7,
				AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{
					SetAssignees: &repowolfv1.GiteaStringList{},
				},
			},
		},
		{name: "empty description", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Description: proto.String("")}},
		{
			name: "exact text bounds",
			edit: &repowolfv1.GiteaIssueEditRequest{
				Index:       7,
				Title:       proto.String(strings.Repeat("界", 255)),
				Description: proto.String(strings.Repeat("x", maximumMutationBodyBytes)),
			},
		},
	}
	for _, test := range valid {
		t.Run("valid "+test.name, func(t *testing.T) {
			r := request(test.edit)
			if err := ValidateRequest(r); err != nil {
				t.Fatalf("ValidateRequest() error = %v", err)
			}
			if capability, err := Capability(r); err != nil || capability != config.IssuesWrite {
				t.Fatalf("Capability() = %q, %v", capability, err)
			}
			if operation, err := OperationName(r); err != nil || operation != "gitea.issue_edit" {
				t.Fatalf("OperationName() = %q, %v", operation, err)
			}
		})
	}

	tooMany := make([]string, 26)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("name-%02d", i)
	}
	var nilSetAction *repowolfv1.GiteaIssueEditRequest_SetAssignees
	var nilLabelAction *repowolfv1.GiteaIssueEditRequest_AddLabels
	invalidUTF8 := string([]byte{0xff})
	invalid := []struct {
		name string
		edit *repowolfv1.GiteaIssueEditRequest
	}{
		{name: "nil edit", edit: nil},
		{name: "zero index", edit: &repowolfv1.GiteaIssueEditRequest{Index: 0, Title: proto.String("title")}},
		{name: "negative index", edit: &repowolfv1.GiteaIssueEditRequest{Index: -1, Title: proto.String("title")}},
		{name: "no mutation", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7}},
		{name: "empty title", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: proto.String("")}},
		{name: "title over rune limit", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: proto.String(strings.Repeat("界", 256))}},
		{name: "description over byte limit", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Description: proto.String(strings.Repeat("x", maximumMutationBodyBytes+1))}},
		{name: "invalid UTF-8 title", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: proto.String(invalidUTF8)}},
		{name: "invalid UTF-8 description", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Description: proto.String(invalidUTF8)}},
		{name: "NUL title", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Title: proto.String("bad\x00title")}},
		{name: "NUL description", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, Description: proto.String("bad\x00body")}},
		{name: "nil set wrapper", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{}}},
		{name: "nil add assignee wrapper", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_AddAssignees{}}},
		{name: "nil remove assignee wrapper", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_RemoveAssignees{}}},
		{name: "nil add label wrapper", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{}}},
		{name: "nil remove label wrapper", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{}}},
		{name: "typed nil assignee action", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: nilSetAction}},
		{name: "typed nil label action", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: nilLabelAction}},
		{name: "empty add assignees", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_AddAssignees{AddAssignees: &repowolfv1.GiteaStringList{}}}},
		{name: "empty remove assignees", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_RemoveAssignees{RemoveAssignees: &repowolfv1.GiteaStringList{}}}},
		{name: "empty add labels", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{}}}},
		{name: "empty remove labels", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{}}}},
		{name: "empty list member", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{SetAssignees: &repowolfv1.GiteaStringList{Values: []string{""}}}}},
		{name: "duplicate list member", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: []string{"bug", "bug"}}}}},
		{name: "too many list members", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_AddAssignees{AddAssignees: &repowolfv1.GiteaStringList{Values: tooMany}}}},
		{name: "list member over rune limit", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_RemoveLabels{RemoveLabels: &repowolfv1.GiteaStringList{Values: []string{strings.Repeat("界", 256)}}}}},
		{name: "invalid UTF-8 list member", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_RemoveAssignees{RemoveAssignees: &repowolfv1.GiteaStringList{Values: []string{invalidUTF8}}}}},
		{name: "NUL list member", edit: &repowolfv1.GiteaIssueEditRequest{Index: 7, LabelAction: &repowolfv1.GiteaIssueEditRequest_AddLabels{AddLabels: &repowolfv1.GiteaStringList{Values: []string{"bad\x00label"}}}}},
	}
	for _, test := range invalid {
		t.Run("invalid "+test.name, func(t *testing.T) {
			r := request(test.edit)
			if err := ValidateRequest(r); err == nil {
				t.Fatal("ValidateRequest() accepted invalid issue edit")
			}
			if capability, err := Capability(r); err == nil || capability != "" {
				t.Fatalf("Capability() = %q, %v", capability, err)
			}
			if operation, err := OperationName(r); err == nil || operation != "" {
				t.Fatalf("OperationName() = %q, %v", operation, err)
			}
		})
	}
}
