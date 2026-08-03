package gitproto

import (
	"fmt"
	"strings"
)

// Capabilities stores unique Git protocol capabilities by name and optional value.
type Capabilities map[string]string

// Has reports whether a named capability was present.
func (capabilities Capabilities) Has(name string) bool {
	_, found := capabilities[name]
	return found
}

// Value returns a capability's parameter, or an empty string when absent or bare.
func (capabilities Capabilities) Value(name string) string { return capabilities[name] }

// ParseCapabilities parses a space-delimited Git protocol capability list.
func ParseCapabilities(value string) (Capabilities, error) {
	capabilities := Capabilities{}
	if value == "" {
		return capabilities, nil
	}
	if strings.HasPrefix(value, " ") {
		value = value[1:]
		if value == "" {
			return nil, fmt.Errorf("invalid empty capability list")
		}
	}
	for _, token := range strings.Split(value, " ") {
		name, parameter, found := strings.Cut(token, "=")
		if name == "" || !validCapabilityPart(name) || (found && (parameter == "" || !validCapabilityPart(parameter))) {
			return nil, fmt.Errorf("invalid capability %q", token)
		}
		if _, duplicate := capabilities[name]; duplicate {
			return nil, fmt.Errorf("duplicate capability %q", name)
		}
		capabilities[name] = parameter
	}
	return capabilities, nil
}

// ValidateRequestedCapabilities accepts only capabilities the server advertised.
func validatePushCertificate(advertised Capabilities) error {
	if !advertised.Has("push-cert") || advertised.Value("push-cert") == "" {
		return fmt.Errorf("push certificate was not advertised with a nonce")
	}
	return nil
}

func ValidateRequestedCapabilities(requested, advertised Capabilities) error {
	for name, parameter := range requested {
		advertisedParameter, found := advertised[name]
		switch name {
		case "agent":
			if !found || advertisedParameter == "" || parameter == "" {
				return fmt.Errorf("agent capability requires an advertised server value and nonempty client value")
			}
			continue
		case "session-id":
			if !found || advertisedParameter == "" || parameter == "" {
				return fmt.Errorf("session-id capability requires an advertised server value and nonempty client value")
			}
			continue
		case "push-cert", "object-format":
			if !found || parameter == "" || parameter != advertisedParameter {
				return fmt.Errorf("requested capability %q does not match advertisement", name)
			}
		default:
			if !found || parameter != advertisedParameter {
				return fmt.Errorf("requested capability %q was not advertised", name)
			}
		}
	}
	return nil
}

func validCapabilityPart(value string) bool {
	for _, character := range value {
		if character <= 0x20 || character == 0x7f || character == 0 {
			return false
		}
	}
	return true
}
