package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rochecompaan/repowolf/internal/config"
)

// LookPath is the startup executable lookup used by service wiring.
var LookPath = exec.LookPath

// Toolset contains canonical executable paths pinned for the service lifetime.
type Toolset struct {
	GH  string
	SSH string
}

// ResolveTools resolves and validates the provider executables once at startup.
func ResolveTools(tools config.Tools, lookPath func(string) (string, error)) (Toolset, error) {
	if lookPath == nil {
		return Toolset{}, fmt.Errorf("resolve provider tools: lookup is unavailable")
	}
	gh, err := resolveTool("gh", tools.GH, lookPath)
	if err != nil {
		return Toolset{}, err
	}
	ssh, err := resolveTool("ssh", tools.SSH, lookPath)
	if err != nil {
		return Toolset{}, err
	}
	return Toolset{GH: gh, SSH: ssh}, nil
}

func resolveTool(name string, override *string, lookPath func(string) (string, error)) (string, error) {
	path := ""
	if override != nil {
		if !filepath.IsAbs(*override) {
			return "", fmt.Errorf("resolve %s: override must be absolute", name)
		}
		path = *override
	} else {
		var err error
		path, err = lookPath(name)
		if err != nil {
			return "", fmt.Errorf("resolve %s: executable not found", name)
		}
	}

	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: invalid executable path", name)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %s: canonicalize executable: %w", name, err)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("resolve %s: inspect executable: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("resolve %s: path is not a regular executable", name)
	}
	return canonical, nil
}
