package tlsconfig

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

func TestInitGeneratesVerifiableCertificates(t *testing.T) {
	files, err := Init(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	ca := readCertificate(t, files.CACertificate)
	server := readCertificate(t, files.ServerCertificate)
	if !ca.IsCA {
		t.Fatal("CA certificate IsCA = false")
	}
	if ca.KeyUsage != x509.KeyUsageCertSign {
		t.Fatalf("CA KeyUsage = %v, want only CertSign", ca.KeyUsage)
	}
	if !reflect.DeepEqual(server.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}) {
		t.Fatalf("server ExtKeyUsage = %v", server.ExtKeyUsage)
	}
	if server.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("server KeyUsage = %v", server.KeyUsage)
	}
	if _, ok := ca.PublicKey.(ed25519.PublicKey); !ok {
		t.Fatalf("CA public key type = %T", ca.PublicKey)
	}
	if _, ok := server.PublicKey.(ed25519.PublicKey); !ok {
		t.Fatalf("server public key type = %T", server.PublicKey)
	}
	if ca.SerialNumber.Cmp(server.SerialNumber) == 0 || ca.SerialNumber.BitLen() != 128 || server.SerialNumber.BitLen() != 128 {
		t.Fatalf("serials = %x and %x, want distinct 128-bit values", ca.SerialNumber, server.SerialNumber)
	}
	if !ca.NotBefore.Equal(testNow) || !ca.NotAfter.Equal(testNow.AddDate(5, 0, 0)) {
		t.Fatalf("CA validity = %v through %v", ca.NotBefore, ca.NotAfter)
	}
	if !server.NotBefore.Equal(testNow) || !server.NotAfter.Equal(testNow.Add(397*24*time.Hour)) {
		t.Fatalf("server validity = %v through %v", server.NotBefore, server.NotAfter)
	}

	if err := ca.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("CA self-signature error = %v", err)
	}
	if err := server.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("server signature error = %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, name := range []string{"repo.internal", "127.0.0.1"} {
		if _, err := server.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, CurrentTime: testNow}); err != nil {
			t.Fatalf("server.Verify(%q) error = %v", name, err)
		}
	}
	if _, err := server.Verify(x509.VerifyOptions{Roots: roots, DNSName: "wrong.internal", CurrentTime: testNow}); err == nil {
		t.Fatal("server.Verify(wrong.internal) unexpectedly succeeded")
	}
}

func TestInitUsesRestrictivePrivateKeyModes(t *testing.T) {
	files, err := Init(testOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	for _, path := range []string{files.CAPrivateKey, files.ServerPrivateKey} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 600", filepath.Base(path), got)
		}
	}
	for _, path := range []string{files.CACertificate, files.ServerCertificate} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("%s mode = %o, want 644", filepath.Base(path), got)
		}
	}
}

func TestInitRefusesOverwriteWithoutChangingAnyFile(t *testing.T) {
	dir := t.TempDir()
	files, err := Init(testOptions(dir))
	if err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	before := snapshotFiles(t, allFiles(files))

	if _, err := Init(testOptions(dir)); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second Init() error = %v, want fs.ErrExist", err)
	}
	for path, want := range before {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s after refusal = %x, %v; want unchanged", filepath.Base(path), got, err)
		}
	}
	assertNoTemporaryFiles(t, dir)
}

func TestInitPreexistingFinalIsPreservedAndInvocationFilesAreCleaned(t *testing.T) {
	const sentinel = "preexisting sentinel"
	for _, finalName := range []string{"ca.crt", "ca.key", "tls.crt", "tls.key"} {
		t.Run(finalName, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, finalName)
			if err := os.WriteFile(path, []byte(sentinel), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, err := Init(testOptions(dir)); !errors.Is(err, fs.ErrExist) {
				t.Fatalf("Init() error = %v, want fs.ErrExist", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != sentinel {
				t.Fatalf("preexisting file = %q, %v; want sentinel", got, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != finalName {
				t.Fatalf("directory entries = %v; want only %s", entryNames(entries), finalName)
			}
		})
	}
}

func TestCleanupPublishedPreservesAReplacement(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged")
	final := filepath.Join(dir, "final")
	generatedInfo := writeAndStat(t, staged, "generated")
	if err := os.Link(staged, final); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(final); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := cleanupPublished([]publishedFile{{path: final, info: generatedInfo}}); err != nil {
		t.Fatalf("cleanupPublished() error = %v", err)
	}
	assertFileContents(t, final, "replacement")
}

func TestCleanupInvocationPreservesAReplacedTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	temporary := filepath.Join(dir, "temporary")
	generatedInfo := writeAndStat(t, temporary, "generated")
	if err := os.Link(temporary, filepath.Join(dir, "generated-anchor")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temporary, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	staged := []stagedFile{{temporary: temporary, info: generatedInfo}}
	if err := cleanupInvocation(dir, staged, nil); err != nil {
		t.Fatalf("cleanupInvocation() error = %v", err)
	}
	assertFileContents(t, temporary, "replacement")
}

func testOptions(dir string) InitOptions {
	entropy := append(bytes.Repeat([]byte{0x91}, 32), bytes.Repeat([]byte{0xa2}, 16)...)
	entropy = append(entropy, bytes.Repeat([]byte{0xb3}, 32)...)
	entropy = append(entropy, bytes.Repeat([]byte{0xc4}, 16)...)
	return InitOptions{
		OutputDir:   dir,
		DNSNames:    []string{"repo.internal"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		Now:         func() time.Time { return testNow },
		Random:      bytes.NewReader(entropy),
	}
}

func readCertificate(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, rest := pem.Decode(contents)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatalf("invalid certificate PEM in %s", path)
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func allFiles(files Files) []string {
	return []string{files.CACertificate, files.CAPrivateKey, files.ServerCertificate, files.ServerPrivateKey}
}

func snapshotFiles(t *testing.T, paths []string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		result[path] = contents
	}
	return result
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("directory entries = %v; want four final files", entryNames(entries))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func writeAndStat(t *testing.T, path, contents string) fs.FileInfo {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
	}
}
