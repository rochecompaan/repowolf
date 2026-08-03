package tlsconfig

import (
	"crypto/ed25519"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net"
	"path/filepath"
	"time"
)

const (
	certificateMode fs.FileMode = 0o644
	privateKeyMode  fs.FileMode = 0o600
)

var errInvalidInitOptions = errors.New("invalid certificate initialization options")

// InitOptions configures local certificate generation.
type InitOptions struct {
	OutputDir   string
	DNSNames    []string
	IPAddresses []net.IP
	Now         func() time.Time
	Random      io.Reader
}

// Files names the generated public certificates and private keys.
type Files struct {
	CACertificate     string
	CAPrivateKey      string
	ServerCertificate string
	ServerPrivateKey  string
}

type fileSpec struct {
	path     string
	contents []byte
	mode     fs.FileMode
}

// Init creates a local CA and a server certificate without replacing any file.
func Init(options InitOptions) (Files, error) {
	files := outputFiles(options.OutputDir)
	if options.OutputDir == "" || options.Now == nil || options.Random == nil ||
		(len(options.DNSNames) == 0 && len(options.IPAddresses) == 0) {
		return Files{}, errInvalidInitOptions
	}

	specs, err := generateFileSpecs(options, files)
	if err != nil {
		return Files{}, err
	}
	if err := publishAll(options.OutputDir, specs); err != nil {
		return Files{}, err
	}
	return files, nil
}

func outputFiles(dir string) Files {
	return Files{
		CACertificate:     filepath.Join(dir, "ca.crt"),
		CAPrivateKey:      filepath.Join(dir, "ca.key"),
		ServerCertificate: filepath.Join(dir, "tls.crt"),
		ServerPrivateKey:  filepath.Join(dir, "tls.key"),
	}
}

func generateFileSpecs(options InitOptions, files Files) ([]fileSpec, error) {
	now := options.Now()
	caPublic, caPrivate, err := ed25519.GenerateKey(options.Random)
	if err != nil {
		return nil, err
	}
	caSerial, err := randomSerial(options.Random)
	if err != nil {
		return nil, err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "RepoWolf Local CA"},
		NotBefore:             now,
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(options.Random, caTemplate, caTemplate, caPublic, caPrivate)
	if err != nil {
		return nil, err
	}

	serverPublic, serverPrivate, err := ed25519.GenerateKey(options.Random)
	if err != nil {
		return nil, err
	}
	serverSerial, err := randomSerial(options.Random)
	if err != nil {
		return nil, err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: "RepoWolf Server"},
		NotBefore:    now,
		NotAfter:     now.Add(397 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     append([]string(nil), options.DNSNames...),
		IPAddresses:  cloneIPs(options.IPAddresses),
	}
	serverDER, err := x509.CreateCertificate(options.Random, serverTemplate, caTemplate, serverPublic, caPrivate)
	if err != nil {
		return nil, err
	}

	caKey, err := privateKeyPEM(caPrivate)
	if err != nil {
		return nil, err
	}
	serverKey, err := privateKeyPEM(serverPrivate)
	if err != nil {
		return nil, err
	}
	return []fileSpec{
		{path: files.CACertificate, contents: certificatePEM(caDER), mode: certificateMode},
		{path: files.CAPrivateKey, contents: caKey, mode: privateKeyMode},
		{path: files.ServerCertificate, contents: certificatePEM(serverDER), mode: certificateMode},
		{path: files.ServerPrivateKey, contents: serverKey, mode: privateKeyMode},
	}, nil
}

func randomSerial(random io.Reader) (*big.Int, error) {
	serialBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, serialBytes); err != nil {
		return nil, err
	}
	serialBytes[0] |= 0x80
	return new(big.Int).SetBytes(serialBytes), nil
}

func privateKeyPEM(key ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func certificatePEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func cloneIPs(addresses []net.IP) []net.IP {
	cloned := make([]net.IP, len(addresses))
	for i, address := range addresses {
		cloned[i] = append(net.IP(nil), address...)
	}
	return cloned
}
