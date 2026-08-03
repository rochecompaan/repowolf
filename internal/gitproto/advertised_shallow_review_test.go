package gitproto

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseAdvertisementAcceptsCapturedShallowAdvertisement(t *testing.T) {
	raw := joinPackets(
		packet(sha1A+" HEAD\x00report-status\n"),
		packet(sha1B+" refs/heads/feature\n"),
		packet("shallow "+sha1A+"\n"),
		flush(),
	)
	result, err := ParseAdvertisement(fragmented(raw, 1), 4096)
	if err != nil || !bytes.Equal(result.Raw, raw) {
		t.Fatalf("ParseAdvertisement() = (%#v, %v)", result, err)
	}
}

func TestParseAdvertisedCapabilitiesAcceptsUniqueSHA256Shallows(t *testing.T) {
	shallowA := strings.Repeat("c", 64)
	shallowB := strings.Repeat("d", 64)
	raw := joinPackets(
		packet(sha256A+" HEAD\x00report-status object-format=sha256\n"),
		packet(sha256B+" refs/heads/feature\n"),
		packet("shallow "+shallowA+"\n"),
		packet("shallow "+shallowB+"\n"),
		flush(),
	)
	result, err := ParseAdvertisement(fragmented(raw, 2), 4096)
	if err != nil || !bytes.Equal(result.Raw, raw) {
		t.Fatalf("ParseAdvertisedCapabilities() = (%#v, %v)", result, err)
	}
}

func TestParseAdvertisedCapabilitiesRejectsInvalidShallowAdvertisement(t *testing.T) {
	for name, raw := range map[string][]byte{
		"duplicate": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+sha1B+"\n"),
			packet("shallow "+sha1B+"\n"),
			flush(),
		),
		"wrong SHA256 width": joinPackets(
			packet(sha256A+" HEAD\x00report-status object-format=sha256\n"),
			packet("shallow "+sha1A+"\n"),
			flush(),
		),
		"missing LF": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+sha1B),
			flush(),
		),
		"embedded LF": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+sha1B[:20]+"\n"+sha1B[20:]+"\n"),
			flush(),
		),
		"CR": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+sha1B+"\r\n"),
			flush(),
		),
		"invalid object": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+strings.Repeat("g", 40)+"\n"),
			flush(),
		),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseAdvertisement(bytes.NewReader(raw), 4096)
			if err == nil {
				t.Fatal("ParseAdvertisedCapabilities() error = nil")
			}
			if result.Raw != nil || result.Capabilities != nil {
				t.Fatalf("result = %#v, want no forwardable advertisement", result)
			}
		})
	}
}
