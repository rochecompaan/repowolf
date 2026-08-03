package gitproto

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/policy"
)

func (parser *receiveParser) addShallow(line string) error {
	if !strings.HasPrefix(line, "shallow ") || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 || strings.ContainsAny(line, "\r\x00") {
		return fmt.Errorf("invalid shallow declaration")
	}
	objectID := strings.TrimSuffix(strings.TrimPrefix(line, "shallow "), "\n")
	if !validObjectID(objectID) {
		return fmt.Errorf("invalid shallow object ID")
	}
	key := strings.ToLower(objectID)
	if _, exists := parser.seenShallows[key]; exists {
		return fmt.Errorf("duplicate shallow object ID")
	}
	parser.seenShallows[key] = struct{}{}
	parser.shallowIDs = append(parser.shallowIDs, objectID)
	return nil
}

func (parser *receiveParser) negotiateObjectIDWidth(requested Capabilities) error {
	width := 40
	if requested.Has("object-format") {
		switch requested.Value("object-format") {
		case "sha1":
			if parser.options.AdvertisedCaps.Value("object-format") != "sha1" {
				return fmt.Errorf("unsupported requested object format %q", requested.Value("object-format"))
			}
		case "sha256":
			if parser.options.AdvertisedCaps.Value("object-format") != "sha256" {
				return fmt.Errorf("unsupported requested object format %q", requested.Value("object-format"))
			}
			width = 64
		default:
			return fmt.Errorf("unsupported requested object format %q", requested.Value("object-format"))
		}
	}
	for _, objectID := range parser.shallowIDs {
		if len(objectID) != width {
			return fmt.Errorf("shallow object ID does not match negotiated object format")
		}
	}
	parser.oidWidth = width
	return nil
}

func (parser *receiveParser) addCommand(line string) error {
	if parser.options.MaxCommands > 0 && len(parser.updates) >= parser.options.MaxCommands {
		return fmt.Errorf("receive-pack command count exceeds %d", parser.options.MaxCommands)
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || strings.Join(fields, " ") != line {
		return fmt.Errorf("invalid ref update command")
	}
	oldID, newID, ref := fields[0], fields[1], fields[2]
	if !validObjectID(oldID) || !validObjectID(newID) || !parser.setOIDWidth(oldID) || !parser.setOIDWidth(newID) {
		return fmt.Errorf("invalid or mixed object IDs")
	}
	if isZeroObjectID(newID) && !parser.options.AdvertisedCaps.Has("delete-refs") {
		return fmt.Errorf("delete command requires advertised delete-refs capability")
	}
	if !validRef(ref) {
		return fmt.Errorf("invalid ref %q", ref)
	}
	if _, exists := parser.seenRefs[ref]; exists {
		return fmt.Errorf("duplicate ref %q", ref)
	}
	parser.seenRefs[ref] = struct{}{}
	parser.updates = append(parser.updates, policy.Update{OldOID: oldID, NewOID: newID, Ref: ref})
	return nil
}

func validatePushPolicy(pushPolicy config.PushPolicy, updates []policy.Update) error {
	if pushPolicy.MaxRefUpdates > 0 && len(updates) > pushPolicy.MaxRefUpdates {
		return fmt.Errorf("push updates %d refs, maximum is %d", len(updates), pushPolicy.MaxRefUpdates)
	}
	denied := make(map[string]struct{}, len(pushPolicy.DenyRefs))
	for _, ref := range pushPolicy.DenyRefs {
		denied[ref] = struct{}{}
	}
	for _, update := range updates {
		if _, blocked := denied[update.Ref]; blocked {
			return fmt.Errorf("push update for denied ref %q", update.Ref)
		}
		if pushPolicy.DenyDeletes && isZeroObjectID(update.NewOID) {
			return fmt.Errorf("push deletion for ref %q is denied", update.Ref)
		}
	}
	return nil
}

func (parser *receiveParser) setOIDWidth(objectID string) bool {
	if parser.oidWidth == 0 {
		parser.oidWidth = len(objectID)
		return true
	}
	return parser.oidWidth == len(objectID)
}

func validObjectID(objectID string) bool {
	if len(objectID) != 40 && len(objectID) != 64 {
		return false
	}
	for _, character := range objectID {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return false
		}
	}
	return true
}

func isZeroObjectID(objectID string) bool {
	return objectID != "" && strings.Trim(objectID, "0") == ""
}

func validRef(ref string) bool {
	if !utf8.ValidString(ref) || !strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "..") || strings.Contains(ref, "//") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".") {
		return false
	}
	for _, character := range ref {
		if character <= 0x20 || character == 0x7f || strings.ContainsRune("~^:?*[\\", character) {
			return false
		}
	}
	if ref == "@" {
		return false
	}
	for _, part := range strings.Split(ref, "/") {
		if part == "" || part[0] == '.' || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
