// Package tlsconfig loads server TLS material and bootstraps local certificates.
package tlsconfig

import (
	"crypto/tls"
	"errors"
)

var errLoadServer = errors.New("failed to load server TLS certificate")

// LoadServer loads a PEM certificate and private key for a TLS 1.3 server.
func LoadServer(certPath, keyPath string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, errLoadServer
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
	}, nil
}
