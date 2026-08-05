package gitproto

import (
	"bytes"
	"testing"
)

func TestPacketReaderAcceptsProtocolMaximumTotalLength(t *testing.T) {
	payload := bytes.Repeat([]byte{'x'}, maximumPacketLength-4)
	raw := append([]byte("fff0"), payload...)
	reader, err := newPacketReader(bytes.NewReader(raw), maximumPacketLength)
	if err != nil {
		t.Fatalf("newPacketReader() error = %v", err)
	}
	got, flush, err := reader.read()
	if err != nil || flush || !bytes.Equal(got, payload) {
		t.Fatalf("read() = (%d bytes, flush %t, error %v)", len(got), flush, err)
	}
}

func TestPacketReaderRejectsPacketAboveProtocolMaximumBeforePayloadRead(t *testing.T) {
	reader, err := newPacketReader(&headerThenPanicReader{header: []byte("fff1")}, maximumPacketLength+1)
	if err != nil {
		t.Fatalf("newPacketReader() error = %v", err)
	}
	_, _, err = reader.read()
	if err == nil {
		t.Fatal("read() error = nil")
	}
}
