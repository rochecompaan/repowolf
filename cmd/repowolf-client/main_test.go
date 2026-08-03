package main

import (
	"bytes"
	"context"
	"testing"
)

func TestModeForBase(t *testing.T) {
	for _, name := range []string{"gh", "repowolf-git-ssh"} {
		if mode, ok := modeForBase(name); !ok || mode != name {
			t.Fatalf("modeForBase(%q) = %q, %v", name, mode, ok)
		}
	}
	if _, ok := modeForBase("unknown"); ok {
		t.Fatal("unexpected client mode")
	}
}

func TestModeForBaseUsesExecutableBase(t *testing.T) {
	if mode, ok := modeForBase("/sandbox/bin/gh"); !ok || mode != "gh" {
		t.Fatalf("modeForBase() = %q, %v", mode, ok)
	}
}

func TestRunClientDispatchesGhCompatibilityMode(t *testing.T) {
	var stderr bytes.Buffer
	status := runClient(context.Background(), "/sandbox/bin/gh", []string{"api", "/user"}, &bytes.Buffer{}, &stderr)
	if status != 2 || stderr.String() != "gh: unsupported or invalid command\n" {
		t.Fatalf("runClient() = %d, stderr=%q", status, stderr.String())
	}
}
