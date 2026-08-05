package gitproto

import "testing"

func TestValidateRequestedCapabilitiesRequiresAdvertisedCapabilities(t *testing.T) {
	advertised, err := ParseCapabilities("report-status push-cert=nonce-123 object-format=sha256 agent=git/2.45")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	requested, err := ParseCapabilities("report-status push-cert=nonce-123 object-format=sha256 agent=git/2.46")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	if err := ValidateRequestedCapabilities(requested, advertised); err != nil {
		t.Fatalf("ValidateRequestedCapabilities() error = %v", err)
	}
}

func TestValidateRequestedCapabilitiesRequiresAdvertisedParameterizedAgent(t *testing.T) {
	requested, err := ParseCapabilities("agent=git/2.46")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	for name, advertised := range map[string]Capabilities{
		"unadvertised": {},
		"bare":         {"agent": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateRequestedCapabilities(requested, advertised); err == nil {
				t.Fatal("ValidateRequestedCapabilities() error = nil")
			}
		})
	}
	if err := ValidateRequestedCapabilities(requested, Capabilities{"agent": "git/2.45"}); err != nil {
		t.Fatalf("ValidateRequestedCapabilities() error = %v", err)
	}
}

func TestParseCapabilitiesRejectsEmptyAgentValue(t *testing.T) {
	if _, err := ParseCapabilities("agent="); err == nil {
		t.Fatal("ParseCapabilities() error = nil")
	}
}

func TestValidateRequestedCapabilitiesAcceptsDifferingAdvertisedSessionIDs(t *testing.T) {
	advertised, err := ParseCapabilities("session-id=server-3f6e4d")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	requested, err := ParseCapabilities("session-id=client-5a5d9c")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	if err := ValidateRequestedCapabilities(requested, advertised); err != nil {
		t.Fatalf("ValidateRequestedCapabilities() error = %v", err)
	}
}

func TestValidateRequestedCapabilitiesRejectsEmptyOrUnadvertisedSessionID(t *testing.T) {
	for name, requestedText := range map[string]string{
		"empty":        "session-id=",
		"unadvertised": "session-id=client-5a5d9c",
	} {
		t.Run(name, func(t *testing.T) {
			requested, err := ParseCapabilities(requestedText)
			if name == "empty" {
				if err == nil {
					t.Fatal("ParseCapabilities() error = nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCapabilities() error = %v", err)
			}
			if err := ValidateRequestedCapabilities(requested, Capabilities{}); err == nil {
				t.Fatal("ValidateRequestedCapabilities() error = nil")
			}
		})
	}
}

func TestValidateRequestedCapabilitiesRejectsBareAdvertisedSessionID(t *testing.T) {
	requested, err := ParseCapabilities("session-id=client-5a5d9c")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	if err := ValidateRequestedCapabilities(requested, Capabilities{"session-id": ""}); err == nil {
		t.Fatal("ValidateRequestedCapabilities() error = nil")
	}
}

func TestValidateRequestedCapabilitiesRejectsUnknownAndWrongParameters(t *testing.T) {
	advertised, err := ParseCapabilities("report-status push-cert=nonce-123 object-format=sha256")
	if err != nil {
		t.Fatalf("ParseCapabilities() error = %v", err)
	}
	for name, requestedText := range map[string]string{
		"unknown":                "unadvertised",
		"wrong push certificate": "push-cert=wrong-nonce",
		"wrong object format":    "object-format=sha1",
	} {
		t.Run(name, func(t *testing.T) {
			requested, err := ParseCapabilities(requestedText)
			if err != nil {
				t.Fatalf("ParseCapabilities() error = %v", err)
			}
			if err := ValidateRequestedCapabilities(requested, advertised); err == nil {
				t.Fatal("ValidateRequestedCapabilities() error = nil")
			}
		})
	}
}
