package providerhttp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/rochecompaan/repowolf/internal/testutil"
)

func TestLoadRootCAsAppendsPrivateCA(t *testing.T) {
	certificate := testutil.GenerateCertificate(t, t.TempDir())
	system, err := loadRootCAs("")
	if err != nil {
		t.Fatal(err)
	}
	combined, err := loadRootCAs(certificate.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(combined.Subjects()) <= len(system.Subjects()) {
		t.Fatalf("combined roots = %d, system roots = %d", len(combined.Subjects()), len(system.Subjects()))
	}
}

func TestLoadRootCAsRejectsInvalidFiles(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, content []byte, mode os.FileMode) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, content, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oversized := make([]byte, maxCAFileBytes+1)
	unreadable := write("unreadable.pem", []byte("certificate"), 0)
	fifo := filepath.Join(directory, "roots.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(directory, "missing.pem")},
		{name: "empty", path: write("empty.pem", nil, 0o600)},
		{name: "malformed", path: write("malformed.pem", []byte("not PEM"), 0o600)},
		{name: "no certificate", path: write("key.pem", []byte("-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----\n"), 0o600)},
		{name: "oversized", path: write("oversized.pem", oversized, 0o600)},
		{name: "directory", path: directory},
		{name: "FIFO", path: fifo},
		{name: "unreadable", path: unreadable},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "unreadable" {
				if file, err := os.Open(test.path); err == nil {
					file.Close()
					t.Skip("current user can read mode-000 file")
				}
			}
			_, err := loadRootCAs(test.path)
			if !errors.Is(err, ErrTrustRoots) {
				t.Fatalf("loadRootCAs() error = %v, want trust roots", err)
			}
			if !strings.Contains(err.Error(), test.path) {
				t.Fatalf("error %q does not name path", err)
			}
		})
	}
}
