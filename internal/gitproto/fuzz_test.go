package gitproto

import (
	"bytes"
	"testing"
)

const fuzzInputLimit = 1 << 20

func FuzzParseReceivePack(f *testing.F) {
	for _, seed := range receiveSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > fuzzInputLimit {
			t.Skip()
		}
		_, _ = ParseReceivePack(bytes.NewReader(raw), ReceiveOptions{
			MaxBytes:    fuzzInputLimit,
			MaxCommands: 16,
			AdvertisedCaps: Capabilities{
				"delete-refs":   "",
				"object-format": "sha256",
				"push-cert":     "nonce-123",
				"push-options":  "",
				"report-status": "",
				"side-band-64k": "",
			},
		})
	})
}

func FuzzPacketLine(f *testing.F) {
	for _, seed := range packetLineSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > fuzzInputLimit {
			t.Skip()
		}
		reader, err := newPacketReader(bytes.NewReader(raw), fuzzInputLimit)
		if err == nil {
			_, _, _ = reader.read()
		}
	})
}

func FuzzParseAdvertisement(f *testing.F) {
	for _, seed := range advertisementSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > fuzzInputLimit {
			t.Skip()
		}
		_, _ = ParseAdvertisement(bytes.NewReader(raw), fuzzInputLimit)
	})
}

func FuzzParseCapabilities(f *testing.F) {
	for _, seed := range capabilitySeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > fuzzInputLimit {
			t.Skip()
		}
		_, _ = ParseCapabilities(string(raw))
	})
}

func receiveSeeds() [][]byte {
	return [][]byte{
		joinPackets(packet(sha1A+" "+sha1B+" refs/heads/feature\x00report-status"), flush()),
		joinPackets(packet(sha256A+" "+sha256B+" refs/heads/feature\x00report-status object-format=sha256"), flush()),
		capturedV250ShallowPush("refs/heads/feature"),
		capturedV250SignedPush(nil),
		signedPushWithOptions([]string{"ci.skip"}, []string{"ci.skip"}),
		[]byte("zzzz"),
		[]byte("0030" + sha1A),
	}
}

func packetLineSeeds() [][]byte {
	return [][]byte{
		flush(),
		packet("payload"),
		[]byte("fff1"),
		[]byte("zzzz"),
		[]byte("0003"),
		[]byte("0008abc"),
	}
}

func advertisementSeeds() [][]byte {
	return [][]byte{
		joinPackets(packet(sha1A+" HEAD\x00report-status\n"), flush()),
		joinPackets(packet(sha256A+" HEAD\x00report-status object-format=sha256\n"), flush()),
		joinPackets(packet(sha1A+" HEAD\x00report-status\n"), packet("shallow "+sha1B+"\n"), flush()),
		flush(),
		joinPackets(packet(sha1A+" HEAD"), flush()),
		[]byte("zzzz"),
	}
}

func capabilitySeeds() [][]byte {
	return [][]byte{
		{},
		[]byte("report-status side-band-64k"),
		[]byte(" object-format=sha256 push-cert=nonce-123 push-options"),
		[]byte("agent=git/2.50 session-id=client-123"),
		[]byte("report-status  push-options"),
		[]byte("agent="),
		[]byte("bad\x00capability"),
	}
}
