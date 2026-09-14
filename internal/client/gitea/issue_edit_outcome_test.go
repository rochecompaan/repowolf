package gitea

import (
	"bytes"
	"context"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func editOutcomeCommand() command {
	return command{
		request:  &repowolfv1.GiteaRequest{Operation: &repowolfv1.GiteaRequest_IssueEdit{IssueEdit: &repowolfv1.GiteaIssueEditRequest{Index: 7}}},
		mutation: true,
	}
}

func TestIssueEditOutcomeDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "partial", err: rpcstatus.Error(rpcstatus.ErrEditPartial), want: "tea: issue edit partially applied; inspect issue state before retrying\n"},
		{name: "unknown", err: rpcstatus.Error(rpcstatus.ErrWriteOutcomeUnknown), want: "tea: write outcome unknown; inspect repository state before retrying\n"},
		{name: "untrusted near match", err: status.Error(codes.FailedPrecondition, "issue edit partially applied secret"), want: "tea: Gitea operation failed\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := reportExecutionError(context.Background(), editOutcomeCommand(), test.err, &stderr); code != 1 || stderr.String() != test.want {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
		})
	}
}
