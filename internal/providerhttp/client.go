package providerhttp

import (
	"context"
	"crypto/tls"
	"errors"
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
	roots, err := loadRootCAs(options.CAFile)
	if err != nil {
		return nil, err
	}
	base := options.Base
	if base == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		tlsConfig := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
		transport.TLSClientConfig = tlsConfig.Clone()
		base = transport
	}
	bounded := &boundedTransport{base: base, timeout: options.Timeout, maxResponseBytes: options.MaxResponseBytes}
	return &http.Client{
		Transport: &authenticatedTransport{expected: expected, token: options.Token, base: bounded},
		Timeout:   options.Timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("%w", ErrRedirect)
		},
	}, nil
}

type boundedTransport struct {
	base             http.RoundTripper
	timeout          time.Duration
	maxResponseBytes int64
}

func (transport *boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	operationTimeout := transport.timeout
	lead := min(operationTimeout/10, 10*time.Millisecond)
	if lead > 0 {
		// Expire the transport-owned context just ahead of Client.Timeout so
		// transport expiry has a stable classification while the client cap
		// remains defense in depth.
		operationTimeout -= lead
	}
	callerShorter := false
	if deadline, ok := request.Context().Deadline(); ok && time.Until(deadline) < operationTimeout {
		callerShorter = true
	}
	operationContext, cancel := context.WithTimeout(request.Context(), operationTimeout)
	clone := request.Clone(operationContext)
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		cancel()
		return nil, mapOperationError(request.Context(), operationContext, callerShorter, err)
	}
	if response == nil {
		cancel()
		return nil, fmt.Errorf("provider HTTP transport returned no response")
	}
	if isRedirect(response.StatusCode) && response.Header.Get("Location") != "" {
		response.Header = response.Header.Clone()
		response.Header.Set("Location", "https://redirect.invalid/")
	}
	if response.Body == nil {
		cancel()
		return response, nil
	}
	response.Body = newBoundedResponseBody(response.Body, operationContext, request.Context(), callerShorter, cancel, transport.maxResponseBytes)
	return response, nil
}

func mapOperationError(caller, operation context.Context, callerShorter bool, err error) error {
	if callerErr := caller.Err(); callerErr != nil && (callerShorter || errors.Is(callerErr, context.Canceled)) {
		return callerErr
	}
	if errors.Is(operation.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w", ErrTimeout)
	}
	if contextErr := operation.Err(); contextErr != nil {
		return contextErr
	}
	return err
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}
