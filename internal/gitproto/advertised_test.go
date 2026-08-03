package gitproto

import (
	"bytes"
	"testing"
)

func TestParseAdvertisementPreservesAdvertisement(t *testing.T) {
	raw := joinPackets(
		packet(sha1A+" HEAD\x00report-status push-options object-format=sha1\n"),
		packet(sha1B+" refs/heads/main\n"),
		flush(),
	)

	result, err := ParseAdvertisement(fragmented(raw, 3), 4096)
	if err != nil {
		t.Fatalf("ParseAdvertisement() error = %v", err)
	}
	if !bytes.Equal(result.Raw, raw) || !result.Capabilities.Has("push-options") || result.Capabilities.Value("object-format") != "sha1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseAdvertisedCapabilitiesRejectsMalformedFirstReference(t *testing.T) {
	for name, raw := range map[string][]byte{
		"missing separator":  joinPackets(packet(sha1A+" HEAD"), flush()),
		"empty before flush": flush(),
		"oversize":           joinPackets(packet(sha1A+" HEAD\x00push-options"), flush()),
	} {
		t.Run(name, func(t *testing.T) {
			limit := 4096
			if name == "oversize" {
				limit = 4
			}
			result, err := ParseAdvertisement(bytes.NewReader(raw), limit)
			if err == nil {
				t.Fatal("ParseAdvertisedCapabilities() error = nil")
			}
			if result.Raw != nil || result.Capabilities != nil {
				t.Fatalf("result = %#v, want empty", result)
			}
		})
	}
}
