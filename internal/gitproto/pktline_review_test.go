package gitproto

import (
	"strings"
	"testing"
)

func TestPacketReaderRejectsDeclaredPayloadAboveRemainingLimitBeforeRead(t *testing.T) {
	reader, err := newPacketReader(&headerThenPanicReader{header: []byte("ffff")}, 64)
	if err != nil {
		t.Fatalf("newPacketReader() error = %v", err)
	}
	_, _, err = reader.read()
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("read() error = %v, want configured-limit error", err)
	}
}

type headerThenPanicReader struct {
	header []byte
	used   bool
}

func (reader *headerThenPanicReader) Read(destination []byte) (int, error) {
	if reader.used {
		panic("pkt-line payload was read after declared limit rejection")
	}
	reader.used = true
	copy(destination, reader.header)
	return len(reader.header), nil
}
