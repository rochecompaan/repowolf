package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Binaries are the service and the two public multicall client links.
type Binaries struct {
	Service string
	Client  string
	GH      string
	GitSSH  string
}

// BuildBinaries builds only the public commands into directory.
func BuildBinaries(t testing.TB, directory string) Binaries {
	t.Helper()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	root := repositoryRoot(t)
	service := filepath.Join(directory, "repowolf")
	client := filepath.Join(directory, "repowolf-client")
	build(t, root, service, "./cmd/repowolf")
	build(t, root, client, "./cmd/repowolf-client")
	binaries := Binaries{Service: service, Client: client, GH: filepath.Join(directory, "gh"), GitSSH: filepath.Join(directory, "repowolf-git-ssh")}
	if err := os.Symlink(client, binaries.GH); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(client, binaries.GitSSH); err != nil {
		t.Fatal(err)
	}
	return binaries
}

// Environment replaces named values without leaving duplicate host entries.
func Environment(base []string, values ...string) []string {
	replacements := make(map[string]string, len(values))
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		replacements[name] = value
	}
	result := make([]string, 0, len(base)+len(values))
	for _, value := range base {
		name, _, _ := strings.Cut(value, "=")
		if _, replaced := replacements[name]; !replaced {
			result = append(result, value)
		}
	}
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		if replacements[name] == value {
			result = append(result, value)
			delete(replacements, name)
		}
	}
	return result
}

// InstallExecutable copies a deterministic test script and makes it executable.
func InstallExecutable(t testing.TB, source, destination string) string {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	return destination
}

func build(t testing.TB, root, output, packagePath string) {
	t.Helper()
	command := exec.Command("go", "build", "-trimpath", "-o", output, packagePath)
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", packagePath, err, result)
	}
}

func repositoryRoot(t testing.TB) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate testutil source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}
