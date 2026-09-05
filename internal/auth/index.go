package auth

import (
	"crypto/subtle"
	"fmt"
	"sort"
)

// Index holds startup-loaded token digests and their principals.
type Index struct {
	entries []entry
}

type entry struct {
	digest    [32]byte
	principal string
}

// NewIndex creates an authentication index from principal token values.
func NewIndex(principals map[string][]string) (*Index, error) {
	ids := make([]string, 0, len(principals))
	for id := range principals {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	index := &Index{}
	seen := make(map[[32]byte]string)
	for _, id := range ids {
		for _, value := range principals[id] {
			digest, ok := tokenDigest(value)
			if !ok {
				return nil, fmt.Errorf("principal %q has an invalid token", id)
			}
			if previous, duplicate := seen[digest]; duplicate {
				return nil, fmt.Errorf("principal %q duplicates token for principal %q", id, previous)
			}
			seen[digest] = id
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
