package policy

import (
	"fmt"
	"strings"

	"github.com/rochecompaan/repowolf/internal/config"
)

// Update is a requested receive-pack ref update.
type Update struct {
	OldOID string
	NewOID string
	Ref    string
}

// ValidatePush rejects malformed, denied, or excessive ref updates. It does
// not inspect commit ancestry; the provider remains authoritative for that.
func ValidatePush(policy config.PushPolicy, updates []Update) error {
	if len(updates) > policy.MaxRefUpdates {
		return fmt.Errorf("%w: %d updates exceeds maximum %d", ErrRefPolicy, len(updates), policy.MaxRefUpdates)
	}
	denied := make(map[string]struct{}, len(policy.DenyRefs))
	for _, ref := range policy.DenyRefs {
		denied[ref] = struct{}{}
	}
	for _, update := range updates {
		if !validRef(update.Ref) {
			return fmt.Errorf("%w: invalid ref", ErrRefPolicy)
		}
		if !validOID(update.OldOID) || !validOID(update.NewOID) || len(update.OldOID) != len(update.NewOID) {
			return fmt.Errorf("%w: invalid object ID", ErrRefPolicy)
		}
		if _, blocked := denied[update.Ref]; blocked {
			return fmt.Errorf("%w: denied ref", ErrRefPolicy)
		}
		if policy.DenyDeletes && zeroOID(update.NewOID) {
			return fmt.Errorf("%w: denied deletion", ErrRefPolicy)
		}
	}
	return nil
}

func validOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	for _, character := range oid {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func zeroOID(oid string) bool {
	return strings.Trim(oid, "0") == ""
}

func validRef(ref string) bool {
	for _, character := range ref {
		if character <= 0x1f || character == 0x7f {
			return false
		}
	}
	if !strings.HasPrefix(ref, "refs/") || len(ref) == len("refs/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, "//") || strings.ContainsAny(ref, " ~^:?*[\\") {
		return false
	}
	for _, component := range strings.Split(ref, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}
