package github

import (
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestPatchmillProtocolSurface(t *testing.T) {
	file := repowolfv1.File_repowolf_v1_github_proto
	want := map[protoreflect.Name]map[protoreflect.Name]protoreflect.FieldNumber{
		"GitHubIssueViewRequest":        {"include_comments": 2},
		"GitHubIssueLabelChangeRequest": {"number": 1, "add_labels": 2, "remove_labels": 3},
		"GitHubLabelCreateRequest":      {"name": 1, "color": 2, "description": 3},
		"GitHubLabelListRequest":        {"limit": 1},
		"GitHubUserRecord":              {"login": 1},
		"GitHubLabelRecord":             {"name": 1},
		"GitHubRepositoryRecord":        {"ssh_url": 8},
		"GitHubIssueRecord":             {"comments": 11},
	}
	for messageName, fields := range want {
		message := file.Messages().ByName(messageName)
		if message == nil {
			t.Fatalf("missing message %s", messageName)
		}
		for fieldName, fieldNumber := range fields {
			field := message.Fields().ByName(fieldName)
			if field == nil || field.Number() != fieldNumber {
				t.Errorf("%s.%s: got %v, want field %d", messageName, fieldName, field, fieldNumber)
			}
		}
	}

	assertOneofFields(t, file.Messages().ByName("GitHubRequest"), "operation", map[protoreflect.Name]protoreflect.FieldNumber{
		"current_user":       30,
		"label_list":         31,
		"label_create":       32,
		"issue_label_change": 33,
	})
	assertOneofFields(t, file.Messages().ByName("GitHubResponse"), "result", map[protoreflect.Name]protoreflect.FieldNumber{
		"current_user":       30,
		"label_list":         31,
		"label_create":       32,
		"issue_label_change": 33,
	})
}

func assertOneofFields(t *testing.T, message protoreflect.MessageDescriptor, oneofName protoreflect.Name, want map[protoreflect.Name]protoreflect.FieldNumber) {
	t.Helper()
	if message == nil {
		t.Fatalf("missing message containing %s oneof", oneofName)
	}
	oneof := message.Oneofs().ByName(oneofName)
	if oneof == nil {
		t.Fatalf("missing %s.%s oneof", message.Name(), oneofName)
	}
	for fieldName, fieldNumber := range want {
		field := oneof.Fields().ByName(fieldName)
		if field == nil || field.Number() != fieldNumber {
			t.Errorf("%s.%s: got %v, want field %d", message.Name(), fieldName, field, fieldNumber)
		}
	}
}
