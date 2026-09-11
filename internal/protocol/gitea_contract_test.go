package protocol_test

import (
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"testing"
)

func TestGiteaIssueProtocolBranchesAndPresence(t *testing.T) {
	request := (&repowolfv1.GiteaRequest{}).ProtoReflect().Descriptor()
	for _, tc := range []struct {
		name   string
		number int
	}{{"issue_list", 11}, {"issue_view", 12}} {
		f := request.Fields().ByName(protoreflect.Name(tc.name))
		if f == nil || int(f.Number()) != tc.number || f.ContainingOneof() == nil {
			t.Fatalf("request %s", tc.name)
		}
	}
	response := (&repowolfv1.GiteaResponse{}).ProtoReflect().Descriptor()
	for _, tc := range []struct {
		name   string
		number int
	}{{"issue_list", 11}, {"issue_view", 12}} {
		f := response.Fields().ByName(protoreflect.Name(tc.name))
		if f == nil || int(f.Number()) != tc.number {
			t.Fatalf("response %s", tc.name)
		}
	}
	empty := ""
	in := &repowolfv1.GiteaIssueListRequest{Keyword: &empty}
	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out := new(repowolfv1.GiteaIssueListRequest)
	if err := proto.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
	if out.Keyword == nil || out.GetKeyword() != "" {
		t.Fatal("optional empty presence lost")
	}
}
