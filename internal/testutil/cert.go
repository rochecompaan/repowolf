// Package testutil provides offline black-box integration fixtures.
package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Certificate is a generated loopback-only test certificate bundle.
type Certificate struct {
	CAFile          string
	CertificateFile string
	KeyFile         string
	ServerName      string
}

// GenerateCertificate creates a private CA and loopback server certificate.
func GenerateCertificate(t testing.TB, directory string) Certificate {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute)
	caKey := newKey(t)
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RepoWolf test CA"},
		NotBefore: now, NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER := createCertificate(t, ca, ca, &caKey.PublicKey, caKey)
	serverKey := newKey(t)
	server := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: now, NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	serverDER := createCertificate(t, server, ca, &serverKey.PublicKey, caKey)
	bundle := Certificate{
		CAFile: filepath.Join(directory, "ca.pem"), CertificateFile: filepath.Join(directory, "server.pem"),
		KeyFile: filepath.Join(directory, "server-key.pem"), ServerName: "localhost",
	}
	writePEM(t, bundle.CAFile, 0o600, "CERTIFICATE", caDER)
	writePEM(t, bundle.CertificateFile, 0o600, "CERTIFICATE", serverDER)
	keyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, bundle.KeyFile, 0o600, "PRIVATE KEY", keyDER)
	return bundle
}

func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func createCertificate(t testing.TB, template, parent *x509.Certificate, public any, signer any) []byte {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, template, parent, public, signer)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func writePEM(t testing.TB, path string, mode os.FileMode, kind string, der []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(file, &pem.Block{Type: kind, Bytes: der}); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
