package gitproto

import (
	"bytes"
	"fmt"
	"io"
)

const maximumPacketLength = 65520

type packetReader struct {
	reader io.Reader
	limit  int
	raw    bytes.Buffer
}

func newPacketReader(reader io.Reader, limit int) (*packetReader, error) {
	if limit < 4 {
		return nil, fmt.Errorf("receive-pack byte limit must allow a pkt-line header")
	}
	return &packetReader{reader: reader, limit: limit}, nil
}

func (reader *packetReader) read() ([]byte, bool, error) {
	var header [4]byte
	if err := reader.readExact(header[:]); err != nil {
		return nil, false, fmt.Errorf("read pkt-line header: %w", err)
	}
	length, err := parsePacketLength(header[:])
	if err != nil {
		return nil, false, err
	}
	if length == 0 {
		return nil, true, nil
	}
	if length < 4 {
		return nil, false, fmt.Errorf("invalid pkt-line length %d", length)
	}
	if length > maximumPacketLength {
		return nil, false, fmt.Errorf("pkt-line length %d exceeds protocol maximum %d", length, maximumPacketLength)
	}
	if length-4 > reader.limit-reader.raw.Len() {
		return nil, false, fmt.Errorf("receive-pack prefix exceeds %d bytes", reader.limit)
	}
	payload := make([]byte, length-4)
	if err := reader.readExact(payload); err != nil {
		return nil, false, fmt.Errorf("read pkt-line payload: %w", err)
	}
	return payload, false, nil
}

func (reader *packetReader) readExact(destination []byte) error {
	if len(destination) > reader.limit-reader.raw.Len() {
		return fmt.Errorf("receive-pack prefix exceeds %d bytes", reader.limit)
	}
	if _, err := io.ReadFull(reader.reader, destination); err != nil {
		return err
	}
	_, _ = reader.raw.Write(destination)
	return nil
}

func (reader *packetReader) bytes() []byte {
	return append([]byte(nil), reader.raw.Bytes()...)
}

func parsePacketLength(header []byte) (int, error) {
	if len(header) != 4 {
		return 0, fmt.Errorf("pkt-line header has length %d", len(header))
	}
	value := 0
	for _, character := range header {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += int(character - '0')
		case character >= 'a' && character <= 'f':
			value += int(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += int(character-'A') + 10
		default:
			return 0, fmt.Errorf("invalid pkt-line length %q", header)
		}
	}
	return value, nil
}
