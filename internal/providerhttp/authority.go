package providerhttp

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type authority struct {
	host string
	port string
}

func parseAuthority(value string) (authority, error) {
	if value == "" || strings.Contains(value, "://") {
		return authority{}, fmt.Errorf("%w: invalid configured authority", ErrAuthority)
	}
	parsed, err := url.Parse("//" + value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return authority{}, fmt.Errorf("%w: invalid configured authority", ErrAuthority)
	}
	return normalizeAuthority(parsed.Hostname(), parsed.Port())
}

func requestAuthority(request *http.Request) (authority, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Host == "" {
		return authority{}, fmt.Errorf("%w: request must use an HTTPS authority", ErrAuthority)
	}
	return normalizeAuthority(request.URL.Hostname(), request.URL.Port())
}

func normalizeAuthority(host, port string) (authority, error) {
	if host == "" {
		return authority{}, fmt.Errorf("%w: empty host", ErrAuthority)
	}
	if port == "" {
		port = "443"
	}
	if address, err := netip.ParseAddr(host); err == nil {
		host = address.String()
	} else {
		for _, character := range host {
			if character > 0x7f {
				return authority{}, fmt.Errorf("%w: non-ASCII host", ErrAuthority)
			}
		}
		host = strings.ToLower(host)
	}
	return authority{host: host, port: port}, nil
}

type authenticatedTransport struct {
	expected authority
	token    string
	base     http.RoundTripper
}

func (transport *authenticatedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	actual, err := requestAuthority(request)
	if err != nil {
		return nil, err
	}
	if actual != transport.expected {
		return nil, fmt.Errorf("%w: request authority does not match provider", ErrAuthority)
	}
	if len(request.Header.Values("Authorization")) != 0 {
		return nil, fmt.Errorf("%w: request already has authorization", ErrAuthorization)
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "token "+transport.token)
	return transport.base.RoundTrip(clone)
}
