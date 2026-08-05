package gitproto

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

func validRequiredCertificateHeader(field, value string) bool {
	switch field {
	case "pusher":
		return validPusherIdent(value)
	case "pushee", "nonce":
		return validCertificateText(value)
	default:
		return false
	}
}

func validPusherIdent(value string) bool {
	if !validCertificateText(value) {
		return false
	}
	timezoneAt := strings.LastIndex(value, " ")
	if timezoneAt <= 0 || !validTimezone(value[timezoneAt+1:]) {
		return false
	}
	timestampAt := strings.LastIndex(value[:timezoneAt], " ")
	if timestampAt <= 0 || !validDecimal(value[timestampAt+1:timezoneAt]) {
		return false
	}
	ident := value[:timestampAt]
	nameAt := strings.LastIndex(ident, " <")
	if nameAt <= 0 || !strings.HasSuffix(ident, ">") {
		return false
	}
	name, email := ident[:nameAt], ident[nameAt+2:len(ident)-1]
	return validCertificateText(name) && validCertificateText(email) && !strings.ContainsAny(email, " <>")
}

func validTimezone(value string) bool {
	return len(value) == 5 && (value[0] == '+' || value[0] == '-') && validDecimal(value[1:])
}

func validDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validCertificateText(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character <= 0x1f || character == 0x7f {
			return false
		}
	}
	return true
}

func validCertificateBodyPacket(payload []byte) bool {
	return hasSingleTerminalLF(payload) && !bytes.ContainsAny(payload, "\x00\r")
}
