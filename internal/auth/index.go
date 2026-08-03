package auth

import (
	"crypto/subtle"
	"fmt"
	"sort"

	"github.com/rochecompaan/repowolf/internal/config"
)

// LookupEnv returns the value of a named environment variable.
type LookupEnv func(string) (string, bool)

// Index holds startup-loaded token digests and their principals.
type Index struct {
	entries []entry
}

type entry struct {
	digest    [32]byte
	principal string
}

// Load reads configured token environment variables into an authentication index.
func Load(principals map[string]config.Principal, lookup LookupEnv) (*Index, error) {
	ids := make([]string, 0, len(principals))
	for id := range principals {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	index := &Index{}
	seen := make(map[[32]byte]string)
	for _, id := range ids {
		for _, name := range principals[id].TokenEnvs {
			value, found := "", false
			if lookup != nil {
				value, found = lookup(name)
			}
			if !found {
				return nil, fmt.Errorf("token environment %q is missing", name)
			}
			if value == "" {
				return nil, fmt.Errorf("token environment %q is empty", name)
			}

			digest, ok := tokenDigest(value)
			if !ok {
				return nil, fmt.Errorf("token environment %q has an invalid token", name)
			}
			if previous, duplicate := seen[digest]; duplicate {
				return nil, fmt.Errorf("token environment %q duplicates token environment %q", name, previous)
			}
			seen[digest] = name
			index.entries = append(index.entries, entry{digest: digest, principal: id})
		}
	}
	return index, nil
}

// Authenticate returns the principal associated with token, when configured.
func (index *Index) Authenticate(token string) (string, bool) {
	digest, ok := tokenDigest(token)
	if !ok || index == nil {
		return "", false
	}

	matches := 0
	principal := ""
	for _, entry := range index.entries {
		match := subtle.ConstantTimeCompare(digest[:], entry.digest[:])
		matches += match
		if match == 1 {
			principal = entry.principal
		}
	}
	return principal, matches == 1
}
