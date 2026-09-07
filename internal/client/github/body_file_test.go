package github

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParsePullEditBodyFile(t *testing.T) {
	dir := newGitHubRepository(t)
	path := filepath.Join(dir, "body.md")
	if err := os.WriteFile(path, []byte("new body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{"relative", "body.md", "new body\n"},
		{"absolute", path, "new body\n"},
		{"empty", writeEmptyBodyFile(t, dir), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := Parse([]string{"pr", "edit", "18", "--body-file", test.path}, dir)
			if err != nil {
				t.Fatal(err)
			}
			edit := request.GetPullEdit()
			if edit == nil || edit.Body == nil || *edit.Body != test.body {
				t.Fatalf("unexpected body: %#v", edit)
			}
			if strings.Contains(request.String(), test.path) {
				t.Fatalf("request leaked path %q", test.path)
			}
		})
	}
}

func TestParsePullEditBodyFileRejects(t *testing.T) {
	dir := newGitHubRepository(t)
	largePath := writeBodyFile(t, dir, "large.md", strings.Repeat("x", 65_537), 0o600)
	invalidUTF8Path := filepath.Join(dir, "invalid.md")
	if err := os.WriteFile(invalidUTF8Path, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	regularPath := writeBodyFile(t, dir, "regular.md", "body", 0o600)
	symlinkPath := filepath.Join(dir, "body-link.md")
	if err := os.Symlink(regularPath, symlinkPath); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(dir, "body.fifo")
	if err := unix.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatal(err)
	}
	unreadablePath := writeBodyFile(t, dir, "unreadable.md", "body", 0o000)

	tests := []struct {
		name string
		args []string
		skip bool
	}{
		{"too large", []string{"pr", "edit", "18", "--body-file", largePath}, false},
		{"invalid UTF-8", []string{"pr", "edit", "18", "--body-file", invalidUTF8Path}, false},
		{"symbolic link", []string{"pr", "edit", "18", "--body-file", symlinkPath}, false},
		{"directory", []string{"pr", "edit", "18", "--body-file", dir}, false},
		{"FIFO", []string{"pr", "edit", "18", "--body-file", fifoPath}, false},
		{"missing", []string{"pr", "edit", "18", "--body-file", filepath.Join(dir, "missing.md")}, false},
		{"unreadable", []string{"pr", "edit", "18", "--body-file", unreadablePath}, os.Geteuid() == 0},
		{"duplicate", []string{"pr", "edit", "18", "--body-file", regularPath, "--body-file", regularPath}, false},
		{"inline conflict", []string{"pr", "edit", "18", "--body", "inline", "--body-file", regularPath}, false},
		{"missing flag value", []string{"pr", "edit", "18", "--body-file"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.skip {
				t.Skip("root can read mode-000 files")
			}
			request, err := Parse(test.args, dir)
			if err == nil {
				t.Fatalf("accepted %#v", test.args)
			}
			if request != nil {
				t.Fatalf("returned request with body file content: %#v", request)
			}
		})
	}
}

func writeEmptyBodyFile(t *testing.T, dir string) string {
	t.Helper()
	return writeBodyFile(t, dir, "empty.md", "", 0o600)
}

func writeBodyFile(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return path
}
