package gitssh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestRunHandlesOnlyExactGitSSHVariantProbe(t *testing.T) {
	for _, args := range [][]string{
		{"-G", "git@git.example"},
		{"-G", "-p", "2222", "git@git.example"},
		{"-G", "-o", "SendEnv=GIT_PROTOCOL", "git@git.example"},
		{"-G", "-o", "SendEnv=GIT_PROTOCOL", "-p", "1", "git@git.example"},
		{"-G", "-o", "SendEnv=GIT_PROTOCOL", "-p", "65535", "git@Git.Example"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			setGitEnv(t, "", "", "", "")
			var stdout, stderr bytes.Buffer
			if status := Run(context.Background(), args, bytes.NewReader(nil), &stdout, &stderr); status != 0 {
				t.Fatalf("Run(%q) = %d, stdout=%q stderr=%q", args, status, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("probe produced stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}

	nearMisses := map[string][]string{
		"wrong protocol option": {"-G", "-o", "SendEnv=OTHER", "git@git.example"},
		"combined option":       {"-G", "-oSendEnv=GIT_PROTOCOL", "git@git.example"},
		"port before option":    {"-G", "-p", "22", "-o", "SendEnv=GIT_PROTOCOL", "git@git.example"},
		"duplicate probe flag":  {"-G", "-G", "git@git.example"},
		"duplicate option":      {"-G", "-o", "SendEnv=GIT_PROTOCOL", "-o", "SendEnv=GIT_PROTOCOL", "git@git.example"},
		"duplicate port":        {"-G", "-p", "22", "-p", "22", "git@git.example"},
		"zero port":             {"-G", "-o", "SendEnv=GIT_PROTOCOL", "-p", "0", "git@git.example"},
		"signed port":           {"-G", "-o", "SendEnv=GIT_PROTOCOL", "-p", "+22", "git@git.example"},
		"large port":            {"-G", "-o", "SendEnv=GIT_PROTOCOL", "-p", "65536", "git@git.example"},
		"wrong user":            {"-G", "-o", "SendEnv=GIT_PROTOCOL", "root@git.example"},
		"extra option":          {"-G", "-o", "SendEnv=GIT_PROTOCOL", "-v", "git@git.example"},
		"remote command":        {"-G", "-o", "SendEnv=GIT_PROTOCOL", "git@git.example", "git-upload-pack 'owner/repo.git'"},
	}
	for name, args := range nearMisses {
		t.Run(name, func(t *testing.T) {
			setGitEnv(t, "", "", "", "")
			var stdout, stderr bytes.Buffer
			if status := Run(context.Background(), args, bytes.NewReader(nil), &stdout, &stderr); status != 2 {
				t.Fatalf("Run(%q) = %d, stdout=%q stderr=%q", args, status, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || stderr.String() != fixedDiagnostic {
				t.Fatalf("near-miss probe produced stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestInstalledGitGeneratedSSHArgvCompatibility(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("installed git is required for the argv compatibility harness")
	}
	recorder := writeSSHArgvRecorder(t)

	tests := []struct {
		name      string
		remote    string
		operation Operation
		selector  *repowolfv1.RepositorySelector
		push      bool
	}{
		{
			name:      "scp style upload",
			remote:    "git@git.example:owner/repo.git",
			operation: UploadPack,
			selector:  &repowolfv1.RepositorySelector{Host: "git.example", Owner: "owner", Name: "repo"},
		},
		{
			name:      "SSH URL explicit port upload",
			remote:    "ssh://git@git.example:2222/owner/repo.git",
			operation: UploadPack,
			selector:  &repowolfv1.RepositorySelector{Host: "git.example", SshPort: 2222, Owner: "owner", Name: "repo"},
		},
		{
			name:      "SSH URL explicit port receive",
			remote:    "ssh://git@git.example:2222/owner/repo.git",
			operation: ReceivePack,
			selector:  &repowolfv1.RepositorySelector{Host: "git.example", SshPort: 2222, Owner: "owner", Name: "repo"},
			push:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recordDir := t.TempDir()
			if test.push {
				runRecordedGitPush(t, git, recorder, recordDir, test.remote)
			} else {
				runRecordedGit(t, git, recorder, recordDir, "ls-remote", test.remote)
			}
			invocations := readRecordedSSHInvocations(t, recordDir)
			if len(invocations) != 2 {
				t.Fatalf("recorded SSH invocations = %q, want probe and command", invocations)
			}
			var stdout, stderr bytes.Buffer
			setGitEnv(t, "", "", "", "")
			if status := Run(context.Background(), invocations[0], bytes.NewReader(nil), &stdout, &stderr); status != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("generated probe Run() = %d, stdout=%q stderr=%q args=%q", status, stdout.String(), stderr.String(), invocations[0])
			}
			request, err := Parse(invocations[1])
			if err != nil {
				t.Fatalf("Parse generated command %q: %v", invocations[1], err)
			}
			if request.Operation != test.operation || !reflect.DeepEqual(request.Repository, test.selector) {
				t.Fatalf("generated command parsed as %#v", request)
			}
		})
	}
}

func writeSSHArgvRecorder(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "record-ssh")
	script := `#!/bin/sh
set -eu
index=0
while test -e "$REPOWOLF_SSH_RECORD_DIR/$index"; do index=$((index + 1)); done
for argument do printf '%s\n' "$argument"; done >"$REPOWOLF_SSH_RECORD_DIR/$index"
if test "$#" -gt 0 && test "$1" = -G; then exit 0; fi
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func runRecordedGit(t *testing.T, git, recorder, recordDir string, args ...string) {
	t.Helper()
	command := exec.Command(git, args...)
	command.Env = recordedGitEnv(t, recorder, recordDir)
	if err := command.Run(); err == nil {
		t.Fatal("recording SSH unexpectedly completed the Git transport")
	} else if _, ok := err.(*exec.ExitError); !ok {
		t.Fatal(err)
	}
}

func runRecordedGitPush(t *testing.T, git, recorder, recordDir, remote string) {
	t.Helper()
	repository := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", repository},
		{"-C", repository, "-c", "user.name=RepoWolf Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "initial"},
	} {
		command := exec.Command(git, args...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v: %s", args, err, output)
		}
	}
	runRecordedGit(t, git, recorder, recordDir, "-C", repository, "push", remote, "HEAD:refs/heads/compatibility")
}

func recordedGitEnv(t *testing.T, recorder, recordDir string) []string {
	t.Helper()
	home := t.TempDir()
	environment := os.Environ()
	for _, key := range []string{"GIT_SSH", "GIT_SSH_COMMAND", "GIT_SSH_VARIANT", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "HOME", "REPOWOLF_SSH_RECORD_DIR"} {
		environment = removeEnvironmentKey(environment, key)
	}
	return append(environment,
		"GIT_SSH_COMMAND="+recorder,
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+home,
		"REPOWOLF_SSH_RECORD_DIR="+recordDir,
	)
}

func removeEnvironmentKey(environment []string, key string) []string {
	filtered := environment[:0]
	prefix := key + "="
	for _, value := range environment {
		if !strings.HasPrefix(value, prefix) {
			filtered = append(filtered, value)
		}
	}
	return filtered
}

func readRecordedSSHInvocations(t *testing.T, directory string) [][]string {
	t.Helper()
	var invocations [][]string
	for index := 0; ; index++ {
		contents, err := os.ReadFile(filepath.Join(directory, strconv.Itoa(index)))
		if errors.Is(err, os.ErrNotExist) {
			return invocations
		}
		if err != nil {
			t.Fatal(err)
		}
		invocations = append(invocations, strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n"))
	}
}
