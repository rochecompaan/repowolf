//go:build integration && linux

package integration_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestJailCommandPermitsShellRuntimeEnvironment(t *testing.T) {
	for _, runtimeVariable := range []string{"OLDPWD", "TMPDIR"} {
		t.Run(runtimeVariable, func(t *testing.T) {
			command := exec.Command(mustLookPath(t, "bash"), filepath.Join(testRepositoryRoot(t), "integration", "testdata", "jail-command.sh"), "client", "git")
			command.Env = []string{
				"REPOWOLF_ENDPOINT=https://127.0.0.1:8443",
				"REPOWOLF_TOKEN=test-token",
				"REPOWOLF_CA_FILE=/not-mounted",
				"GIT_SSH_COMMAND=repowolf-git-ssh",
				runtimeVariable + "=/shell-runtime",
			}
			err := command.Run()
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) || exitError.ExitCode() != 24 {
				t.Fatalf("jail command rejected a shell runtime environment variable before its CA mount check")
			}
		})
	}
}
