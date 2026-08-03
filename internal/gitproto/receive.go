// Package gitproto parses the policy-relevant receive-pack request prefix.
package gitproto

import (
	"bytes"
	"fmt"
	"io"

	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
)

// ReceiveOptions controls bounded receive-pack parsing.
type ReceiveOptions struct {
	MaxBytes       int
	MaxCommands    int
	Policy         config.PushPolicy
	AdvertisedCaps Capabilities
}

// ReceiveResult is safe to forward only after policy validation succeeds.
type ReceiveResult struct {
	Prefix       []byte
	Updates      []policy.Update
	Capabilities Capabilities
}

// ParseReceivePack reads exactly the pre-pack receive-pack request prefix.
func ParseReceivePack(input io.Reader, options ReceiveOptions) (ReceiveResult, error) {
	if options.MaxCommands < 1 {
		return ReceiveResult{}, fmt.Errorf("receive-pack command limit must be positive")
	}
	reader, err := newPacketReader(input, options.MaxBytes)
	if err != nil {
		return ReceiveResult{}, err
	}
	parser := receiveParser{
		reader:       reader,
		options:      options,
		seenRefs:     map[string]struct{}{},
		seenShallows: map[string]struct{}{},
	}
	if err := parser.parse(); err != nil {
		return ReceiveResult{}, err
	}
	if err := policy.ValidatePush(options.Policy, parser.updates); err != nil {
		return ReceiveResult{}, err
	}
	return ReceiveResult{
		Prefix:       reader.bytes(),
		Updates:      append([]policy.Update(nil), parser.updates...),
		Capabilities: parser.capabilities,
	}, nil
}

type receiveParser struct {
	reader       *packetReader
	options      ReceiveOptions
	updates      []policy.Update
	capabilities Capabilities
	seenRefs     map[string]struct{}
	seenShallows map[string]struct{}
	shallowIDs   []string
	oidWidth     int
}

func (parser *receiveParser) parse() error {
	for {
		payload, flush, err := parser.reader.read()
		if err != nil {
			return err
		}
		if flush {
			return fmt.Errorf("receive-pack request has no command list")
		}
		if !bytes.HasPrefix(payload, []byte("shallow ")) {
			return parser.parseRequest(payload)
		}
		if err := parser.addShallow(string(payload)); err != nil {
			return err
		}
	}
}

func (parser *receiveParser) parseRequest(first []byte) error {
	if bytes.HasPrefix(first, []byte("push-cert\x00")) {
		return parser.parseCertificate(first)
	}
	command, capabilities, err := parser.parseFirstCommand(first)
	if err != nil {
		return err
	}
	if err := ValidateRequestedCapabilities(capabilities, parser.options.AdvertisedCaps); err != nil {
		return err
	}
	if err := parser.negotiateObjectIDWidth(capabilities); err != nil {
		return err
	}
	parser.capabilities = capabilities
	if err := parser.addCommand(command); err != nil {
		return err
	}
	for {
		payload, flush, err := parser.reader.read()
		if err != nil {
			return err
		}
		if flush {
			return parser.parsePushOptions(capabilities, nil, false)
		}
		if err := parser.addCommand(string(payload)); err != nil {
			return err
		}
	}
}

func (parser *receiveParser) parseFirstCommand(payload []byte) (string, Capabilities, error) {
	command, caps, found := bytes.Cut(payload, []byte{0})
	if !found {
		return "", nil, fmt.Errorf("first command lacks capability separator")
	}
	if bytes.Contains(command, []byte{'\n'}) {
		return "", nil, fmt.Errorf("ordinary command must not contain newline")
	}
	capabilities, err := ParseCapabilities(string(caps))
	if err != nil {
		return "", nil, err
	}
	return string(command), capabilities, nil
}
