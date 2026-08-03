package clientconfig

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func tlsConfig(config Config) (*tls.Config, error) {
	endpoint, err := validate(config)
	if err != nil {
		return nil, err
	}
	roots, err := trustRoots(config.CAFile)
	if err != nil {
		return nil, err
	}
	serverName := config.ServerName
	if serverName == "" {
		serverName = endpoint.Hostname()
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
		ServerName: serverName,
	}, nil
}

func trustRoots(caFile string) (*x509.CertPool, error) {
	if caFile == "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system trust roots: %w", err)
		}
		return roots, nil
	}
	contents, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read REPOWOLF_CA_FILE: %w", err)
	}
	roots := x509.NewCertPool()
	foundCA := false
	for rest := contents; len(rest) != 0; {
		block, remaining := pem.Decode(rest)
		rest = remaining
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse REPOWOLF_CA_FILE: %w", err)
		}
		if certificate.IsCA {
			foundCA = true
		}
	}
	if !foundCA || !roots.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("REPOWOLF_CA_FILE contains no CA certificates")
	}
	return roots, nil
}
