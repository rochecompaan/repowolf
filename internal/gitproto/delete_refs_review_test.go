package gitproto

import (
	"bytes"
	"testing"
)

func TestParseReceivePackRequiresAdvertisedDeleteRefsForDeletes(t *testing.T) {
	for name, testCase := range map[string]struct {
		raw        []byte
		advertised Capabilities
	}{
		"ordinary": {
			raw:        joinPackets(packet(sha1A+" "+zero1+" refs/heads/feature\x00 report-status"), flush()),
			advertised: receiveOptions(4096).AdvertisedCaps,
		},
		"signed": {
			raw:        signedEnvelope("-----BEGIN PGP SIGNATURE-----", "-----END PGP SIGNATURE-----", nil, sha1A+" "+zero1+" refs/heads/feature\n"),
			advertised: certificateCapabilities(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			unadvertised := ReceiveOptions{MaxBytes: 4096, MaxCommands: 16, AdvertisedCaps: testCase.advertised}
			result, err := ParseReceivePack(bytes.NewReader(testCase.raw), unadvertised)
			if err == nil {
				t.Fatal("ParseReceivePack() error = nil without delete-refs")
			}
			assertNoForwardableReceiveResult(t, result)

			advertised := ReceiveOptions{MaxBytes: 4096, MaxCommands: 16, AdvertisedCaps: make(Capabilities, len(testCase.advertised)+1)}
			for capability, value := range testCase.advertised {
				advertised.AdvertisedCaps[capability] = value
			}
			advertised.AdvertisedCaps["delete-refs"] = ""
			result, err = ParseReceivePack(bytes.NewReader(testCase.raw), advertised)
			if err != nil || !bytes.Equal(result.Prefix, testCase.raw) || len(result.Updates) != 1 {
				t.Fatalf("ParseReceivePack() with delete-refs = (%#v, %v)", result, err)
			}
		})
	}
}
