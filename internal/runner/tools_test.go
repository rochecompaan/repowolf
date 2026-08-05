package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
)

func TestResolveToolsCanonicalizesEachToolOnce(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "provider-tool")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "provider-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	lookPath := func(name string) (string, error) {
		calls[name]++
		return link, nil
	}

	tools, err := ResolveTools(config.Tools{}, lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if tools.GH != target || tools.SSH != target {
		t.Fatalf("tools = %#v, want canonical path %q", tools, target)
	}
	if calls["gh"] != 1 || calls["ssh"] != 1 {
		t.Fatalf("lookups = %#v, want one per tool", calls)
	}
}

func TestResolveToolsUsesAbsoluteOverridesWithoutPATHLookup(t *testing.T) {
	target := filepath.Join(t.TempDir(), "provider-tool")
	if err := os.WriteFile(target, []byte("provider"), 0o700); err != nil {
		t.Fatal(err)
	}
	gh, ssh := target, target
	lookups := 0
	tools, err := ResolveTools(config.Tools{GH: &gh, SSH: &ssh}, func(string) (string, error) {
		lookups++
		return "", errors.New("unexpected lookup")
	})
	if err != nil {
		t.Fatal(err)
	}
	if lookups != 0 || tools.GH != target || tools.SSH != target {
		t.Fatalf("lookups=%d tools=%#v", lookups, tools)
	}
}

func TestResolveToolsRejectsUnsafeExecutables(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("MVP process runner targets Linux")
	}
	directory := t.TempDir()
	nonExecutable := filepath.Join(directory, "plain")
	if err := os.WriteFile(nonExecutable, []byte("plain"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, resolved := range map[string]string{
		"relative":       "relative/tool",
		"directory":      directory,
		"non-executable": nonExecutable,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveTools(config.Tools{}, func(string) (string, error) { return resolved, nil })
			if err == nil {
				t.Fatal("ResolveTools succeeded")
			}
		})
	}
}
