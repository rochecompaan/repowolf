package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackRejectsInvalidRequiredCertificateHeaderValues(t *testing.T) {
	valid := []string{
		"pusher Example Person <example@example.com> 1700000000 +0530\n",
		"pushee ssh://git@github.com/owner/repo.git\n",
		"nonce nonce-123\n",
	}
	for name, headers := range map[string][]string{
		"pusher missing email": {
			"pusher Example Person 1700000000 +0530\n", valid[1], valid[2],
		},
		"pusher nondecimal timestamp": {
			"pusher Example Person <example@example.com> now +0530\n", valid[1], valid[2],
		},
		"pusher invalid timezone": {
			"pusher Example Person <example@example.com> 1700000000 0530\n", valid[1], valid[2],
		},
		"pusher control": {
			"pusher Example\x1f <example@example.com> 1700000000 +0530\n", valid[1], valid[2],
		},
		"pushee control": {valid[0], "pushee ssh://git@github.com/owner/repo\x7f\n", valid[2]},
		"nonce empty":    {valid[0], valid[1], "nonce \n"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(certificateWithHeaders(headers)), receiveCertificateOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func TestParseReceivePackAcceptsLocalPusherIdentityEmail(t *testing.T) {
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, zero1+" "+sha1B+" refs/heads/feature\n")
	raw = bytes.Replace(raw, packet("pusher Example <example@example.com> 0 +0000\n"), packet("pusher Example <local> 0 +0000\n"), 1)

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err != nil || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
	}
}

func TestParseReceivePackFramesTwoFlushSignedPushOptionsAndCountsBothSections(t *testing.T) {
	raw := signedPushWithOptions([]string{"ci.skip"}, []string{"ci.skip"})

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(len(raw)))
	if err != nil || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
	}
	result, err = ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(len(raw)-1))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil below option-inclusive maximum")
	}
	assertNoForwardableReceiveResult(t, result)
}
