package gitproto

import (
	"bytes"
	"fmt"
	"strings"
)

func (parser *receiveParser) parseCertificate(first []byte) error {
	prefix := []byte("push-cert\x00")
	capabilities, err := ParseCapabilities(string(first[len(prefix):]))
	if err != nil {
		return fmt.Errorf("invalid push certificate preamble")
	}
	if err := ValidateRequestedCapabilities(capabilities, parser.options.AdvertisedCaps); err != nil {
		return err
	}
	if err := parser.negotiateObjectIDWidth(capabilities); err != nil {
		return err
	}
	parser.capabilities = capabilities
	if err := validatePushCertificate(parser.options.AdvertisedCaps); err != nil {
		return err
	}
	if err := parser.expectPacket("certificate version 0.1\n"); err != nil {
		return err
	}
	headerOptions, err := parser.parseCertificateHeaders(capabilities)
	if err != nil {
		return err
	}
	if err := parser.parseCertificateBody(); err != nil {
		return err
	}
	if err := parser.expectFlush("push certificate terminator"); err != nil {
		return err
	}
	return parser.parsePushOptions(capabilities, headerOptions, true)
}

func (parser *receiveParser) parseCertificateHeaders(requested Capabilities) ([]string, error) {
	required := []string{"pusher", "pushee", "nonce"}
	nextRequired := 0
	headerOptions := []string{}
	for {
		payload, flush, err := parser.reader.read()
		if err != nil {
			return nil, err
		}
		if flush {
			return nil, fmt.Errorf("invalid push certificate headers")
		}
		if bytes.Equal(payload, []byte{'\n'}) {
			if nextRequired != len(required) {
				return nil, fmt.Errorf("push certificate lacks required headers")
			}
			return headerOptions, nil
		}
		field, value, err := parseCertificateHeader(payload)
		if err != nil {
			return nil, err
		}
		if nextRequired < len(required) {
			if field != required[nextRequired] || !validRequiredCertificateHeader(field, value) {
				return nil, fmt.Errorf("invalid push certificate header order or value")
			}
			if field == "nonce" && value != parser.options.AdvertisedCaps.Value("push-cert") {
				return nil, fmt.Errorf("push certificate nonce does not match advertisement")
			}
			nextRequired++
			continue
		}
		if field != "push-option" || !requested.Has("push-options") || !parser.options.AdvertisedCaps.Has("push-options") || !validPushOption(value) {
			return nil, fmt.Errorf("invalid push certificate push-option")
		}
		headerOptions = append(headerOptions, value)
	}
}

func parseCertificateHeader(payload []byte) (string, string, error) {
	if !hasSingleTerminalLF(payload) || bytes.ContainsAny(payload, "\x00\r") {
		return "", "", fmt.Errorf("invalid push certificate header")
	}
	line := string(payload[:len(payload)-1])
	field, value, found := strings.Cut(line, " ")
	if !found || field == "" || value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", "", fmt.Errorf("invalid push certificate header")
	}
	return field, value, nil
}

func (parser *receiveParser) parsePushOptions(capabilities Capabilities, headerOptions []string, signed bool) error {
	if !capabilities.Has("push-options") {
		if signed && len(headerOptions) != 0 {
			return fmt.Errorf("signed push option header lacks client selection")
		}
		return nil
	}
	if !parser.options.AdvertisedCaps.Has("push-options") {
		return fmt.Errorf("client requested unadvertised push-options capability")
	}
	trailingOptions := []string{}
	for {
		payload, flush, err := parser.reader.read()
		if err != nil {
			return err
		}
		if flush {
			if !signed {
				return nil
			}
			if len(headerOptions) != len(trailingOptions) {
				return fmt.Errorf("signed push option count mismatch")
			}
			for index, option := range headerOptions {
				if trailingOptions[index] != option {
					return fmt.Errorf("signed push option mismatch at index %d", index)
				}
			}
			return nil
		}
		option := string(payload)
		if !validPushOption(option) {
			return fmt.Errorf("invalid push option")
		}
		trailingOptions = append(trailingOptions, option)
	}
}

func (parser *receiveParser) expectFlush(context string) error {
	_, flush, err := parser.reader.read()
	if err != nil {
		return err
	}
	if !flush {
		return fmt.Errorf("expected %s flush", context)
	}
	return nil
}

func (parser *receiveParser) expectPacket(want string) error {
	payload, flush, err := parser.reader.read()
	if err != nil {
		return err
	}
	if flush || string(payload) != want {
		return fmt.Errorf("expected push certificate %q", strings.TrimSpace(want))
	}
	return nil
}

func signatureEnvelopeEnd(begin string) (string, bool) {
	switch begin {
	case "-----BEGIN PGP SIGNATURE-----":
		return "-----END PGP SIGNATURE-----", true
	case "-----BEGIN PGP MESSAGE-----":
		return "-----END PGP MESSAGE-----", true
	case "-----BEGIN SSH SIGNATURE-----":
		return "-----END SSH SIGNATURE-----", true
	case "-----BEGIN SIGNED MESSAGE-----":
		return "-----END SIGNED MESSAGE-----", true
	default:
		return "", false
	}
}

func validPushOption(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func isCommandLine(line string) bool {
	fields := strings.Fields(line)
	return len(fields) == 3 && validObjectID(fields[0]) && validObjectID(fields[1])
}
