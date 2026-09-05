package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rochecompaan/repowolf/internal/config"
)

func TestResolveToolsAlwaysResolvesSSH(t *testing.T) {
	directory := t.TempDir()
	ssh := filepath.Join(directory, "ssh")
	gh := filepath.Join(directory, "gh")
	for _, path := range []string{ssh, gh} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	calls := []string{}
	lookPath := func(name string) (string, error) {
		calls = append(calls, name)
		if name == "ssh" {
			return ssh, nil
		}
		return gh, nil
	}

	tools, err := ResolveTools(config.Tools{}, false, lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if tools.SSH != ssh {
		t.Fatalf("SSH = %q, want %q", tools.SSH, ssh)
	}
	if len(calls) != 1 || calls[0] != "ssh" {
		t.Fatalf("lookups = %#v, want [ssh]", calls)
	}
}

func TestResolveToolsSkipsGHForGiteaOnlyRuntime(t *testing.T) {
	ssh := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	invalidGH := filepath.Join(t.TempDir(), "missing-gh")
	calls := []string{}
	lookPath := func(name string) (string, error) {
		calls = append(calls, name)
		return ssh, nil
	}

	tools, err := ResolveTools(config.Tools{GH: &invalidGH}, false, lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if tools.GH != "" || tools.SSH != ssh {
		t.Fatalf("tools = %#v, want empty GH and SSH %q", tools, ssh)
	}
	if len(calls) != 1 || calls[0] != "ssh" {
		t.Fatalf("lookups = %#v, want [ssh]", calls)
	}
}

func TestResolveToolsRequiresGHForGitHubRuntime(t *testing.T) {
	directory := t.TempDir()
	ssh := filepath.Join(directory, "ssh")
	gh := filepath.Join(directory, "gh")
	for _, path := range []string{ssh, gh} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	calls := []string{}
	lookPath := func(name string) (string, error) {
		calls = append(calls, name)
		if name == "gh" {
			return gh, nil
		}
		return ssh, nil
	}

	tools, err := ResolveTools(config.Tools{}, true, lookPath)
	if err != nil {
		t.Fatal(err)
	}
	if tools.GH != gh || tools.SSH != ssh {
		t.Fatalf("tools = %#v, want GH %q and SSH %q", tools, gh, ssh)
	}
	if len(calls) != 2 || calls[0] != "gh" || calls[1] != "ssh" {
		t.Fatalf("lookups = %#v, want [gh ssh]", calls)
	}
}

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

	tools, err := ResolveTools(config.Tools{}, true, lookPath)
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
	tools, err := ResolveTools(config.Tools{GH: &gh, SSH: &ssh}, true, func(string) (string, error) {
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
			_, err := ResolveTools(config.Tools{}, true, func(string) (string, error) { return resolved, nil })
			if err == nil {
				t.Fatal("ResolveTools succeeded")
			}
		})
	}
}
