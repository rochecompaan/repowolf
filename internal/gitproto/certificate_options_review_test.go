package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackAcceptsMatchingSignedPushOptions(t *testing.T) {
	raw := signedPushWithOptions([]string{"ci.skip", "label one"}, []string{"ci.skip", "label one"})
	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err != nil || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
	}
}

func TestParseReceivePackAcceptsCertificateTerminatingFlushWithoutSelectedPushOptions(t *testing.T) {
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, zero1+" "+sha1B+" refs/heads/feature\n")
	raw = bytes.Replace(raw, packet("push-cert\x00 report-status push-options"), packet("push-cert\x00 report-status"), 1)
	raw = raw[:len(raw)-len(flush())]

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err != nil || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
	}
}

func TestParseReceivePackRequiresCertificateFlushWithoutPushOptions(t *testing.T) {
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, zero1+" "+sha1B+" refs/heads/feature\n")
	raw = raw[:len(raw)-2*len(flush())]
	raw = append(raw, flush()...)

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRejectsSignedPushOptionsMissingEitherFlush(t *testing.T) {
	withOption := signedPushWithOptions([]string{"ci.skip"}, []string{"ci.skip"})
	firstFlush := append(append([]byte(nil), flush()...), packet("ci.skip")...)
	withoutOptions := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, zero1+" "+sha1B+" refs/heads/feature\n")
	cases := map[string][]byte{
		"missing certificate flush": bytes.Replace(withOption, firstFlush, packet("ci.skip"), 1),
		"missing terminal flush":    withoutOptions[:len(withoutOptions)-len(flush())],
	}
	for name, malformed := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(malformed), receiveCertificateOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func TestParseReceivePackRejectsMismatchedSignedPushOptions(t *testing.T) {
	cases := map[string]struct {
		headers []string
		options []string
	}{
		"missing": {
			headers: []string{"ci.skip"},
			options: nil,
		},
		"extra": {
			headers: []string{"ci.skip"},
			options: []string{"ci.skip", "extra"},
		},
		"reordered": {
			headers: []string{"first", "second"},
			options: []string{"second", "first"},
		},
		"changed": {
			headers: []string{"ci.skip"},
			options: []string{"ci.run"},
		},
		"empty trailing": {
			headers: []string{"ci.skip"},
			options: []string{""},
		},
		"tab trailing": {
			headers: []string{"ci.skip"},
			options: []string{"ci\tskip"},
		},
		"nonascii trailing": {
			headers: []string{"ci.skip"},
			options: []string{"café"},
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(signedPushWithOptions(testCase.headers, testCase.options)), receiveCertificateOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func signedPushWithOptions(headers, options []string) []byte {
	certificateHeaders := make([]string, len(headers))
	for index, option := range headers {
		certificateHeaders[index] = "push-option " + option + "\n"
	}
	raw := signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", certificateHeaders, zero1+" "+sha1B+" refs/heads/feature\n")
	raw = raw[:len(raw)-len(flush())]
	for _, option := range options {
		raw = append(raw, packet(option)...)
	}
	return append(raw, flush()...)
}
