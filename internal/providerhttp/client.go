package providerhttp

import (
	"fmt"
	"net/http"
	"time"
)

type Options struct {
	Authority        string
	Token            string
	CAFile           string
	Timeout          time.Duration
	MaxResponseBytes int64
	Base             http.RoundTripper
}

func New(options Options) (*http.Client, error) {
	expected, err := parseAuthority(options.Authority)
	if err != nil {
		return nil, err
	}
	if options.Token == "" {
		return nil, fmt.Errorf("%w: empty provider token", ErrAuthorization)
	}
	if options.Timeout <= 0 {
		return nil, fmt.Errorf("provider HTTP timeout must be positive")
	}
	if options.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("provider HTTP response limit must be positive")
	}
	base := options.Base
	if base == nil {
		base = http.DefaultTransport
	}
	base = &redirectSanitizingTransport{base: base}
	return &http.Client{
		Transport: &authenticatedTransport{expected: expected, token: options.Token, base: base},
		Timeout:   options.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("%w", ErrRedirect)
		},
	}, nil
}

type redirectSanitizingTransport struct {
	base http.RoundTripper
}

func (transport *redirectSanitizingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if response != nil && isRedirect(response.StatusCode) && response.Header.Get("Location") != "" {
		response.Header = response.Header.Clone()
		response.Header.Set("Location", "https://redirect.invalid/")
	}
	return response, err
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}
