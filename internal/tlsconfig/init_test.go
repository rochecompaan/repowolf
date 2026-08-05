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
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

func TestInitPublishesCompleteVerifiableCertificateDirectory(t *testing.T) {
	output := testOutput(t)
	files, err := Init(testOptions(output))
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if files != outputFiles(output) {
		t.Fatalf("Init() files = %#v, want %#v", files, outputFiles(output))
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("output mode = %v, want private directory mode 0700", info.Mode())
	}
	assertNoStagingDirectories(t, filepath.Dir(output))

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
	files, err := Init(testOptions(testOutput(t)))
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

func TestInitRefusesExistingDestinationWithoutChangingItsFiles(t *testing.T) {
	output := testOutput(t)
	files, err := Init(testOptions(output))
	if err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	before := snapshotFiles(t, allFiles(files))

	if _, err := Init(testOptions(output)); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second Init() error = %v, want fs.ErrExist", err)
	}
	for path, want := range before {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s after refusal = %x, %v; want unchanged", filepath.Base(path), got, err)
		}
	}
	assertNoStagingDirectories(t, filepath.Dir(output))
}

func TestInitPreservesEveryExistingDestinationType(t *testing.T) {
	for _, destinationType := range []string{"file", "directory", "symlink"} {
		t.Run(destinationType, func(t *testing.T) {
			parent := t.TempDir()
			output := filepath.Join(parent, "certificates")
			const sentinel = "preexisting sentinel"
			switch destinationType {
			case "file":
				if err := os.WriteFile(output, []byte(sentinel), 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(output, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "sentinel"), []byte(sentinel), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(parent, "target")
				if err := os.Mkdir(target, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "sentinel"), []byte(sentinel), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, output); err != nil {
					t.Fatal(err)
				}
			}

			if _, err := Init(testOptions(output)); !errors.Is(err, fs.ErrExist) {
				t.Fatalf("Init() error = %v, want fs.ErrExist", err)
			}
			switch destinationType {
			case "file":
				assertFileContents(t, output, sentinel)
			case "directory":
				assertOnlySentinel(t, output, sentinel)
			case "symlink":
				info, err := os.Lstat(output)
				if err != nil || info.Mode()&fs.ModeSymlink == 0 {
					t.Fatalf("destination after Init = %v, %v; want symlink", info, err)
				}
				assertOnlySentinel(t, output, sentinel)
			}
			assertNoStagingDirectories(t, parent)
		})
	}
}

func TestInitPrepublicationFailureLeavesNoFinalPath(t *testing.T) {
	publishError := errors.New("publish failed")
	originalRename := renameNoReplace
	renameNoReplace = func(int, string, int, string, uint) error { return publishError }
	t.Cleanup(func() { renameNoReplace = originalRename })

	output := testOutput(t)
	if _, err := Init(testOptions(output)); !errors.Is(err, publishError) {
		t.Fatalf("Init() error = %v, want publication error", err)
	}
	if _, err := os.Lstat(output); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("output after failed Init = %v, want absent", err)
	}
	assertNoStagingDirectories(t, filepath.Dir(output))
}

func TestInitDoesNotRollBackAfterPublication(t *testing.T) {
	syncError := errors.New("parent sync failed")
	originalSync := syncParent
	syncParent = func(int) error { return syncError }
	t.Cleanup(func() { syncParent = originalSync })

	output := testOutput(t)
	if _, err := Init(testOptions(output)); !errors.Is(err, syncError) {
		t.Fatalf("Init() error = %v, want parent sync error", err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 4 {
		t.Fatalf("published output entries = %v, %v; want complete output", entries, err)
	}
}

func TestInitJoinsPrepublicationAndCleanupErrors(t *testing.T) {
	publishError := errors.New("publish failed")
	cleanupError := errors.New("staging cleanup failed")
	originalRename := renameNoReplace
	originalRemove := removeStaging
	renameNoReplace = func(int, string, int, string, uint) error { return publishError }
	removeStaging = func(string) error { return cleanupError }
	t.Cleanup(func() {
		renameNoReplace = originalRename
		removeStaging = originalRemove
	})

	output := testOutput(t)
	_, err := Init(testOptions(output))
	if !errors.Is(err, publishError) || !errors.Is(err, cleanupError) {
		t.Fatalf("Init() error = %v, want joined publication and cleanup errors", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("output after failed Init = %v, want absent", statErr)
	}
}

func testOptions(output string) InitOptions {
	entropy := append(bytes.Repeat([]byte{0x91}, 32), bytes.Repeat([]byte{0xa2}, 16)...)
	entropy = append(entropy, bytes.Repeat([]byte{0xb3}, 32)...)
	entropy = append(entropy, bytes.Repeat([]byte{0xc4}, 16)...)
	return InitOptions{
		OutputDir:   output,
		DNSNames:    []string{"repo.internal"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		Now:         func() time.Time { return testNow },
		Random:      bytes.NewReader(entropy),
	}
}

func testOutput(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "certificates")
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

func assertNoStagingDirectories(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".repowolf-cert-") {
			t.Fatalf("staging directory %q remains", entry.Name())
		}
	}
}

func assertOnlySentinel(t *testing.T, directory, contents string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Fatalf("%s entries = %v, %v; want only sentinel", directory, entries, err)
	}
	assertFileContents(t, filepath.Join(directory, "sentinel"), contents)
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), got, err, want)
	}
}
