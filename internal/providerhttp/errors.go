package providerhttp

import "errors"

var (
	ErrAuthority     = errors.New("provider HTTP authority rejected")
	ErrAuthorization = errors.New("provider HTTP authorization rejected")
	ErrRedirect      = errors.New("provider HTTP redirect rejected")
)
