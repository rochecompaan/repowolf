package main

import "testing"

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
