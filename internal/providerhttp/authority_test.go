package providerhttp

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingTransport struct {
	calls int
	req   *http.Request
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	transport.req = request
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
}

func TestAuthorityAndAuthorization(t *testing.T) {
	const token = "provider-secret"
	tests := []struct {
		name      string
		authority string
		request   string
		header    string
		wantErr   error
	}{
		{name: "DNS case folded", authority: "EXAMPLE.com", request: "https://example.COM/path"},
		{name: "implicit expected port", authority: "example.com", request: "https://example.com:443/path"},
		{name: "explicit expected port", authority: "example.com:443", request: "https://example.com/path"},
		{name: "canonical IPv4", authority: "127.0.0.1", request: "https://127.0.0.1/path"},
		{name: "canonical IPv6", authority: "[0:0:0:0:0:0:0:1]", request: "https://[::1]/path"},
		{name: "HTTP", authority: "example.com", request: "http://example.com/path", wantErr: ErrAuthority},
		{name: "wrong host", authority: "example.com", request: "https://other.example/path", wantErr: ErrAuthority},
		{name: "wrong port", authority: "example.com", request: "https://example.com:444/path", wantErr: ErrAuthority},
		{name: "userinfo", authority: "example.com", request: "https://user@example.com/path", wantErr: ErrAuthority},
		{name: "authorization already present", authority: "example.com", request: "https://example.com/path", header: "Bearer caller", wantErr: ErrAuthorization},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &recordingTransport{}
			client, err := New(Options{Authority: test.authority, Token: token, Timeout: time.Second, MaxResponseBytes: 1024, Base: base})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodGet, test.request, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.header != "" {
				request.Header.Set("Authorization", test.header)
			}
			originalURL := new(url.URL)
			*originalURL = *request.URL
			originalHeader := request.Header.Clone()
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Do() error = %v, want %v", err, test.wantErr)
				}
				if base.calls != 0 {
					t.Fatal("rejected request reached base transport")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if base.calls != 1 || base.req.Header.Values("Authorization")[0] != "token "+token || len(base.req.Header.Values("Authorization")) != 1 {
					t.Fatalf("dispatch = %d, Authorization = %q", base.calls, base.req.Header.Values("Authorization"))
				}
			}
			if !reflect.DeepEqual(request.URL, originalURL) || !reflect.DeepEqual(request.Header, originalHeader) {
				t.Fatal("transport mutated caller request")
			}
			if err != nil && strings.Contains(err.Error(), token) {
				t.Fatal("error disclosed token")
			}
		})
	}
}

func TestNewRejectsMalformedAuthorityWithoutTokenLeak(t *testing.T) {
	const token = "provider-secret"
	for _, authority := range []string{"", "https://example.com", "user@example.com", "example.com:bad", "example.com/path"} {
		_, err := New(Options{Authority: authority, Token: token, Timeout: time.Second, MaxResponseBytes: 1, Base: &recordingTransport{}})
		if !errors.Is(err, ErrAuthority) || strings.Contains(err.Error(), token) {
			t.Fatalf("New(%q) error = %v", authority, err)
		}
	}
}
