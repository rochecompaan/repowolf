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
