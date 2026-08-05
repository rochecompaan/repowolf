package auth_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/rochecompaan/repowolf/internal/auth"
	"github.com/rochecompaan/repowolf/internal/config"
)

func TestLoadRejectsMissingToken(t *testing.T) {
	principals := map[string]config.Principal{
		"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}},
	}

	_, err := auth.Load(principals, func(string) (string, bool) { return "", false })
	assertSafeEnvironmentError(t, err, "REPOWOLF_TOKEN_AGENT", "")
}

func TestLoadRejectsEmptyToken(t *testing.T) {
	principals := map[string]config.Principal{
		"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}},
	}

	_, err := auth.Load(principals, func(string) (string, bool) { return "", true })
	assertSafeEnvironmentError(t, err, "REPOWOLF_TOKEN_AGENT", "")
}

func TestLoadDoesNotDiscloseMalformedToken(t *testing.T) {
	secret := "not-a-valid-token"
	principals := map[string]config.Principal{
		"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT"}},
	}

	_, err := auth.Load(principals, func(string) (string, bool) { return secret, true })
	assertSafeEnvironmentError(t, err, "REPOWOLF_TOKEN_AGENT", secret)
}

func TestLoadRejectsDuplicateTokenForOnePrincipal(t *testing.T) {
	secret := testToken(1)
	principals := map[string]config.Principal{
		"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A", "REPOWOLF_TOKEN_AGENT_B"}},
	}

	_, err := auth.Load(principals, lookupValues(map[string]string{
		"REPOWOLF_TOKEN_AGENT_A": secret,
		"REPOWOLF_TOKEN_AGENT_B": secret,
	}))
	assertSafeEnvironmentError(t, err, "REPOWOLF_TOKEN_AGENT_B", secret)
}

func TestLoadRejectsDuplicateTokenAcrossPrincipals(t *testing.T) {
	secret := testToken(2)
	principals := map[string]config.Principal{
		"agent-a": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A"}},
		"agent-b": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_B"}},
	}

	_, err := auth.Load(principals, lookupValues(map[string]string{
		"REPOWOLF_TOKEN_AGENT_A": secret,
		"REPOWOLF_TOKEN_AGENT_B": secret,
	}))
	assertSafeEnvironmentError(t, err, "REPOWOLF_TOKEN_AGENT_B", secret)
}

func TestLoadAuthenticatesDistinctTokensForOnePrincipal(t *testing.T) {
	first := testToken(3)
	second := testToken(4)
	principals := map[string]config.Principal{
		"agent": {TokenEnvs: []string{"REPOWOLF_TOKEN_AGENT_A", "REPOWOLF_TOKEN_AGENT_B"}},
	}

	index, err := auth.Load(principals, lookupValues(map[string]string{
		"REPOWOLF_TOKEN_AGENT_A": first,
		"REPOWOLF_TOKEN_AGENT_B": second,
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, token := range []string{first, second} {
		principal, ok := index.Authenticate(token)
		if !ok || principal != "agent" {
			t.Fatalf("Authenticate() = (%q, %t), want (agent, true)", principal, ok)
		}
	}
	if _, ok := index.Authenticate(testToken(5)); ok {
		t.Fatal("Authenticate() accepted an unconfigured token")
	}
	if _, ok := index.Authenticate("not-a-valid-token"); ok {
		t.Fatal("Authenticate() accepted a malformed token")
	}
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

func lookupValues(values map[string]string) auth.LookupEnv {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}

func testToken(byteValue byte) string {
	return "rw1_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byteValue}, 32))
}

func assertSafeEnvironmentError(t *testing.T, err error, environment, secret string) {
	t.Helper()
	if err == nil {
		t.Fatal("Load() returned nil error")
	}
	if !strings.Contains(err.Error(), environment) {
		t.Fatalf("Load() error did not name %q", environment)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatal("Load() error disclosed a token")
	}
}
