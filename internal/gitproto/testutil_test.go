package gitproto

import (
	"bytes"
	"io"

	"github.com/rochecompaan/repowolf/internal/config"
)

func receiveOptions(maxBytes int) ReceiveOptions {
	return ReceiveOptions{MaxBytes: maxBytes, MaxCommands: 16, Policy: config.PushPolicy{MaxRefUpdates: 16}, AdvertisedCaps: standardCapabilities()}
}

func receiveCertificateOptions(maxBytes int) ReceiveOptions {
	return ReceiveOptions{MaxBytes: maxBytes, MaxCommands: 16, Policy: config.PushPolicy{MaxRefUpdates: 16}, AdvertisedCaps: certificateCapabilities()}
}

func standardCapabilities() Capabilities {
	return Capabilities{
		"report-status": "",
		"side-band-64k": "",
		"push-options":  "",
	}
}

func certificateCapabilities() Capabilities {
	capabilities := standardCapabilities()
	capabilities["push-cert"] = "nonce-123"
	return capabilities
}

func signedCertificate(command string) []byte {
	return signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, command)
}

func signedEnvelope(begin, end string, headers []string, command string) []byte {
	packets := [][]byte{
		packet("push-cert\x00 report-status push-options"),
		packet("certificate version 0.1\n"),
		packet("pusher Example <example@example.com> 0 +0000\n"),
		packet("pushee ssh://git@github.com/owner/repo.git\n"),
		packet("nonce nonce-123\n"),
	}
	for _, header := range headers {
		packets = append(packets, packet(header))
	}
	packets = append(packets,
		packet("\n"),
		packet(command),
		packet(begin+"\n"),
		packet("dGVzdA==\n"),
		packet(end+"\n"),
		packet("push-cert-end\n"),
		flush(),
		flush(),
	)
	return joinPackets(packets...)
}

func packet(payload string) []byte         { return []byte(hexLength(len(payload)+4) + payload) }
func flush() []byte                        { return []byte("0000") }
func joinPackets(packets ...[]byte) []byte { return bytes.Join(packets, nil) }

func hexLength(value int) string {
	const hex = "0123456789abcdef"
	return string([]byte{hex[value>>12&15], hex[value>>8&15], hex[value>>4&15], hex[value&15]})
}

type fragmentedReader struct {
	data []byte
	step int
}

func fragmented(data []byte, step int) io.Reader { return &fragmentedReader{data: data, step: step} }

func (reader *fragmentedReader) Read(destination []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	n := reader.step
	if n > len(destination) {
		n = len(destination)
	}
	if n > len(reader.data) {
		n = len(reader.data)
	}
	copy(destination, reader.data[:n])
	reader.data = reader.data[n:]
	return n, nil
}
