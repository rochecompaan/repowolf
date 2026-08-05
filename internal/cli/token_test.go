package cli_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
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

func TestRunTokenGenerateDoesNotDiscloseWriterPayload(t *testing.T) {
	writer := &payloadErrorWriter{}
	err := cli.RunTokenGenerate(writer, bytes.NewReader(make([]byte, 32)))
	if err == nil {
		t.Fatal("RunTokenGenerate() returned nil error")
	}
	if writer.payload == "" {
		t.Fatal("writer did not receive the generated token")
	}
	if err.Error() != "failed to write generated token" {
		t.Fatal("RunTokenGenerate() did not return a fixed local error")
	}
	if strings.Contains(err.Error(), writer.payload) || strings.Contains(err.Error(), "payload-derived") {
		t.Fatal("RunTokenGenerate() disclosed writer-provided text")
	}
}

type payloadErrorWriter struct {
	payload string
}

func (w *payloadErrorWriter) Write(payload []byte) (int, error) {
	w.payload = string(payload)
	return 0, errors.New("payload-derived failure: " + w.payload)
}

var _ io.Writer = (*payloadErrorWriter)(nil)
