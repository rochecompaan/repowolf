package cli_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/rochecompaan/repowolf/internal/cli"
)

func TestRunTokenGenerateWritesGeneratedToken(t *testing.T) {
	var stdout bytes.Buffer
	entropy := bytes.Repeat([]byte{0x6b}, 32)

	if err := cli.RunTokenGenerate(&stdout, bytes.NewReader(entropy)); err != nil {
		t.Fatalf("RunTokenGenerate() error = %v", err)
	}
	if len(stdout.String()) != 48 {
		t.Fatal("RunTokenGenerate() wrote an invalid token")
	}
}

func TestRunTokenGenerateReturnsWriterError(t *testing.T) {
	if err := cli.RunTokenGenerate(errorWriter{}, bytes.NewReader(make([]byte, 32))); !errors.Is(err, errWrite) {
		t.Fatalf("RunTokenGenerate() error = %v, want writer error", err)
	}
}

var errWrite = errors.New("write failed")

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errWrite
}

var _ io.Writer = errorWriter{}
