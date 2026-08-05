package gitproto

import (
	"fmt"
	"strings"
)

func (parser *receiveParser) parseCertificateBody() error {
	commandsSeen := false
	signatureEnd := ""
	signatureEnded := false
	signatureContent := false
	for {
		payload, flush, err := parser.reader.read()
		if err != nil {
			return err
		}
		if flush || !validCertificateBodyPacket(payload) {
			return fmt.Errorf("invalid push certificate body")
		}
		line := strings.TrimSuffix(string(payload), "\n")
		if line == "push-cert-end" {
			if !commandsSeen || signatureEnd == "" || !signatureEnded || !signatureContent {
				return fmt.Errorf("incomplete push certificate")
			}
			return nil
		}
		if signatureEnd == "" {
			if isCommandLine(line) {
				if err := parser.addCommand(line); err != nil {
					return err
				}
				commandsSeen = true
				continue
			}
			var found bool
			signatureEnd, found = signatureEnvelopeEnd(line)
			if !found {
				return fmt.Errorf("invalid push certificate signature")
			}
			continue
		}
		if line == signatureEnd {
			if signatureEnded {
				return fmt.Errorf("invalid push certificate signature")
			}
			signatureEnded = true
			continue
		}
		if signatureEnded {
			return fmt.Errorf("invalid push certificate signature")
		}
		if line != "" {
			signatureContent = true
		}
	}
}
