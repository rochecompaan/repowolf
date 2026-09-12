package gitea

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "gitea.dev/sdk"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/providerhttp"
	"github.com/rochecompaan/repowolf/internal/testutil"
)

func TestNewConstructsGiteaClientWithoutNetwork(t *testing.T) {
	client, err := New(config.Provider{Kind: config.ProviderGitea, APIHost: "gitea.example.com", GitHost: "gitea.example.com", SSHUser: "git", SSHPort: 22, TokenEnv: "REPOWOLF_TOKEN_GITEA"}, "secret")
	if err != nil || client == nil {
		t.Fatalf("New() client = %v, error = %v", client, err)
	}
	var _ *sdk.Client = client
}

func TestNewRejectsInvalidDirectProviderRecords(t *testing.T) {
	for _, provider := range []config.Provider{
		{Kind: config.ProviderGitHub, APIHost: "gitea.example.com"},
		{Kind: config.ProviderGitea, APIHost: "https://gitea.example.com"},
		{Kind: config.ProviderGitea, APIHost: "gitea.example.com:3000"},
		{Kind: config.ProviderGitea, APIHost: ""},
	} {
		if client, err := New(provider, "secret"); err == nil || client != nil {
			t.Fatalf("New(%#v) = %v, %v", provider, client, err)
		}
	}
}

func TestNewClientDoesNotProbeNetwork(t *testing.T) {
	var calls atomic.Int32
	client, err := newClient("https://gitea.example.com/", providerhttp.Options{Token: "secret", Timeout: time.Second, MaxResponseBytes: 1024, Base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected network call")
	})})
	if err != nil || client == nil || calls.Load() != 0 {
		t.Fatalf("newClient() client = %v, error = %v, calls = %d", client, err, calls.Load())
	}
}

func TestSDKServerVersionUsesBoundedAuthenticatedTransport(t *testing.T) {
	const token = "sdk-secret"
	var gotPath, gotAuthorization, gotHost string
	server, certificate := sdkTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath, gotAuthorization, gotHost = request.URL.Path, request.Header.Get("Authorization"), request.Host
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"version":"1.24.7"}`)
	}))
	defer server.Close()
	client, err := newClient(server.URL, providerhttp.Options{Token: token, CAFile: certificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	version, _, err := client.ServerVersion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.24.7" || gotPath != "/api/v1/version" || gotAuthorization != "token "+token || gotHost != strings.TrimPrefix(server.URL, "https://") {
		t.Fatalf("version/path/auth/host = %q, %q, %q, %q", version, gotPath, gotAuthorization, gotHost)
	}
}

func TestSDKErrorResponsesDoNotDiscloseBody(t *testing.T) {
	const token = "sdk-failure-secret"
	for _, test := range []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "reflected token in JSON message", contentType: "application/json", body: `{"message":"` + token + `"}`},
		{name: "short raw body", contentType: "text/plain", body: "short-provider-body"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, certificate := sdkTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()
			client, err := newClient(server.URL, providerhttp.Options{Token: token, CAFile: certificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.ServerVersion(context.Background())
			if err == nil {
				t.Fatal("ServerVersion() unexpectedly succeeded")
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), test.body) || strings.Contains(err.Error(), "short-provider-body") {
				t.Fatalf("error disclosed provider response body: %v", err)
			}
		})
	}
}

func TestSDKTransportFailuresAreStableAndSafe(t *testing.T) {
	const token = "sdk-failure-secret"
	tests := []struct {
		name    string
		handler http.Handler
		options func(testutil.Certificate) providerhttp.Options
		want    error
	}{
		{name: "redirect", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Location", "https://untrusted.example/private")
			writer.WriteHeader(http.StatusFound)
		}), options: func(cert testutil.Certificate) providerhttp.Options {
			return providerhttp.Options{Token: token, CAFile: cert.CAFile, Timeout: time.Second, MaxResponseBytes: 1024}
		}, want: providerhttp.ErrRedirect},
		{name: "timeout", handler: http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) { <-request.Context().Done() }), options: func(cert testutil.Certificate) providerhttp.Options {
			return providerhttp.Options{Token: token, CAFile: cert.CAFile, Timeout: 40 * time.Millisecond, MaxResponseBytes: 1024}
		}, want: providerhttp.ErrTimeout},
		{name: "response limit", handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(writer, `{"version":"`+strings.Repeat("x", 2048)+`"}`)
		}), options: func(cert testutil.Certificate) providerhttp.Options {
			return providerhttp.Options{Token: token, CAFile: cert.CAFile, Timeout: time.Second, MaxResponseBytes: 1024}
		}, want: providerhttp.ErrResponseLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, certificate := sdkTLSServer(t, test.handler)
			defer server.Close()
			client, err := newClient(server.URL, test.options(certificate))
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = client.ServerVersion(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("ServerVersion() error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), strings.Repeat("x", 32)) {
				t.Fatalf("error disclosed sensitive data: %v", err)
			}
		})
	}
}

func TestSDKClientsKeepAuthoritiesAndTokensIsolated(t *testing.T) {
	var firstAuth, secondAuth string
	first, firstCertificate := sdkTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		firstAuth = request.Header.Get("Authorization")
		_, _ = io.WriteString(writer, `{"version":"1.1"}`)
	}))
	defer first.Close()
	second, secondCertificate := sdkTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		secondAuth = request.Header.Get("Authorization")
		_, _ = io.WriteString(writer, `{"version":"2.2"}`)
	}))
	defer second.Close()
	firstClient, err := newClient(first.URL, providerhttp.Options{Token: "first-token", CAFile: firstCertificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	secondClient, err := newClient(second.URL, providerhttp.Options{Token: "second-token", CAFile: secondCertificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	firstVersion, _, firstErr := firstClient.ServerVersion(context.Background())
	secondVersion, _, secondErr := secondClient.ServerVersion(context.Background())
	if firstErr != nil || secondErr != nil || firstVersion != "1.1" || secondVersion != "2.2" || firstAuth != "token first-token" || secondAuth != "token second-token" {
		t.Fatalf("isolated calls = %q/%v/%q, %q/%v/%q", firstVersion, firstErr, firstAuth, secondVersion, secondErr, secondAuth)
	}
}

func TestSDKRejectsUntrustedCertificate(t *testing.T) {
	server, _ := sdkTLSServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	client, err := newClient(server.URL, providerhttp.Options{Token: "secret", Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ServerVersion(context.Background()); err == nil {
		t.Fatal("untrusted server was accepted")
	}
}

func sdkTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, testutil.Certificate) {
	t.Helper()
	certificate := testutil.GenerateCertificate(t, t.TempDir())
	pair, err := tls.LoadX509KeyPair(certificate.CertificateFile, certificate.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	return server, certificate
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
