package cli

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/rochecompaan/repowolf/internal/tlsconfig"
)

var (
	// ErrInvalidCertArguments identifies invalid cert init command arguments.
	ErrInvalidCertArguments  = errors.New("invalid cert init arguments")
	errCertInitFailed        = errors.New("certificate initialization failed")
	errWriteCertificatePaths = errors.New("failed to write certificate paths")
)

// RunCertInit parses cert init flags, generates certificate material, and writes its paths.
func RunCertInit(args []string, stdout io.Writer, now func() time.Time, random io.Reader) error {
	output, dnsNames, addresses, err := parseCertInit(args)
	if err != nil {
		return err
	}
	files, err := tlsconfig.Init(tlsconfig.InitOptions{
		OutputDir:   output,
		DNSNames:    dnsNames,
		IPAddresses: addresses,
		Now:         now,
		Random:      random,
	})
	if err != nil {
		return errCertInitFailed
	}
	for _, path := range []string{files.CACertificate, files.CAPrivateKey, files.ServerCertificate, files.ServerPrivateKey} {
		if _, err := fmt.Fprintln(stdout, path); err != nil {
			return errWriteCertificatePaths
		}
	}
	return nil
}

func parseCertInit(args []string) (string, []string, []net.IP, error) {
	var output string
	var dnsNames []string
	var addresses []net.IP
	for len(args) > 0 {
		flag := args[0]
		args = args[1:]
		if len(args) == 0 {
			return "", nil, nil, ErrInvalidCertArguments
		}
		value := args[0]
		args = args[1:]
		switch flag {
		case "--output":
			if output != "" || value == "" {
				return "", nil, nil, ErrInvalidCertArguments
			}
			output = value
		case "--dns":
			if value == "" {
				return "", nil, nil, ErrInvalidCertArguments
			}
			dnsNames = append(dnsNames, value)
		case "--ip":
			address := net.ParseIP(value)
			if address == nil {
				return "", nil, nil, ErrInvalidCertArguments
			}
			addresses = append(addresses, address)
		default:
			return "", nil, nil, ErrInvalidCertArguments
		}
	}
	if output == "" || len(dnsNames)+len(addresses) == 0 {
		return "", nil, nil, ErrInvalidCertArguments
	}
	return output, dnsNames, addresses, nil
}
