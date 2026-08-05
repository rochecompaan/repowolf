package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/runner"
)

func TestPullChecksRejectsEmptyAdvertisedAndOverReturnedPagesWithoutPartialResult(t *testing.T) {
	head := runner.Result{Stdout: []byte(`{"head":{"sha":"0123456789012345678901234567890123456789"}}`)}
	checks0 := runner.Result{Stdout: []byte(checkPageJSON("check_runs", 0, 0))}
	tests := []struct {
		name      string
		results   []runner.Result
		wantCalls int
		limit     bool
	}{
		{
			name:      "empty advertised check-runs page",
			results:   []runner.Result{head, {Stdout: []byte(checkPageJSON("check_runs", 101, 100))}, {Stdout: []byte(checkPageJSON("check_runs", 101, 0))}},
			wantCalls: 3,
		},
		{
			name:      "empty advertised statuses page",
			results:   []runner.Result{head, checks0, {Stdout: []byte(checkPageJSON("statuses", 101, 100))}, {Stdout: []byte(checkPageJSON("statuses", 101, 0))}},
			wantCalls: 4,
		},
		{
			name:      "over-returned check-runs page",
			results:   []runner.Result{head, {Stdout: []byte(checkPageJSON("check_runs", 1, 2))}},
			wantCalls: 2,
			limit:     true,
		},
		{
			name:      "over-returned statuses page",
			results:   []runner.Result{head, checks0, {Stdout: []byte(checkPageJSON("statuses", 1, 2))}},
			wantCalls: 3,
			limit:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := &fakeCaller{results: test.results}
			response, err := testAdapter(t, caller).Execute(context.Background(), repository(), request(&repowolfv1.GitHubRequest_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksRequest{Number: 1}}))
			if response != nil || err == nil {
				t.Fatalf("Execute() = %#v, %v, want no partial response", response, err)
			}
			if test.limit && !errors.Is(err, runner.ErrOutputLimit) {
				t.Fatalf("Execute() error = %v, want output limit", err)
			}
			if !test.limit && !strings.Contains(err.Error(), "complete") {
				t.Fatalf("Execute() error = %v, want incomplete response", err)
			}
			if len(caller.commands) != test.wantCalls {
				t.Fatalf("commands = %d, want %d", len(caller.commands), test.wantCalls)
			}
		})
	}
}
