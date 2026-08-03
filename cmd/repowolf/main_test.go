package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 || stdout.String() != "dev\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunUsageForUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.String() != "usage: repowolf <serve|config|token|cert>\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunTokenGenerate(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"token", "generate"}, &stdout, &stderr); code != 0 || stdout.Len() != 48 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout-bytes=%d stderr=%q", code, stdout.Len(), stderr.String())
	}
}

func TestRunCertInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certificates")
	var stdout, stderr bytes.Buffer
	code := run([]string{"cert", "init", "--output", dir, "--dns", "repo.internal"}, &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, path := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("generated path %q: %v", path, err)
		}
	}
}

func TestRunCertInitUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"cert", "init", "--output", t.TempDir()}, &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "invalid cert init arguments\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
