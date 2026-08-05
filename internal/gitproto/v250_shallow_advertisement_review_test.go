package gitproto

import (
	"bytes"
	"errors"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
)

func TestParseReceivePackAcceptsCapturedGitV250ShallowPayload(t *testing.T) {
	raw := capturedV250ShallowPush("refs/heads/feature")
	result, err := ParseReceivePack(fragmented(raw, 1), receiveOptions(4096))
	if err != nil || !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 {
		t.Fatalf("ParseReceivePack() = (%#v, %v)", result, err)
	}
}

func TestParseReceivePackAppliesPolicyAfterCapturedGitV250ShallowPayload(t *testing.T) {
	raw := capturedV250ShallowPush("refs/heads/main")
	result, err := ParseReceivePack(bytes.NewReader(raw), ReceiveOptions{
		MaxBytes:       4096,
		MaxCommands:    16,
		AdvertisedCaps: receiveOptions(4096).AdvertisedCaps,
		Policy:         config.PushPolicy{MaxRefUpdates: 16, DenyRefs: []string{"refs/heads/main"}},
	})
	if !errors.Is(err, policy.ErrRefPolicy) {
		t.Fatalf("ParseReceivePack() error = %v, want policy.ErrRefPolicy", err)
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRejectsMalformedGitV250ShallowPayload(t *testing.T) {
	for name, shallow := range map[string]string{
		"missing terminal LF": "shallow " + sha1A,
		"multiple LF":         "shallow " + sha1A + "\n\n",
		"embedded LF":         "shallow " + sha1A[:20] + "\n" + sha1A[20:] + "\n",
		"CR":                  "shallow " + sha1A + "\r\n",
		"NUL":                 "shallow " + sha1A + "\x00\n",
	} {
		t.Run(name, func(t *testing.T) {
			raw := joinPackets(packet(shallow), packet(sha1A+" "+sha1B+" refs/heads/feature\x00 report-status"), flush())
			result, err := ParseReceivePack(bytes.NewReader(raw), receiveOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func TestParseReceivePackRejectsHaveAsUpdateRef(t *testing.T) {
	raw := joinPackets(packet(sha1A+" "+sha1B+" .have\x00 report-status"), flush())
	result, err := ParseReceivePack(bytes.NewReader(raw), receiveOptions(4096))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseAdvertisementAcceptsHaveOnlyAsAdvertisementRef(t *testing.T) {
	for name, raw := range map[string][]byte{
		"first": joinPackets(
			packet(sha1A+" .have\x00report-status\n"),
			packet(sha1B+" refs/heads/feature\n"),
			flush(),
		),
		"later": joinPackets(
			packet(sha1A+" HEAD\x00report-status\n"),
			packet(sha1B+" .have\n"),
			flush(),
		),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseAdvertisement(fragmented(raw, 1), 4096)
			if err != nil || !bytes.Equal(result.Raw, raw) {
				t.Fatalf("ParseAdvertisement() = (%#v, %v)", result, err)
			}
		})
	}
}

func TestParseAdvertisedCapabilitiesRejectsHaveNearVariants(t *testing.T) {
	for name, raw := range map[string][]byte{
		"first suffix": joinPackets(packet(sha1A+" .have/\x00report-status\n"), flush()),
		"first peeled": joinPackets(packet(sha1A+" .have^{}\x00report-status\n"), flush()),
		"later suffix": joinPackets(packet(sha1A+" HEAD\x00report-status\n"), packet(sha1B+" .havex\n"), flush()),
		"later nested": joinPackets(packet(sha1A+" HEAD\x00report-status\n"), packet(sha1B+" refs/heads/.have\n"), flush()),
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

func capturedV250ShallowPush(ref string) []byte {
	return joinPackets(
		packet("shallow "+sha1A+"\n"),
		packet(sha1A+" "+sha1B+" "+ref+"\x00 report-status"),
		flush(),
	)
}
