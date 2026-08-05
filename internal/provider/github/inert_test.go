package github

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

const hostileText = "--hostname attacker.invalid; $(touch pwned)\n--input /secret"

func TestFreeFormMutationDataRemainsInBoundedJSONStdin(t *testing.T) {
	response := `{"number":1,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":[],"html_url":"https://safe.example/1","created_at":"c","updated_at":"u"}`
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(response)}}}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	body := hostileText
	req := request(&repowolfv1.GitHubRequest_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateRequest{Title: hostileText, Body: &body, Labels: []string{"--label"}, Assignees: []string{"attacker"}}})
	if _, err := adapter.Execute(context.Background(), repository(), req); err != nil {
		t.Fatal(err)
	}
	command := caller.commands[0]
	for _, argument := range command.Args {
		if strings.Contains(argument, "attacker.invalid") || strings.Contains(argument, "touch pwned") {
			t.Fatalf("untrusted value reached argv: %#v", command.Args)
		}
	}
	var input map[string]any
	if err := json.Unmarshal(command.Stdin, &input); err != nil {
		t.Fatal(err)
	}
	if input["title"] != hostileText || input["body"] != hostileText || !bytes.Contains(command.Stdin, []byte(`--hostname attacker.invalid`)) {
		t.Fatalf("stdin = %q", command.Stdin)
	}
}

func TestInvalidRequestNeverCallsProvider(t *testing.T) {
	caller := &fakeCaller{}
	adapter, err := New(AdapterOptions{Path: "/pinned/gh", Environment: []string{}, Timeout: time.Minute, Caller: caller})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: 1, Body: ""}}))
	if err == nil {
		t.Fatal("invalid request accepted")
	}
	if len(caller.commands) != 0 {
		t.Fatalf("provider calls = %d", len(caller.commands))
	}
}
