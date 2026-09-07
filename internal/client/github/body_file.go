package github

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maximumBodyFileBytes = 65_536

func readBodyFile(cwd, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("body file path is required")
	}
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(cwd, resolved)
	}
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("open body file")
	}
	file := os.NewFile(uintptr(fd), resolved)
	if file == nil {
		_ = unix.Close(fd)
		return "", fmt.Errorf("open body file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("body file must be regular")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximumBodyFileBytes+1))
	if err != nil || len(raw) > maximumBodyFileBytes || !utf8.Valid(raw) {
		return "", fmt.Errorf("invalid body file")
	}
	return string(raw), nil
}
