package github

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

// Regression: an issue label change uses one fixed preflight and one fixed
// replacement update, with label values confined to JSON stdin.
func TestIssueLabelChangeUsesOneTypedUpdate(t *testing.T) {
	preflight := `{"labels":[{"name":"bug"},{"name":"patchmill:queued"}]}`
	updated := issueLabelChangeFixture(`[{"name":"bug"},{"name":"patchmill:ready"}]`)
	caller := &fakeCaller{results: []runner.Result{{Stdout: []byte(preflight)}, {Stdout: []byte(updated)}}}
	request := issueLabelChangeRequest(&repowolfv1.GitHubIssueLabelChangeRequest{
		Number:       18,
		AddLabels:    []string{"patchmill:ready", "bug"},
		RemoveLabels: []string{"patchmill:queued"},
	})

	response, err := testAdapter(t, caller).Execute(context.Background(), repository(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got := response.GetIssueLabelChange().GetIssue().GetLabels(); !reflect.DeepEqual(got, []string{"bug", "patchmill:ready"}) {
		t.Fatalf("response labels = %#v", got)
	}
	want := []runner.Command{
		expectedAPI("GET", "/repos/owner/repo/issues/18", nil, maximumMutationBytes),
		expectedAPI("PATCH", "/repos/owner/repo/issues/18", []byte(`{"labels":["bug","patchmill:ready"]}`), maximumMutationBytes-len(preflight)),
	}
	if !reflect.DeepEqual(caller.commands, want) {
		t.Fatalf("commands = %#v\nwant %#v", caller.commands, want)
	}
	for _, command := range caller.commands {
		args := strings.Join(command.Args, "\x00")
		for _, label := range []string{"patchmill:ready", "bug", "patchmill:queued"} {
			if strings.Contains(args, label) {
				t.Fatalf("label %q escaped JSON stdin into command args %#v", label, command.Args)
			}
		}
	}
}

// Regression: preflight rejects pull requests and untrustworthy provider label
// sets before issuing any mutation.
func TestIssueLabelChangeRejectsPullRequestAndInvalidCurrentLabels(t *testing.T) {
	objectLabels := providerLabelsFixture(t, numberedLabels(100))

	tests := []struct {
		name      string
		preflight []byte
		add       string
	}{
		{"pull request marker", []byte(`{"labels":[],"pull_request":{}}`), "new"},
		{"duplicate provider labels", []byte(`{"labels":[{"name":"bug"},{"name":"bug"}]}`), "new"},
		{"missing labels", []byte(`{"number":18}`), "new"},
		{"missing label name", []byte(`{"labels":[{}]}`), "new"},
		{"empty label name", []byte(`{"labels":[{"name":""}]}`), "new"},
		{"invalid UTF-8", []byte("{\"labels\":[{\"name\":\"\xff\"}]}"), "new"},
		{"more than 100 computed labels", []byte(`{"labels":` + objectLabels + `}`), "label-100"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: []runner.Result{{Stdout: test.preflight}}}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueLabelChangeRequest(&repowolfv1.GitHubIssueLabelChangeRequest{Number: 18, AddLabels: []string{test.add}}))
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v, want rejection", response, err)
			}
			if len(caller.commands) != 1 {
				t.Fatalf("commands = %d, want preflight only", len(caller.commands))
			}
		})
	}
}

// Regression: malformed mutation output cannot produce a typed result.
func TestIssueLabelChangeRejectsMalformedUpdateOutput(t *testing.T) {
	caller := &fakeCaller{results: []runner.Result{
		{Stdout: []byte(`{"labels":[{"name":"bug"}]}`)},
		{Stdout: []byte(`{"number":18}`)},
	}}
	response, err := testAdapter(t, caller).Execute(context.Background(), repository(), issueLabelChangeRequest(&repowolfv1.GitHubIssueLabelChangeRequest{Number: 18, AddLabels: []string{"ready"}}))
	if response != nil || err == nil {
		t.Fatalf("Execute() = %#v, %v, want malformed response rejection", response, err)
	}
	if len(caller.commands) != 2 {
		t.Fatalf("commands = %d, want preflight and mutation", len(caller.commands))
	}
}

// Regression: cancellation observed after preflight prevents the PATCH call.
func TestIssueLabelChangeStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	caller := &cancelAfterFirstCaller{
		fake:   &fakeCaller{results: []runner.Result{{Stdout: []byte(`{"labels":[{"name":"bug"}]}`)}}},
		cancel: cancel,
	}
	response, err := testAdapter(t, caller).Execute(ctx, repository(), issueLabelChangeRequest(&repowolfv1.GitHubIssueLabelChangeRequest{Number: 18, AddLabels: []string{"ready"}}))
	if response != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() = %#v, %v, want context cancellation", response, err)
	}
	if len(caller.fake.commands) != 1 {
		t.Fatalf("commands = %d, want preflight only", len(caller.fake.commands))
	}
}

// Regression: label replacement is stable and deduplicates requested additions
// while rejecting duplicate provider state and oversized final sets.
func TestIssueLabelChangeComputesDeterministicReplacement(t *testing.T) {
	got, err := changedLabels(
		[]string{"bug", "patchmill:queued"},
		[]string{"patchmill:ready", "bug"},
		[]string{"patchmill:queued"},
	)
	if err != nil || !reflect.DeepEqual(got, []string{"bug", "patchmill:ready"}) {
		t.Fatalf("changedLabels() = %#v, %v", got, err)
	}
	if got, err := changedLabels([]string{"bug", "bug"}, nil, nil); got != nil || err == nil {
		t.Fatalf("duplicate changedLabels() = %#v, %v", got, err)
	}
	if got, err := changedLabels(numberedLabels(100), []string{"label-100"}, nil); got != nil || err == nil {
		t.Fatalf("oversized changedLabels() = %#v, %v", got, err)
	}
}

func issueLabelChangeRequest(change *repowolfv1.GitHubIssueLabelChangeRequest) *repowolfv1.GitHubRequest {
	return &repowolfv1.GitHubRequest{Operation: &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: change}}
}

func issueLabelChangeFixture(labels string) string {
	return `{"number":18,"title":"title","body":"body","state":"open","user":{"login":"me"},"assignees":[],"labels":` + labels + `,"html_url":"https://safe.example/18","created_at":"c","updated_at":"u"}`
}

func providerLabelsFixture(t *testing.T, names []string) string {
	t.Helper()
	labels := make([]map[string]string, len(names))
	for index, name := range names {
		labels[index] = map[string]string{"name": name}
	}
	raw, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

type cancelAfterFirstCaller struct {
	fake   *fakeCaller
	cancel context.CancelFunc
}

func (caller *cancelAfterFirstCaller) Call(ctx context.Context, command runner.Command) (runner.Result, error) {
	result, err := caller.fake.Call(ctx, command)
	caller.cancel()
	return result, err
}
