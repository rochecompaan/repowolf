package providerhttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

func TestRedirectsAreRejectedWithoutReplayOrDisclosure(t *testing.T) {
	const token = "redirect-secret"
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				writer.Header().Set("Location", "https://untrusted.example/private-location")
				writer.WriteHeader(status)
			}))
			defer server.Close()

			client, err := New(Options{Authority: strings.TrimPrefix(server.URL, "https://"), Token: token, Timeout: time.Second, MaxResponseBytes: 1024, Base: server.Client().Transport})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("not-replayed"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			if !errors.Is(err, ErrRedirect) || calls.Load() != 1 {
				t.Fatalf("Do() response = %v, error = %v, calls = %d", response, err, calls.Load())
			}
			if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "private-location") || strings.Contains(err.Error(), "untrusted.example") {
				t.Fatalf("redirect error disclosed sensitive data: %v", err)
			}
		})
	}
}

func TestTLSRequiresPrivateRootAndTLS13(t *testing.T) {
	server, certificate := privateTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(writer, "ok") }), tls.VersionTLS13)
	defer server.Close()
	authority := strings.TrimPrefix(server.URL, "https://")

	trusted, err := New(Options{Authority: authority, Token: "secret", CAFile: certificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	response, err := trusted.Get(server.URL)
	if err != nil {
		t.Fatalf("private CA request failed: %v", err)
	}
	response.Body.Close()

	untrusted, err := New(Options{Authority: authority, Token: "secret", Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrusted.Get(server.URL); err == nil {
		t.Fatal("request trusted an unconfigured private CA")
	}

	tls12, tls12Certificate := privateTLSServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), tls.VersionTLS12)
	defer tls12.Close()
	client, err := New(Options{Authority: strings.TrimPrefix(tls12.URL, "https://"), Token: "secret", CAFile: tls12Certificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(tls12.URL); err == nil {
		t.Fatal("TLS 1.2-only server was accepted")
	}
}

func TestTLSPerformsHostnameVerification(t *testing.T) {
	server, certificate := privateTLSServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), tls.VersionTLS13)
	defer server.Close()
	pool := x509.NewCertPool()
	pemBytes, err := os.ReadFile(certificate.CAFile)
	if err != nil || !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("load test CA")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	port := server.Listener.Addr().(*net.TCPAddr).Port
	client, err := New(Options{Authority: fmt.Sprintf("[::1]:%d", port), Token: "secret", Timeout: time.Second, MaxResponseBytes: 1024, Base: transport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(fmt.Sprintf("https://[::1]:%d/", port)); err == nil {
		t.Fatal("certificate hostname mismatch was accepted")
	}
}

func TestWholeOperationDeadlineAndCallerDeadline(t *testing.T) {
	server, certificate := privateTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}), tls.VersionTLS13)
	defer server.Close()
	authority := strings.TrimPrefix(server.URL, "https://")
	client, err := New(Options{Authority: authority, Token: "secret", CAFile: certificate.CAFile, Timeout: 40 * time.Millisecond, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(server.URL); !errors.Is(err, ErrTimeout) {
		t.Fatalf("stalled header error = %v, want timeout", err)
	}

	client, err = New(Options{Authority: authority, Token: "secret", CAFile: certificate.CAFile, Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if _, err := client.Do(request); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTimeout) {
		t.Fatalf("caller deadline error = %v", err)
	}
}

func TestWholeOperationDeadlineIncludesResponseBody(t *testing.T) {
	server, certificate := privateTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(writer, "x")
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	}), tls.VersionTLS13)
	defer server.Close()
	client, err := New(Options{Authority: strings.TrimPrefix(server.URL, "https://"), Token: "secret", CAFile: certificate.CAFile, Timeout: 40 * time.Millisecond, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(response.Body)
	if string(content) != "x" || !errors.Is(err, ErrTimeout) {
		t.Fatalf("body = %q, error = %v", content, err)
	}
}

func TestCallerCancellationClosesResponseBody(t *testing.T) {
	providerBody := &blockingBody{closed: make(chan struct{})}
	client, err := New(Options{Authority: "example.com", Token: "secret", Timeout: time.Second, MaxResponseBytes: 1024, Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: providerBody, Request: request}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-providerBody.closed:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not close provider body")
	}
	if _, err := response.Body.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() error = %v, want cancellation", err)
	}
}

func TestDecodedResponseLimitInclusiveForAllStatuses(t *testing.T) {
	const limit = int64(8 << 20)
	for _, status := range []int{http.StatusOK, http.StatusBadGateway} {
		for _, size := range []int64{0, 7, limit, limit + 1} {
			t.Run(fmt.Sprintf("%d/%d", status, size), func(t *testing.T) {
				body := &countingBody{remaining: size}
				client, err := New(Options{Authority: "example.com", Token: "secret", Timeout: time.Second, MaxResponseBytes: limit, Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: request}, nil
				})})
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Get("https://example.com/")
				if err != nil {
					t.Fatal(err)
				}
				content, readErr := io.ReadAll(response.Body)
				if int64(len(content)) != min(size, limit) {
					t.Fatalf("read %d bytes", len(content))
				}
				if size > limit && !errors.Is(readErr, ErrResponseLimit) {
					t.Fatalf("read error = %v", readErr)
				}
				if size <= limit && readErr != nil {
					t.Fatalf("read error = %v", readErr)
				}
				if !body.closed.Load() {
					t.Fatal("body was not closed at terminal read")
				}
			})
		}
	}
}

func TestResponseLimitCountsDecodedGzipBytes(t *testing.T) {
	const limit = int64(1024)
	server, certificate := privateTLSServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Encoding", "gzip")
		compressed := gzip.NewWriter(writer)
		_, _ = compressed.Write(bytes.Repeat([]byte("x"), int(limit+1)))
		_ = compressed.Close()
	}), tls.VersionTLS13)
	defer server.Close()
	client, err := New(Options{Authority: strings.TrimPrefix(server.URL, "https://"), Token: "secret", CAFile: certificate.CAFile, Timeout: time.Second, MaxResponseBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(response.Body)
	if !errors.Is(err, ErrResponseLimit) || int64(len(content)) != limit {
		t.Fatalf("decoded read = %d, error = %v", len(content), err)
	}
}

func privateTLSServer(t *testing.T, handler http.Handler, maxVersion uint16) (*httptest.Server, testutil.Certificate) {
	t.Helper()
	certificate := testutil.GenerateCertificate(t, t.TempDir())
	pair, err := tls.LoadX509KeyPair(certificate.CertificateFile, certificate.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, MaxVersion: maxVersion}
	server.StartTLS()
	return server, certificate
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type blockingBody struct {
	closed chan struct{}
	once   atomic.Bool
}

func (body *blockingBody) Read([]byte) (int, error) {
	<-body.closed
	return 0, io.ErrClosedPipe
}

func (body *blockingBody) Close() error {
	if body.once.CompareAndSwap(false, true) {
		close(body.closed)
	}
	return nil
}

type countingBody struct {
	remaining int64
	closed    atomic.Bool
}

func (body *countingBody) Read(buffer []byte) (int, error) {
	if body.remaining == 0 {
		return 0, io.EOF
	}
	n := min(int64(len(buffer)), body.remaining)
	for i := range buffer[:n] {
		buffer[i] = 'x'
	}
	body.remaining -= n
	return int(n), nil
}
func (body *countingBody) Close() error { body.closed.Store(true); return nil }
