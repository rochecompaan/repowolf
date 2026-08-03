package gitproto

import (
	"bytes"
	"testing"
)

func TestParseCapabilitiesAcceptsExactlyOneLeadingCapabilitySpace(t *testing.T) {
	capabilities, err := ParseCapabilities(" report-status push-options")
	if err != nil || !capabilities.Has("report-status") || !capabilities.Has("push-options") {
		t.Fatalf("ParseCapabilities() = (%#v, %v)", capabilities, err)
	}
	for _, malformed := range []string{" ", "  report-status", "report-status ", "report-status  push-options"} {
		if _, err := ParseCapabilities(malformed); err == nil {
			t.Errorf("ParseCapabilities(%q) error = nil", malformed)
		}
	}
}

func TestParseReceivePackAcceptsCapturedGitV250SendPackFraming(t *testing.T) {
	for name, raw := range map[string][]byte{
		"ordinary":        capturedV250OrdinaryPush(nil),
		"ordinary option": capturedV250OrdinaryPush([]string{"ci.skip"}),
		"signed":          capturedV250SignedPush(nil),
		"signed option":   capturedV250SignedPush([]string{"ci.skip"}),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
			if err != nil || !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 {
				t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
			}
		})
	}
}

func capturedV250OrdinaryPush(options []string) []byte {
	capabilities := " report-status"
	if len(options) != 0 {
		capabilities += " push-options"
	}
	packets := [][]byte{
		packet(sha1A + " " + sha1B + " refs/heads/feature\x00" + capabilities),
		flush(),
	}
	for _, option := range options {
		packets = append(packets, packet(option))
	}
	if len(options) != 0 {
		packets = append(packets, flush())
	}
	return joinPackets(packets...)
}

func capturedV250SignedPush(options []string) []byte {
	capabilities := " report-status push-cert=nonce-123"
	headers := [][]byte{}
	if len(options) != 0 {
		capabilities += " push-options"
		for _, option := range options {
			headers = append(headers, packet("push-option "+option+"\n"))
		}
	}
	packets := [][]byte{
		packet("push-cert\x00" + capabilities),
		packet("certificate version 0.1\n"),
		packet("pusher Example <example@example.com> 0 +0000\n"),
		packet("pushee ssh://git@github.com/owner/repo.git\n"),
		packet("nonce nonce-123\n"),
	}
	packets = append(packets, headers...)
	packets = append(packets,
		packet("\n"),
		packet(zero1+" "+sha1B+" refs/heads/feature\n"),
		packet("-----BEGIN PGP SIGNATURE-----\n"),
		packet("dGVzdA==\n"),
		packet("-----END PGP SIGNATURE-----\n"),
		packet("push-cert-end\n"),
		flush(),
	)
	for _, option := range options {
		packets = append(packets, packet(option))
	}
	if len(options) != 0 {
		packets = append(packets, flush())
	}
	return joinPackets(packets...)
}
