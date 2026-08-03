package gitproto

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// Advertisement is the raw server advertisement and first-reference capabilities.
type Advertisement struct {
	Raw          []byte
	Capabilities Capabilities
}

// ParseAdvertisement reads one v0 receive-pack advertisement exactly.
func ParseAdvertisement(input io.Reader, maxBytes int) (Advertisement, error) {
	reader, err := newPacketReader(input, maxBytes)
	if err != nil {
		return Advertisement{}, err
	}
	first, flush, err := reader.read()
	if err != nil {
		return Advertisement{}, err
	}
	if flush {
		return Advertisement{}, fmt.Errorf("empty receive-pack advertisement")
	}
	line, rawCapabilities, found := bytes.Cut(first, []byte{0})
	if !found || bytes.Contains(rawCapabilities, []byte{0}) {
		return Advertisement{}, fmt.Errorf("advertisement lacks one capability separator")
	}
	if bytes.Contains(line, []byte{'\n'}) || !hasSingleTerminalLF(rawCapabilities) {
		return Advertisement{}, fmt.Errorf("advertisement first reference lacks one terminal LF")
	}
	objectID, ref, found := strings.Cut(string(line), " ")
	if !found || strings.Contains(ref, " ") || !validObjectID(objectID) || !validAdvertisedRef(ref) {
		return Advertisement{}, fmt.Errorf("invalid advertisement first reference")
	}
	capabilities, err := ParseCapabilities(string(rawCapabilities[:len(rawCapabilities)-1]))
	if err != nil {
		return Advertisement{}, err
	}
	width, err := advertisedObjectIDWidth(capabilities)
	if err != nil {
		return Advertisement{}, err
	}
	if len(objectID) != width {
		return Advertisement{}, fmt.Errorf("advertisement first reference has wrong object ID width")
	}
	seenShallows := map[string]struct{}{}
	for {
		payload, flush, err := reader.read()
		if err != nil {
			return Advertisement{}, err
		}
		if flush {
			return Advertisement{Raw: reader.bytes(), Capabilities: capabilities}, nil
		}
		if bytes.HasPrefix(payload, []byte("shallow ")) {
			if err := addAdvertisedShallow(payload, width, seenShallows); err != nil {
				return Advertisement{}, err
			}
			continue
		}
		if !hasSingleTerminalLF(payload) || !validAdvertisementLine(string(payload[:len(payload)-1]), width) {
			return Advertisement{}, fmt.Errorf("invalid advertisement reference")
		}
	}
}

func advertisedObjectIDWidth(capabilities Capabilities) (int, error) {
	switch capabilities.Value("object-format") {
	case "", "sha1":
		return 40, nil
	case "sha256":
		return 64, nil
	default:
		return 0, fmt.Errorf("unsupported advertised object format %q", capabilities.Value("object-format"))
	}
}

func addAdvertisedShallow(payload []byte, width int, seen map[string]struct{}) error {
	if !hasSingleTerminalLF(payload) || bytes.ContainsAny(payload, "\x00\r") {
		return fmt.Errorf("invalid advertised shallow declaration")
	}
	objectID := string(payload[len("shallow ") : len(payload)-1])
	if len(objectID) != width || !validObjectID(objectID) {
		return fmt.Errorf("invalid advertised shallow object ID")
	}
	key := strings.ToLower(objectID)
	if _, exists := seen[key]; exists {
		return fmt.Errorf("duplicate advertised shallow object ID")
	}
	seen[key] = struct{}{}
	return nil
}

func hasSingleTerminalLF(value []byte) bool {
	return len(value) > 0 && value[len(value)-1] == '\n' && bytes.Count(value, []byte{'\n'}) == 1
}

func validAdvertisementLine(line string, width int) bool {
	objectID, ref, found := strings.Cut(line, " ")
	return found && len(objectID) == width && !strings.Contains(ref, " ") && validObjectID(objectID) && validAdvertisedRef(ref)
}

func validAdvertisedRef(ref string) bool {
	if ref == "HEAD" || ref == ".have" || ref == "capabilities^{}" {
		return true
	}
	if strings.HasSuffix(ref, "^{}") {
		return validRef(strings.TrimSuffix(ref, "^{}"))
	}
	return validRef(ref)
}
