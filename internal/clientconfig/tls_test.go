package clientconfig

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestTrustRootsAddsOnlyCACertificates(t *testing.T) {
	leaf, leafPEM := selfSignedServerLeaf(t, "untrusted.example")
	caFile := filepath.Join(t.TempDir(), "roots.pem")
	bundle := append(selfSignedCA(t), leafPEM...)
	if err := os.WriteFile(caFile, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	roots, err := trustRoots(caFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "untrusted.example"}); err == nil {
		t.Fatal("mixed CA bundle trusted a non-CA server leaf")
	}
}

func TestTLSConfigUsesSystemRootsAndServerNames(t *testing.T) {
	t.Run("endpoint-derived", func(t *testing.T) {
		settings, err := tlsConfig(Config{Endpoint: "https://service.example:8443", Token: testToken})
		if err != nil {
			t.Fatal(err)
		}
		if settings.RootCAs == nil || settings.ServerName != "service.example" {
			t.Fatalf("tlsConfig() roots=%v serverName=%q", settings.RootCAs != nil, settings.ServerName)
		}
	})
	t.Run("explicit", func(t *testing.T) {
		settings, err := tlsConfig(Config{Endpoint: "https://127.0.0.1:8443", Token: testToken, ServerName: "service.example"})
		if err != nil {
			t.Fatal(err)
		}
		if settings.ServerName != "service.example" {
			t.Fatalf("ServerName = %q", settings.ServerName)
		}
	})
}

func TestTrustRootsRejectsUnsafeCAFiles(t *testing.T) {
	t.Run("FIFO", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		if err := unix.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		if _, err := trustRoots(path); err == nil {
			t.Fatal("trustRoots accepted FIFO")
		}
		if time.Since(started) > 250*time.Millisecond {
			t.Fatal("trustRoots blocked on FIFO")
		}
	})
	t.Run("device", func(t *testing.T) {
		if _, err := trustRoots("/dev/null"); err == nil {
			t.Fatal("trustRoots accepted device")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target.pem")
		if err := os.WriteFile(target, []byte(testCAPEM), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "ca.pem")
		if err := os.Symlink(target, path); err != nil {
			t.Fatal(err)
		}
		if _, err := trustRoots(path); err == nil {
			t.Fatal("trustRoots followed symlink")
		}
	})
	t.Run("oversize", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := trustRoots(path); err == nil {
			t.Fatal("trustRoots accepted oversized file")
		}
	})
}

func selfSignedCA(t *testing.T) []byte {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(98),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func selfSignedServerLeaf(t *testing.T, serverName string) (*x509.Certificate, []byte) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(99),
		Subject:               pkix.Name{CommonName: serverName},
		DNSNames:              []string{serverName},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  false,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
