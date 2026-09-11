package providerhttp

import "errors"

var (
	ErrAuthority     = errors.New("provider HTTP authority rejected")
	ErrAuthorization = errors.New("provider HTTP authorization rejected")
	ErrRedirect      = errors.New("provider HTTP redirect rejected")
	ErrTrustRoots    = errors.New("provider HTTP trust roots rejected")
	ErrTimeout       = errors.New("provider HTTP operation timed out")
	ErrResponseLimit = errors.New("provider HTTP response limit exceeded")
)
