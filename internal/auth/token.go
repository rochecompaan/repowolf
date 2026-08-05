// Package auth generates and authenticates service bearer tokens.
package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const (
	tokenPrefix       = "rw1_"
	tokenEntropyBytes = 32
	tokenLength       = len(tokenPrefix) + 43
)

// Generate returns an opaque token generated from 256 bits of entropy.
func Generate(random io.Reader) (string, error) {
	entropy := make([]byte, tokenEntropyBytes)
	if _, err := io.ReadFull(random, entropy); err != nil {
		return "", errors.New("failed to read token entropy")
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(entropy), nil
}

func tokenDigest(token string) ([sha256.Size]byte, bool) {
	if len(token) != tokenLength || !strings.HasPrefix(token, tokenPrefix) {
		return [sha256.Size]byte{}, false
	}

	encoded := token[len(tokenPrefix):]
	entropy, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(entropy) != tokenEntropyBytes || base64.RawURLEncoding.EncodeToString(entropy) != encoded {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(entropy), true
}
