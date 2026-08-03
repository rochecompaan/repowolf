package auth_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
)

func TestGenerateReturnsVersionedBase64URLToken(t *testing.T) {
	entropy := bytes.Repeat([]byte{0x5a}, 32)

	token, err := auth.Generate(bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	want := "rw1_" + base64.RawURLEncoding.EncodeToString(entropy)
	if token != want {
		t.Fatal("Generate() returned an unexpected token")
	}
	if len(token) != 47 || !strings.HasPrefix(token, "rw1_") {
		t.Fatal("Generate() returned a token with an invalid format")
	}
}

func TestGenerateRejectsShortEntropy(t *testing.T) {
	_, err := auth.Generate(bytes.NewReader(make([]byte, 31)))
	if err == nil {
		t.Fatal("Generate() accepted insufficient entropy")
	}
}
