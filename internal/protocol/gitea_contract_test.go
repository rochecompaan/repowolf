package protocol_test

import (
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestGiteaIssueProtocolBranchesAndPresence(t *testing.T) {
	assertBranches(t, (&repowolfv1.GiteaRequest{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"issue_list": 11,
		"issue_view": 12,
	}, true)
	assertBranches(t, (&repowolfv1.GiteaResponse{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"issue_list": 11,
		"issue_view": 12,
	}, true)

	list := (&repowolfv1.GiteaIssueListRequest{}).ProtoReflect().Descriptor()
	for _, name := range []protoreflect.Name{"keyword", "author", "assignee", "mentions", "owner"} {
		field := list.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.StringKind || !field.HasPresence() {
			t.Errorf("%s must be an optional string", name)
		}
		assertOptionalEmptyPresence(t, field)
	}
	milestone := (&repowolfv1.GiteaIssueRecord{}).ProtoReflect().Descriptor().Fields().ByName("milestone")
	if milestone == nil || milestone.Kind() != protoreflect.StringKind || !milestone.HasPresence() {
		t.Error("milestone must be an optional string")
	}
}

func TestGiteaIssueEditProtocol(t *testing.T) {
	assertBranches(t, (&repowolfv1.GiteaRequest{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"issue_edit": 17}, true)
	assertBranches(t, (&repowolfv1.GiteaResponse{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"issue_edit": 17}, true)
	descriptor := (&repowolfv1.GiteaIssueEditRequest{}).ProtoReflect().Descriptor()
	for _, name := range []protoreflect.Name{"title", "description"} {
		field := descriptor.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.StringKind || !field.HasPresence() {
			t.Fatalf("%s must be an optional string", name)
		}
	}
	input := &repowolfv1.GiteaIssueEditRequest{Index: 7, AssigneeAction: &repowolfv1.GiteaIssueEditRequest_SetAssignees{SetAssignees: &repowolfv1.GiteaStringList{Values: []string{}}}}
	data, err := proto.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	output := new(repowolfv1.GiteaIssueEditRequest)
	if err := proto.Unmarshal(data, output); err != nil {
		t.Fatal(err)
	}
	if output.GetSetAssignees() == nil || output.AssigneeAction == nil {
		t.Fatal("empty set-assignees presence lost")
	}
}

func TestGiteaIssueProtocolEnums(t *testing.T) {
	assertEnum(t, repowolfv1.GiteaIssueState(0).Descriptor(), []string{
		"GITEA_ISSUE_STATE_UNSPECIFIED", "GITEA_ISSUE_STATE_OPEN", "GITEA_ISSUE_STATE_CLOSED", "GITEA_ISSUE_STATE_ALL",
	})
	assertEnum(t, repowolfv1.GiteaIssueKind(0).Descriptor(), []string{
		"GITEA_ISSUE_KIND_UNSPECIFIED", "GITEA_ISSUE_KIND_ISSUE",
	})
	assertEnum(t, repowolfv1.GiteaIssueField(0).Descriptor(), []string{
		"GITEA_ISSUE_FIELD_UNSPECIFIED", "GITEA_ISSUE_FIELD_INDEX", "GITEA_ISSUE_FIELD_STATE", "GITEA_ISSUE_FIELD_AUTHOR",
		"GITEA_ISSUE_FIELD_AUTHOR_ID", "GITEA_ISSUE_FIELD_URL", "GITEA_ISSUE_FIELD_TITLE", "GITEA_ISSUE_FIELD_BODY",
		"GITEA_ISSUE_FIELD_CREATED", "GITEA_ISSUE_FIELD_UPDATED", "GITEA_ISSUE_FIELD_DEADLINE", "GITEA_ISSUE_FIELD_ASSIGNEES",
		"GITEA_ISSUE_FIELD_MILESTONE", "GITEA_ISSUE_FIELD_LABELS", "GITEA_ISSUE_FIELD_COMMENTS", "GITEA_ISSUE_FIELD_REPO",
		"GITEA_ISSUE_FIELD_OWNER", "GITEA_ISSUE_FIELD_KIND",
	})
}

func TestGiteaIssueProtocolFieldShapesAndClosedSurface(t *testing.T) {
	list := (&repowolfv1.GiteaIssueListRequest{}).ProtoReflect().Descriptor()
	issue := (&repowolfv1.GiteaIssueRecord{}).ProtoReflect().Descriptor()
	comment := (&repowolfv1.GiteaCommentRecord{}).ProtoReflect().Descriptor()
	result := (&repowolfv1.GiteaIssueListResult{}).ProtoReflect().Descriptor()

	for _, field := range []protoreflect.FieldDescriptor{
		list.Fields().ByName("from"), list.Fields().ByName("until"),
		issue.Fields().ByName("created"), issue.Fields().ByName("updated"), issue.Fields().ByName("deadline"),
		comment.Fields().ByName("created"), comment.Fields().ByName("updated"),
	} {
		if field == nil || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "google.protobuf.Timestamp" {
			t.Errorf("field %v must be google.protobuf.Timestamp", field)
		}
	}
	for _, field := range []protoreflect.FieldDescriptor{
		list.Fields().ByName("fields"), result.Fields().ByName("issues"), issue.Fields().ByName("assignees"),
		issue.Fields().ByName("labels"), issue.Fields().ByName("comments"),
	} {
		if field == nil || field.Cardinality() != protoreflect.Repeated || field.IsMap() {
			t.Errorf("field %v must be an ordered repeated field", field)
		}
	}

	for _, message := range []protoreflect.MessageDescriptor{list, issue, comment, result, (&repowolfv1.GiteaIssueViewRequest{}).ProtoReflect().Descriptor(), (&repowolfv1.GiteaIssueViewResult{}).ProtoReflect().Descriptor()} {
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			name := strings.ToLower(string(field.Name()))
			if field.IsMap() || name == "raw_json" || name == "endpoint" || name == "token" || name == "query" || name == "arbitrary_query" {
				t.Errorf("closed issue protocol exposes forbidden field %s.%s", message.FullName(), field.Name())
			}
		}
	}
}

func TestGiteaPullProtocolBranchesEnumsAndFieldLayout(t *testing.T) {
	assertBranches(t, (&repowolfv1.GiteaRequest{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"pull_list": 18, "pull_view": 19}, true)
	assertBranches(t, (&repowolfv1.GiteaResponse{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{"pull_list": 18, "pull_view": 19}, true)
	assertEnum(t, repowolfv1.GiteaPullState(0).Descriptor(), []string{
		"GITEA_PULL_STATE_UNSPECIFIED", "GITEA_PULL_STATE_OPEN", "GITEA_PULL_STATE_CLOSED", "GITEA_PULL_STATE_ALL",
	})
	assertEnum(t, repowolfv1.GiteaPullField(0).Descriptor(), []string{
		"GITEA_PULL_FIELD_UNSPECIFIED", "GITEA_PULL_FIELD_INDEX", "GITEA_PULL_FIELD_STATE", "GITEA_PULL_FIELD_AUTHOR",
		"GITEA_PULL_FIELD_AUTHOR_ID", "GITEA_PULL_FIELD_URL", "GITEA_PULL_FIELD_TITLE", "GITEA_PULL_FIELD_BODY",
		"GITEA_PULL_FIELD_MERGEABLE", "GITEA_PULL_FIELD_BASE", "GITEA_PULL_FIELD_BASE_COMMIT", "GITEA_PULL_FIELD_HEAD",
		"GITEA_PULL_FIELD_CREATED", "GITEA_PULL_FIELD_UPDATED", "GITEA_PULL_FIELD_DEADLINE", "GITEA_PULL_FIELD_ASSIGNEES",
		"GITEA_PULL_FIELD_MILESTONE", "GITEA_PULL_FIELD_LABELS", "GITEA_PULL_FIELD_COMMENTS",
	})
	assertEnum(t, repowolfv1.GiteaPullReviewState(0).Descriptor(), []string{
		"GITEA_PULL_REVIEW_STATE_UNSPECIFIED", "GITEA_PULL_REVIEW_STATE_APPROVED", "GITEA_PULL_REVIEW_STATE_PENDING",
		"GITEA_PULL_REVIEW_STATE_COMMENT", "GITEA_PULL_REVIEW_STATE_REQUEST_CHANGES", "GITEA_PULL_REVIEW_STATE_REQUEST_REVIEW",
	})
	assertFieldLayout(t, (&repowolfv1.GiteaPullReviewRecord{}).ProtoReflect().Descriptor(), []string{
		"id", "actor", "state", "body", "commit_id", "stale", "official", "dismissed", "code_comment_count", "submitted", "url",
	})
	assertFieldLayout(t, (&repowolfv1.GiteaPullRecord{}).ProtoReflect().Descriptor(), []string{
		"index", "state", "author_id", "author", "url", "title", "body", "draft", "mergeable", "allow_maintainer_edit",
		"base", "base_commit", "head", "created", "updated", "deadline", "assignees", "milestone", "labels", "comment_count",
		"comments", "requested_reviewers", "reviews",
	})
}

func TestGiteaPullProtocolPresenceOneofRepeatedAndClosedSurface(t *testing.T) {
	pull := (&repowolfv1.GiteaPullRecord{}).ProtoReflect().Descriptor()
	review := (&repowolfv1.GiteaPullReviewRecord{}).ProtoReflect().Descriptor()
	actor := (&repowolfv1.GiteaReviewActor{}).ProtoReflect().Descriptor()
	actorOneof := actor.Oneofs().ByName("actor")
	if actorOneof == nil {
		t.Fatal("review actor oneof is missing")
	}
	userField, teamField := actorOneof.Fields().ByName("user"), actorOneof.Fields().ByName("team")
	if actorOneof.Fields().Len() != 2 || userField == nil || userField.Number() != 1 || userField.Kind() != protoreflect.MessageKind || userField.Message().FullName() != "repowolf.v1.GiteaReviewUser" || teamField == nil || teamField.Number() != 2 || teamField.Kind() != protoreflect.MessageKind || teamField.Message().FullName() != "repowolf.v1.GiteaReviewTeam" {
		t.Fatalf("invalid review actor oneof: %v", actorOneof)
	}
	for _, name := range []protoreflect.Name{"mergeable", "allow_maintainer_edit"} {
		field := pull.Fields().ByName(name)
		if field == nil || field.Kind() != protoreflect.BoolKind || !field.HasPresence() {
			t.Errorf("%s must be an optional bool", name)
		}
	}
	milestone := pull.Fields().ByName("milestone")
	if milestone == nil || milestone.Kind() != protoreflect.StringKind || !milestone.HasPresence() {
		t.Error("pull milestone must be an optional string")
	}
	for _, field := range []protoreflect.FieldDescriptor{
		pull.Fields().ByName("created"), pull.Fields().ByName("updated"), pull.Fields().ByName("deadline"), review.Fields().ByName("submitted"),
	} {
		if field == nil || field.Kind() != protoreflect.MessageKind || field.Message().FullName() != "google.protobuf.Timestamp" {
			t.Errorf("field %v must be google.protobuf.Timestamp", field)
		}
	}
	for _, field := range []protoreflect.FieldDescriptor{
		(&repowolfv1.GiteaPullListRequest{}).ProtoReflect().Descriptor().Fields().ByName("fields"),
		(&repowolfv1.GiteaPullListResult{}).ProtoReflect().Descriptor().Fields().ByName("pulls"),
		pull.Fields().ByName("assignees"), pull.Fields().ByName("labels"), pull.Fields().ByName("comments"),
		pull.Fields().ByName("requested_reviewers"), pull.Fields().ByName("reviews"),
	} {
		if field == nil || field.Cardinality() != protoreflect.Repeated || field.IsMap() {
			t.Errorf("field %v must be an ordered repeated field", field)
		}
	}
	messages := []protoreflect.MessageDescriptor{
		(&repowolfv1.GiteaPullListRequest{}).ProtoReflect().Descriptor(), (&repowolfv1.GiteaPullViewRequest{}).ProtoReflect().Descriptor(),
		(&repowolfv1.GiteaReviewUser{}).ProtoReflect().Descriptor(), (&repowolfv1.GiteaReviewTeam{}).ProtoReflect().Descriptor(), actor,
		review, pull, (&repowolfv1.GiteaPullListResult{}).ProtoReflect().Descriptor(), (&repowolfv1.GiteaPullViewResult{}).ProtoReflect().Descriptor(),
	}
	for _, message := range messages {
		for i := 0; i < message.Fields().Len(); i++ {
			field := message.Fields().Get(i)
			name := strings.ToLower(string(field.Name()))
			if field.IsMap() || strings.Contains(name, "raw") || strings.Contains(name, "json") || strings.Contains(name, "endpoint") || strings.Contains(name, "token") || strings.Contains(name, "query") || strings.Contains(name, "provider") {
				t.Errorf("closed pull protocol exposes forbidden field %s.%s", message.FullName(), field.Name())
			}
		}
	}
}

func TestGiteaPullProtocolMarshalPreservesOptionalBooleansAndActorExclusivity(t *testing.T) {
	for _, test := range []struct {
		name  string
		value *bool
	}{
		{name: "unset"},
		{name: "false", value: proto.Bool(false)},
		{name: "true", value: proto.Bool(true)},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := &repowolfv1.GiteaPullRecord{Mergeable: test.value, AllowMaintainerEdit: test.value}
			data, err := proto.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			output := new(repowolfv1.GiteaPullRecord)
			if err := proto.Unmarshal(data, output); err != nil {
				t.Fatal(err)
			}
			if test.value == nil {
				if output.Mergeable != nil || output.AllowMaintainerEdit != nil {
					t.Fatalf("unset optional booleans became present: %#v", output)
				}
			} else if output.Mergeable == nil || output.AllowMaintainerEdit == nil || *output.Mergeable != *test.value || *output.AllowMaintainerEdit != *test.value {
				t.Fatalf("optional boolean presence/value lost: %#v", output)
			}
		})
	}
	milestoneInput := &repowolfv1.GiteaPullRecord{Milestone: proto.String("")}
	milestoneData, err := proto.Marshal(milestoneInput)
	if err != nil {
		t.Fatal(err)
	}
	milestoneOutput := new(repowolfv1.GiteaPullRecord)
	if err := proto.Unmarshal(milestoneData, milestoneOutput); err != nil {
		t.Fatal(err)
	}
	if milestoneOutput.Milestone == nil || *milestoneOutput.Milestone != "" {
		t.Fatalf("optional empty milestone presence lost: %#v", milestoneOutput)
	}
	for _, input := range []*repowolfv1.GiteaReviewActor{
		{Actor: &repowolfv1.GiteaReviewActor_User{User: &repowolfv1.GiteaReviewUser{Id: 7, Login: "alice"}}},
		{Actor: &repowolfv1.GiteaReviewActor_Team{Team: &repowolfv1.GiteaReviewTeam{Id: 9, Name: "reviewers"}}},
	} {
		data, err := proto.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		output := new(repowolfv1.GiteaReviewActor)
		if err := proto.Unmarshal(data, output); err != nil {
			t.Fatal(err)
		}
		if (input.GetUser() != nil && (output.GetUser() == nil || output.GetTeam() != nil)) || (input.GetTeam() != nil && (output.GetTeam() == nil || output.GetUser() != nil)) {
			t.Fatalf("actor oneof exclusivity lost: input=%#v output=%#v", input, output)
		}
	}
}

func assertFieldLayout(t *testing.T, message protoreflect.MessageDescriptor, names []string) {
	t.Helper()
	if message.Fields().Len() != len(names) {
		t.Fatalf("%s has %d fields, want %d", message.FullName(), message.Fields().Len(), len(names))
	}
	for index, name := range names {
		field := message.Fields().Get(index)
		if string(field.Name()) != name || field.Number() != protoreflect.FieldNumber(index+1) {
			t.Errorf("%s field %d = %s/%d, want %s/%d", message.FullName(), index, field.Name(), field.Number(), name, index+1)
		}
	}
}

func assertBranches(t *testing.T, message protoreflect.MessageDescriptor, branches map[protoreflect.Name]protoreflect.FieldNumber, requireOneof bool) {
	t.Helper()
	for name, number := range branches {
		field := message.Fields().ByName(name)
		if field == nil || field.Number() != number || requireOneof && field.ContainingOneof() == nil {
			t.Errorf("%s branch %s must use tag %d", message.FullName(), name, number)
		}
	}
}

func assertEnum(t *testing.T, enum protoreflect.EnumDescriptor, names []string) {
	t.Helper()
	if enum.Values().Len() != len(names) {
		t.Fatalf("%s has %d values, want %d", enum.FullName(), enum.Values().Len(), len(names))
	}
	for number, name := range names {
		value := enum.Values().ByNumber(protoreflect.EnumNumber(number))
		if value == nil || string(value.Name()) != name {
			t.Errorf("%s value %d = %v, want %s", enum.FullName(), number, value, name)
		}
	}
	if !strings.HasSuffix(names[0], "_UNSPECIFIED") {
		t.Errorf("%s zero value must be UNSPECIFIED", enum.FullName())
	}
}

func assertOptionalEmptyPresence(t *testing.T, field protoreflect.FieldDescriptor) {
	t.Helper()
	unset := (&repowolfv1.GiteaIssueListRequest{}).ProtoReflect()
	if unset.Has(field) {
		t.Fatalf("%s unexpectedly present when unset", field.Name())
	}
	set := (&repowolfv1.GiteaIssueListRequest{}).ProtoReflect()
	set.Set(field, protoreflect.ValueOfString(""))
	data, err := proto.Marshal(set.Interface())
	if err != nil {
		t.Fatal(err)
	}
	out := (&repowolfv1.GiteaIssueListRequest{}).ProtoReflect()
	if err := proto.Unmarshal(data, out.Interface()); err != nil {
		t.Fatal(err)
	}
	if !out.Has(field) || out.Get(field).String() != "" {
		t.Fatalf("optional empty presence lost for %s", field.Name())
	}
}
