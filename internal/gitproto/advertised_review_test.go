package gitproto

import (
	"bytes"
	"testing"
)

func TestParseAdvertisementParsesWireRealFirstReference(t *testing.T) {
	raw := joinPackets(
		packet(sha256A+" HEAD\x00report-status push-cert=nonce-123 object-format=sha256 agent=git/2.45\n"),
		packet(sha256B+" refs/heads/main\n"),
		flush(),
	)

	result, err := ParseAdvertisement(fragmented(raw, 1), 4096)
	if err != nil {
		t.Fatalf("ParseAdvertisement() error = %v", err)
	}
	if !bytes.Equal(result.Raw, raw) || !result.Capabilities.Has("push-cert") || result.Capabilities.Value("object-format") != "sha256" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseAdvertisedCapabilitiesAcceptsEmptyCapabilityList(t *testing.T) {
	raw := joinPackets(packet(sha1A+" HEAD\x00\n"), flush())
	result, err := ParseAdvertisement(bytes.NewReader(raw), 4096)
	if err != nil {
		t.Fatalf("ParseAdvertisedCapabilities() error = %v", err)
	}
	if !bytes.Equal(result.Raw, raw) || len(result.Capabilities) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseAdvertisedCapabilitiesRejectsInvalidFirstReferenceShape(t *testing.T) {
	for name, first := range map[string]string{
		"invalid object":      "not-an-oid HEAD\x00report-status\n",
		"unqualified ref":     sha1A + " main\x00report-status\n",
		"missing terminal LF": sha1A + " HEAD\x00report-status",
		"extra terminal LF":   sha1A + " HEAD\x00report-status\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			raw := joinPackets(packet(first), flush())
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
