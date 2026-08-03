package clientconfig

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "rw1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestLoadEnvAcceptsOnlyTLSClientSettings(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte(testCAPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	setClientEnv(t, "https://127.0.0.1:8443", testToken, caFile, "service.example")
	config, err := LoadEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Endpoint != "https://127.0.0.1:8443" || config.Token != testToken || config.CAFile != caFile || config.ServerName != "service.example" {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadEnvRejectsInvalidEndpointAndToken(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		token    string
	}{
		{"missing endpoint", "", testToken},
		{"plaintext endpoint", "http://service.example", testToken},
		{"endpoint path", "https://service.example/path", testToken},
		{"endpoint credentials", "https://user@service.example", testToken},
		{"missing token", "https://service.example", ""},
		{"malformed token", "https://service.example", "secret"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setClientEnv(t, test.endpoint, test.token, "", "")
			if config, err := LoadEnv(); err == nil {
				t.Fatalf("LoadEnv() accepted %#v", config)
			}
		})
	}
}

func TestTLSConfigRejectsInvalidCustomCA(t *testing.T) {
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := tlsConfig(Config{Endpoint: "https://service.example", Token: testToken, CAFile: caFile}); err == nil {
		t.Fatal("tlsConfig accepted invalid CA")
	}
}

func TestBearerCredentialsRequireTransportSecurity(t *testing.T) {
	credentials := bearerCredentials{token: testToken}
	metadata, err := credentials.GetRequestMetadata(context.Background())
	if err != nil || metadata["authorization"] != "Bearer "+testToken {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}
	if !credentials.RequireTransportSecurity() {
		t.Fatal("bearer credentials permitted plaintext transport")
	}
}

func TestDialHonorsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := Dial(ctx, Config{Endpoint: "https://127.0.0.1:1", Token: testToken})
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("Dial() = %v", err)
	}
}

func setClientEnv(t *testing.T, endpoint, token, caFile, serverName string) {
	t.Helper()
	t.Setenv("REPOWOLF_ENDPOINT", endpoint)
	t.Setenv("REPOWOLF_TOKEN", token)
	t.Setenv("REPOWOLF_CA_FILE", caFile)
	t.Setenv("REPOWOLF_SERVER_NAME", serverName)
}

// A syntactically valid self-signed CA fixture. Trust, expiry, and identity are
// exercised with a freshly generated certificate in client_test.go.
const testCAPEM = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIRAOQOf0qqBk9J1ihZHEd5SIgwCgYIKoZIzj0EAwIwEjEQ
MA4GA1UEAxMHdGVzdC1jYTAeFw0yNTAxMDEwMDAwMDBaFw0zNTAxMDEwMDAwMDBa
MBIxEDAOBgNVBAMTB3Rlc3QtY2EwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAASB
bYBHIHFEjBq9cMknwXmAS6r5yBUkNx26Fb58CxJM4G9q2pK2ygxomLUyiXmCGjLe
YhPIG8k7d/jhVn6A4u8Wo2MwYTAdBgNVHQ4EFgQUkhQ3IoQm9VhyMc0FXjrd0r5R
MZcwDwYDVR0TAQH/BAUwAwEB/zAOBgNVHQ8BAf8EBAMCAQYwHwYDVR0jBBgwFoAU
khQ3IoQm9VhyMc0FXjrd0r5RMZcwCgYIKoZIzj0EAwIDSAAwRQIgVbQjliT+1+yP
PrxMB9IhXovEJVnALmILc87KWcRvYAECIQDrmh7jZaZnSAyJMMN+qjMQI5qctovt
xcQmkTeMEgrl+Q==
-----END CERTIFICATE-----
`
