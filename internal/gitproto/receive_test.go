package gitproto

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
)

const (
	sha1A   = "1111111111111111111111111111111111111111"
	sha1B   = "2222222222222222222222222222222222222222"
	zero1   = "0000000000000000000000000000000000000000"
	sha256A = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sha256B = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestParseReceivePackAcceptsCommandsAndPreservesPrefix(t *testing.T) {
	raw := joinPackets(
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status side-band-64k"),
		packet(zero1+" "+sha1B+" refs/tags/v1"),
		flush(),
	)

	result, err := ParseReceivePack(fragmented(raw, 1), receiveOptions(4096))
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("Raw = %q, want %q", result.Prefix, raw)
	}
	if len(result.Updates) != 2 || result.Updates[0].OldOID != sha1A || result.Updates[0].NewOID != sha1B || result.Updates[0].Ref != "refs/heads/feature" || result.Updates[1].Ref != "refs/tags/v1" {
		t.Fatalf("Updates = %#v", result.Updates)
	}
	if !result.Capabilities.Has("report-status") || !result.Capabilities.Has("side-band-64k") {
		t.Fatalf("Capabilities = %#v", result.Capabilities)
	}
}

func TestParseReceivePackRequiresPositiveCommandLimit(t *testing.T) {
	raw := joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush())
	result, err := ParseReceivePack(bytes.NewReader(raw), ReceiveOptions{
		MaxBytes:       4096,
		AdvertisedCaps: standardCapabilities(),
	})
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRejectsCommandsAboveCallerLimitWithoutPartialResult(t *testing.T) {
	raw := joinPackets(
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"),
		packet(zero1+" "+sha1B+" refs/tags/v1"),
		flush(),
	)
	options := receiveOptions(4096)
	options.MaxCommands = 1

	result, err := ParseReceivePack(bytes.NewReader(raw), options)
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackAcceptsExactCommandAndPolicyLimits(t *testing.T) {
	raw := joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush())
	options := receiveOptions(4096)
	options.MaxCommands = 1
	options.Policy.MaxRefUpdates = 1

	result, err := ParseReceivePack(bytes.NewReader(raw), options)
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseReceivePackRejectsPolicyLimitWithoutPartialResult(t *testing.T) {
	raw := joinPackets(
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"),
		packet(zero1+" "+sha1B+" refs/tags/v1"),
		flush(),
	)
	options := receiveOptions(4096)
	options.Policy.MaxRefUpdates = 1

	result, err := ParseReceivePack(bytes.NewReader(raw), options)
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackReturnsCanonicalPolicyFailuresWithoutPartialResult(t *testing.T) {
	oneUpdate := joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush())
	twoUpdates := joinPackets(
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"),
		packet(zero1+" "+sha1B+" refs/tags/v1"),
		flush(),
	)
	deleteUpdate := joinPackets(packet(sha1A+" "+zero1+" refs/heads/feature\x00report-status"), flush())

	tests := []struct {
		name    string
		raw     []byte
		policy  config.PushPolicy
		options func(ReceiveOptions) ReceiveOptions
	}{
		{"zero limit", oneUpdate, config.PushPolicy{}, func(options ReceiveOptions) ReceiveOptions { return options }},
		{"negative limit", oneUpdate, config.PushPolicy{MaxRefUpdates: -1}, func(options ReceiveOptions) ReceiveOptions { return options }},
		{"update limit", twoUpdates, config.PushPolicy{MaxRefUpdates: 1}, func(options ReceiveOptions) ReceiveOptions { return options }},
		{"denied ref", oneUpdate, config.PushPolicy{MaxRefUpdates: 1, DenyRefs: []string{"refs/heads/feature"}}, func(options ReceiveOptions) ReceiveOptions { return options }},
		{"denied delete", deleteUpdate, config.PushPolicy{MaxRefUpdates: 1, DenyDeletes: true}, func(options ReceiveOptions) ReceiveOptions {
			options.AdvertisedCaps["delete-refs"] = ""
			return options
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := test.options(receiveOptions(4096))
			options.Policy = test.policy

			result, err := ParseReceivePack(bytes.NewReader(test.raw), options)
			if !errors.Is(err, policy.ErrRefPolicy) {
				t.Fatalf("ParseReceivePack() error = %v, want policy.ErrRefPolicy", err)
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}

func TestParseReceivePackAcceptsShallowDeclarationsBeforeOrdinaryCommands(t *testing.T) {
	raw := joinPackets(
		packet("shallow "+sha1A+"\n"),
		packet("shallow "+sha1B+"\n"),
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"),
		flush(),
	)

	result, err := ParseReceivePack(fragmented(raw, 1), receiveOptions(4096))
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if len(result.Updates) != 1 || result.Updates[0].Ref != "refs/heads/feature" || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseReceivePackRejectsMainUnderDefaultPolicy(t *testing.T) {
	raw := joinPackets(packet(sha1A+" "+sha1B+" refs/heads/main\x00report-status"), flush())

	result, err := ParseReceivePack(bytes.NewReader(raw), ReceiveOptions{
		MaxBytes:       4096,
		MaxCommands:    16,
		AdvertisedCaps: certificateCapabilities(),
		Policy:         config.PushPolicy{MaxRefUpdates: 16, DenyRefs: []string{"refs/heads/main"}},
	})
	if !errors.Is(err, policy.ErrRefPolicy) {
		t.Fatalf("ParseReceivePack() error = %v, want policy.ErrRefPolicy", err)
	}
	if result.Prefix != nil || result.Updates != nil || result.Capabilities != nil {
		t.Fatalf("result = %#v, want no forwardable result", result)
	}
	updates := RejectedUpdates(err)
	if len(updates) != 1 || updates[0].Ref != "refs/heads/main" {
		t.Fatalf("RejectedUpdates() = %#v", updates)
	}
	updates[0].Ref = "refs/heads/tampered"
	if got := RejectedUpdates(err); len(got) != 1 || got[0].Ref != "refs/heads/main" {
		t.Fatalf("RejectedUpdates() was not defensive: %#v", got)
	}
}

func TestParseReceivePackRejectsMalformedInputsWithoutRawBytes(t *testing.T) {
	cases := map[string][]byte{
		"truncated packet":     []byte("0030" + sha1A),
		"bad length":           []byte("zzzz"),
		"missing capabilities": joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature"), flush()),
		"duplicate ref": joinPackets(
			packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"),
			packet(sha1A+" "+sha1B+" refs/heads/feature"), flush()),
		"invalid utf8 ref": joinPackets(packet(sha1A+" "+sha1B+" refs/heads/\xff\x00report-status"), flush()),
		"mixed oid widths": joinPackets(packet(sha1A+" "+sha256B+" refs/heads/feature\x00report-status"), flush()),
		"oversize":         joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush()),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			limit := 4096
			if name == "oversize" {
				limit = 5
			}
			result, err := ParseReceivePack(bytes.NewReader(raw), receiveOptions(limit))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			if result.Prefix != nil || result.Updates != nil || result.Capabilities != nil {
				t.Fatalf("result = %#v, want no forwardable result", result)
			}
		})
	}
}

func TestParseReceivePackAcceptsShallowPushCertificateAndAdvertisedOptions(t *testing.T) {
	raw := joinPackets(
		packet("shallow "+sha1A+"\n"),
		packet("push-cert\x00 report-status push-options"),
		packet("certificate version 0.1\n"),
		packet("pusher Example <example@example.com> 0 +0000\n"),
		packet("pushee ssh://git@github.com/owner/repo.git\n"),
		packet("nonce nonce-123\n"),
		packet("push-option ci.skip\n"),
		packet("\n"),
		packet(zero1+" "+sha1B+" refs/heads/feature\n"),
		packet("-----BEGIN PGP SIGNATURE-----\n"),
		packet("dGVzdA==\n"),
		packet("-----END PGP SIGNATURE-----\n"),
		packet("push-cert-end\n"),
		flush(),
		packet("ci.skip"),
		flush(),
	)

	result, err := ParseReceivePack(fragmented(raw, 1), ReceiveOptions{
		MaxBytes:       4096,
		MaxCommands:    16,
		Policy:         config.PushPolicy{MaxRefUpdates: 16},
		AdvertisedCaps: certificateCapabilities(),
	})
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if !bytes.Equal(result.Prefix, raw) || len(result.Updates) != 1 || result.Updates[0].Ref != "refs/heads/feature" {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseReceivePackAcceptsSHA256DeletesAndForceShapedUpdates(t *testing.T) {
	raw := joinPackets(
		packet(sha256A+" "+sha256B+" refs/heads/rewritten\x00report-status object-format=sha256"),
		packet(sha256A+" "+strings.Repeat("0", 64)+" refs/heads/obsolete"),
		packet(strings.Repeat("0", 64)+" "+sha256B+" refs/tags/v2"),
		flush(),
	)

	options := receiveOptions(4096)
	options.AdvertisedCaps["object-format"] = "sha256"
	options.AdvertisedCaps["delete-refs"] = ""
	result, err := ParseReceivePack(bytes.NewReader(raw), options)
	if err != nil {
		t.Fatalf("ParseReceivePack() error = %v", err)
	}
	if len(result.Updates) != 3 || !bytes.Equal(result.Prefix, raw) {
		t.Fatalf("result = %#v", result)
	}
}

func TestParseReceivePackRejectsDuplicateShallowAndInvalidRef(t *testing.T) {
	for name, raw := range map[string][]byte{
		"duplicate shallow": joinPackets(
			packet("shallow "+sha1A+"\n"), packet("shallow "+sha1A+"\n"),
			packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush()),
		"unqualified ref": joinPackets(packet(sha1A+" "+sha1B+" main\x00report-status"), flush()),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := ParseReceivePack(bytes.NewReader(raw), receiveOptions(4096))
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			if result.Prefix != nil || result.Updates != nil || result.Capabilities != nil {
				t.Fatalf("result = %#v, want no forwardable result", result)
			}
		})
	}
}

func TestParseReceivePackAppliesPolicyToSignedCertificateCommands(t *testing.T) {
	raw := signedCertificate(zero1 + " " + sha1B + " refs/heads/main\n")
	result, err := ParseReceivePack(bytes.NewReader(raw), ReceiveOptions{
		MaxBytes:       4096,
		MaxCommands:    16,
		AdvertisedCaps: certificateCapabilities(),
		Policy:         config.PushPolicy{MaxRefUpdates: 16, DenyRefs: []string{"refs/heads/main"}},
	})
	if !errors.Is(err, policy.ErrRefPolicy) {
		t.Fatalf("ParseReceivePack() error = %v, want policy.ErrRefPolicy", err)
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRejectsMalformedCertificateSignatureWithoutRawBytes(t *testing.T) {
	raw := joinPackets(
		packet("push-cert\x00 report-status"),
		packet("certificate version 0.1\n"),
		packet("pusher Example <example@example.com> 0 +0000\n"),
		packet("pushee ssh://git@github.com/owner/repo.git\n"),
		packet("nonce nonce-123\n"),
		packet("\n"),
		packet(zero1+" "+sha1B+" refs/heads/feature\n"),
		packet("not a detached signature\n"),
		packet("push-cert-end\n"),
	)

	result, err := ParseReceivePack(bytes.NewReader(raw), receiveCertificateOptions(4096))
	if err == nil {
		t.Fatal("ParseReceivePack() error = nil")
	}
	assertNoForwardableReceiveResult(t, result)
}

func TestParseReceivePackRejectsUnadvertisedPushOptionsAndBadCertificate(t *testing.T) {
	withOption := joinPackets(
		packet(sha1A+" "+sha1B+" refs/heads/feature\x00push-options"), flush(),
		packet("ci.skip"), flush(),
	)
	badCertificate := joinPackets(
		packet("push-cert\x00 report-status"),
		packet("certificate version 0.1\n"),
		packet("\n"),
		packet(sha1A+" "+sha1B+" refs/heads/feature\n"),
		flush(),
	)
	for name, raw := range map[string][]byte{"unadvertised option": withOption, "bad certificate": badCertificate} {
		t.Run(name, func(t *testing.T) {
			options := receiveOptions(4096)
			if name == "unadvertised option" {
				options.AdvertisedCaps = Capabilities{"report-status": ""}
			}
			if name == "bad certificate" {
				options = receiveCertificateOptions(4096)
			}
			result, err := ParseReceivePack(bytes.NewReader(raw), options)
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil")
			}
			assertNoForwardableReceiveResult(t, result)
		})
	}
}
