package gitea

import (
	"bytes"
	"context"
	"testing"

	"github.com/rochecompaan/repowolf/internal/rpcstatus"
)

func TestReportExecutionErrorPullKind(t *testing.T) {
	var stderr bytes.Buffer
	if code := reportExecutionError(context.Background(), command{}, rpcstatus.Error(rpcstatus.ErrPullKind), &stderr); code != 1 || stderr.String() != "tea: index is an issue; use tea issues\n" {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
