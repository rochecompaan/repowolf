package gitea

import (
	"bytes"
	"context"
	"testing"
)

func TestRunRejectsUnsupportedInputWithoutEcho(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := Run(context.Background(), []string{"login", "secret", "-r", "secret"}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || stderr.String() != usage {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestRunReportsConfigurationFailure(t *testing.T) {
	t.Setenv("REPOWOLF_ENDPOINT", "")
	var stderr bytes.Buffer
	status := Run(context.Background(), []string{"repos", "o/r", "-r", "o/r"}, &bytes.Buffer{}, &stderr)
	if status != 1 || stderr.String() != "tea: client configuration failed\n" {
		t.Fatalf("status=%d stderr=%q", status, stderr.String())
	}
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }
func TestWriteExactRejectsShortWrite(t *testing.T) {
	if writeExact(shortWriter{}, []byte("x")) == nil {
		t.Fatal("short write accepted")
	}
}
