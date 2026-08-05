package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackRejectsCertificatePushOptionWithoutClientSelection(t *testing.T) {
	headers := []string{
		"pusher Example <example@example.com> 0 +0000\n",
		"pushee ssh://git@github.com/owner/repo.git\n",
		"nonce nonce-123\n",
		"push-option ci.skip\n",
	}
	result, err := ParseReceivePack(bytes.NewReader(certificateWithHeaders(headers)), receiveCertificateOptions(4096))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRequiresCertificateHeaderOrder(t *testing.T) {
	cases := map[string][]string{
		"reordered required headers": {
			"pushee ssh://git@github.com/owner/repo.git\n",
			"pusher Example <example@example.com> 0 +0000\n",
			"nonce nonce-123\n",
		},
		"push option before required headers": {
			"pusher Example <example@example.com> 0 +0000\n",
			"push-option ci.skip\n",
			"pushee ssh://git@github.com/owner/repo.git\n",
			"nonce nonce-123\n",
		},
		"duplicate required header": {
			"pusher Example <example@example.com> 0 +0000\n",
			"pusher Other <other@example.com> 0 +0000\n",
			"pushee ssh://git@github.com/owner/repo.git\n",
			"nonce nonce-123\n",
		},
		"embedded CR": {
			"pusher Example <example@example.com> 0 +0000\r\n",
			"pushee ssh://git@github.com/owner/repo.git\n",
			"nonce nonce-123\n",
		},
		"extra terminal LF": {
			"pusher Example <example@example.com> 0 +0000\n\n",
			"pushee ssh://git@github.com/owner/repo.git\n",
			"nonce nonce-123\n",
		},
		"embedded NUL": {
			"pusher Example\x00 <example@example.com> 0 +0000\n",
			"pushee ssh://git@github.com/owner/repo.git\n",
			"nonce nonce-123\n",
		},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(certificateWithHeaders(headers)), receiveCertificateOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func certificateWithHeaders(headers []string) []byte {
	packets := [][]byte{
		packet("push-cert\x00 report-status"),
		packet("certificate version 0.1\n"),
	}
	for _, header := range headers {
		packets = append(packets, packet(header))
	}
	packets = append(packets,
		packet("\n"),
		packet(zero1+" "+sha1B+" refs/heads/feature\n"),
		packet("-----BEGIN PGP SIGNATURE-----\n"),
		packet("dGVzdA==\n"),
		packet("-----END PGP SIGNATURE-----\n"),
		packet("push-cert-end\n"),
	)
	return joinPackets(packets...)
}
