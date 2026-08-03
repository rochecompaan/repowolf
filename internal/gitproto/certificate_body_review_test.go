package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackRejectsCertificateBodyEmbeddedControls(t *testing.T) {
	for name, signature := range map[string]string{
		"embedded CR":  "dGVzdA==\r\n",
		"embedded LF":  "dGVz\ndA==\n",
		"embedded NUL": "dGVz\x00dA==\n",
		"double LF":    "dGVzdA==\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(certificateWithSignatureBody(signature)), receiveCertificateOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func certificateWithSignatureBody(signature string) []byte {
	return joinPackets(
		packet("push-cert\x00 report-status"),
		packet("certificate version 0.1\n"),
		packet("pusher Example <example@example.com> 0 +0000\n"),
		packet("pushee ssh://git@github.com/owner/repo.git\n"),
		packet("nonce nonce-123\n"),
		packet("\n"),
		packet(zero1+" "+sha1B+" refs/heads/feature\n"),
		packet("-----BEGIN PGP SIGNATURE-----\n"),
		packet(signature),
		packet("-----END PGP SIGNATURE-----\n"),
		packet("push-cert-end\n"),
	)
}
