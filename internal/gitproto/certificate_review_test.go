package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackAcceptsCertificatePushOptionHeader(t *testing.T) {
	raw := signedPushWithOptions([]string{"ci.skip"}, []string{"ci.skip"})

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseReceivePackAcceptsSupportedSignatureEnvelopes(t *testing.T) {
	for name, envelope := range map[string][2]string{
		"openpgp":         {"-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----"},
		"openpgp RFC1991": {"-----BEGIN PGP MESSAGE-----", "-----END PGP MESSAGE-----"},
		"ssh":             {"-----BEGIN SSH SIGNATURE-----", "-----END SSH SIGNATURE-----"},
		"x509":            {"-----BEGIN SIGNED MESSAGE-----", "-----END SIGNED MESSAGE-----"},
	} {
		t.Run(name, func(t *testing.T) {
			raw := signedEnvelope(envelope[0], envelope[1], nil, zero1+" "+sha1B+" refs/heads/feature\n")
			result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
			if err != nil {
				t.Fatalf("ParseReceivePack() error = %v", err)
			}
			if !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestParseReceivePackRejectsCertificateNonceDifferentFromAdvertisement(t *testing.T) {
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, zero1+" "+sha1B+" refs/heads/feature\n")
	raw = bytes.Replace(raw, []byte("nonce nonce-123\n"), []byte("nonce wrong-123\n"), 1)

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	if result.Prefix != nil || result.Updates != nil || result.Capabilities != nil {
		t.Fatalf("result = %#v, want no forwardable result", result)
	}
}

func TestParseReceivePackRejectsCertificatePushOptionWithoutAdvertisement(t *testing.T) {
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", []string{
		"push-option ci.skip\n",
	}, zero1+" "+sha1B+" refs/heads/feature\n")
	options := receiveCertificateOptions(4096)
	delete(options.AdvertisedCaps, "push-options")

	result, err := ParseReceivePack(bytes.NewReader(raw), options)
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	if result.Prefix != nil || result.Updates != nil || result.Capabilities != nil {
		t.Fatalf("result = %#v, want no forwardable result", result)
	}
}
