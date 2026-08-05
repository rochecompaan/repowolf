package gitproto

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseReceivePackRejectsCaseVariantDuplicateShallows(t *testing.T) {
	for name, testCase := range map[string]struct {
		raw     []byte
		options ReceiveOptions
	}{
		"SHA-1": {
			raw: joinPackets(
				packet("shallow "+strings.Repeat("a", 40)+"\n"),
				packet("shallow "+strings.Repeat("A", 40)+"\n"),
				packet(sha1A+" "+sha1B+" refs/heads/feature\x00 report-status"),
				flush(),
			),
			options: receiveOptions(4096),
		},
		"SHA-256": {
			raw: joinPackets(
				packet("shallow "+strings.Repeat("a", 64)+"\n"),
				packet("shallow "+strings.Repeat("A", 64)+"\n"),
				packet(sha256A+" "+sha256B+" refs/heads/feature\x00 report-status object-format=sha256"),
				flush(),
			),
			options: ReceiveOptions{
				MaxBytes:       4096,
				MaxCommands:    16,
				AdvertisedCaps: Capabilities{"report-status": "", "object-format": "sha256"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(testCase.raw), testCase.options)
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func TestParseAdvertisementRejectsCaseVariantDuplicateShallows(t *testing.T) {
	for name, raw := range map[string][]byte{
		"SHA-1": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet("shallow "+strings.Repeat("a", 40)+"\n"),
			packet("shallow "+strings.Repeat("A", 40)+"\n"),
			flush(),
		),
		"SHA-256": joinPackets(
			packet(sha256A+" HEAD\x00report-status object-format=sha256\n"),
			packet("shallow "+strings.Repeat("a", 64)+"\n"),
			packet("shallow "+strings.Repeat("A", 64)+"\n"),
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
