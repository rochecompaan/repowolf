package gitea

import (
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderIssueMutation(t *testing.T) {
	issue := issueFixture()
	issue.Title = "created title"
	issue.State = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN
	issue.Url = "https://gitea.example/Owner/Repo/issues/7"
	for _, test := range []struct {
		format outputFormat
		want   string
	}{{outputSimple, "index: 7\ntitle: created title\nstate: open\nurl: https://gitea.example/Owner/Repo/issues/7\n"}, {outputTable, "index\ttitle\tstate\turl\n7\tcreated title\topen\thttps://gitea.example/Owner/Repo/issues/7\n"}, {outputJSON, "{\"index\":7,\"title\":\"created title\",\"state\":\"open\",\"url\":\"https://gitea.example/Owner/Repo/issues/7\"}\n"}} {
		out, err := renderIssueMutation(test.format, issue)
		if err != nil || string(out) != test.want {
			t.Fatalf("out=%q err=%v", out, err)
		}
	}
}
func TestRenderCommentMutation(t *testing.T) {
	comment := &repowolfv1.GiteaCommentRecord{Id: 9, AuthorId: 2, Author: "alice", Url: "https://g/c/9", Body: "line1\nline2", Created: timestamppb.New(time.Unix(1, 0)), Updated: timestamppb.New(time.Unix(2, 0))}
	out, err := renderCommentMutation(outputJSON, comment)
	if err != nil || string(out) != "{\"id\":9,\"url\":\"https://g/c/9\",\"body\":\"line1\\nline2\"}\n" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	out, err = renderCommentMutation(outputSimple, comment)
	if err != nil || !strings.Contains(string(out), "body: line1\nline2\n") {
		t.Fatalf("out=%q err=%v", out, err)
	}
}
func TestMutationSimpleOutputSanitizesProviderControlledFields(t *testing.T) {
	issue := issueFixture()
	issue.Title = "safe\n\x1b[31munsafe"
	issue.Url = "https://g.example/issues/7\rspoofed"
	out, err := renderIssueMutation(outputSimple, issue)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(out), "\r\x1b") || strings.Contains(string(out), "title: safe\n") {
		t.Fatalf("unsafe issue output %q", out)
	}

	comment := &repowolfv1.GiteaCommentRecord{Id: 9, AuthorId: 2, Author: "alice", Url: "https://g/c/9\nspoofed", Body: "literal\nbody", Created: timestamppb.New(time.Unix(1, 0)), Updated: timestamppb.New(time.Unix(2, 0))}
	out, err = renderCommentMutation(outputSimple, comment)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "url: https://g/c/9\n") || !strings.Contains(string(out), "body: literal\nbody\n") {
		t.Fatalf("unexpected comment output %q", out)
	}
}

func TestMutationRenderRejectsIncompleteRecord(t *testing.T) {
	issue := issueFixture()
	issue.Title = ""
	if _, err := renderIssueMutation(outputJSON, issue); err == nil {
		t.Fatal("accepted incomplete issue")
	}
	if _, err := renderCommentMutation(outputJSON, nil); err == nil {
		t.Fatal("accepted nil comment")
	}
}
