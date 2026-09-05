package auth_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
)

func TestNewIndexAuthenticatesDistinctTokens(t *testing.T) {
	first := testToken(1)
	second := testToken(2)
	index, err := auth.NewIndex(map[string][]string{
		"agent-a": {first},
		"agent-b": {second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if principal, ok := index.Authenticate(first); !ok || principal != "agent-a" {
		t.Fatalf("Authenticate(first) = %q, %v", principal, ok)
	}
	if principal, ok := index.Authenticate(second); !ok || principal != "agent-b" {
		t.Fatalf("Authenticate(second) = %q, %v", principal, ok)
	}
}

func TestNewIndexRejectsMalformedPrincipalTokenWithoutDisclosure(t *testing.T) {
	secret := "not-a-valid-token"
	_, err := auth.NewIndex(map[string][]string{"agent": {secret}})
	assertSafeTokenError(t, err, "agent", secret)
}

func TestNewIndexRejectsDuplicatePrincipalTokenWithoutDisclosure(t *testing.T) {
	secret := testToken(1)
	_, err := auth.NewIndex(map[string][]string{
		"agent-a": {secret},
		"agent-b": {secret},
	})
	assertSafeTokenError(t, err, "agent-b", secret)
}

func TestPrincipalContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := auth.Principal(ctx); ok {
		t.Fatal("Principal() found a principal in a context without one")
	}

	ctx = auth.WithPrincipal(ctx, "agent")
	principal, ok := auth.Principal(ctx)
	if !ok || principal != "agent" {
		t.Fatalf("Principal() = (%q, %t), want (agent, true)", principal, ok)
	}
}

func testToken(byteValue byte) string {
	return "rw1_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byteValue}, 32))
}

func assertSafeTokenError(t *testing.T, err error, identifier, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("NewIndex() returned nil error")
	}
	if !strings.Contains(err.Error(), identifier) {
		t.Fatalf("NewIndex() error did not name %q", identifier)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("NewIndex() error disclosed a token")
	}
}
