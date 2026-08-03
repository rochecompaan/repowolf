package client

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	gitPointerLimit   = 4096
	gitConfigLimit    = 64 << 10
	gitDiscoveryDepth = 64
)

// originRemote reads bounded local Git metadata only to produce an untrusted
// request hint. It never interprets the remote as policy or authority.
func originRemote(cwd string) (string, error) {
	contents, err := discoverGitConfig(cwd)
	if err != nil {
		return "", err
	}
	return originURL(contents)
}

func discoverGitConfig(cwd string) ([]byte, error) {
	current, err := openDirectoryPath(cwd)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(current) }()

	for depth := 0; depth < gitDiscoveryDepth; depth++ {
		var marker unix.Stat_t
		err := unix.Fstatat(current, ".git", &marker, unix.AT_SYMLINK_NOFOLLOW)
		switch {
		case err == nil && marker.Mode&unix.S_IFMT == unix.S_IFDIR:
			gitDirectory, openErr := openDirectoryAt(current, ".git")
			if openErr != nil {
				return nil, openErr
			}
			defer unix.Close(gitDirectory)
			return readRegularAt(gitDirectory, "config", gitConfigLimit)
		case err == nil && marker.Mode&unix.S_IFMT == unix.S_IFREG:
			pointer, readErr := readRegularAt(current, ".git", gitPointerLimit)
			if readErr != nil {
				return nil, readErr
			}
			return linkedWorktreeConfig(marker, pointer)
		case err == nil:
			return nil, fmt.Errorf("invalid .git metadata type")
		case err != unix.ENOENT:
			return nil, err
		}

		parent, openErr := openParentDirectory(current)
		if openErr != nil {
			return nil, openErr
		}
		var hereInfo, parentInfo unix.Stat_t
		if unix.Fstat(current, &hereInfo) != nil || unix.Fstat(parent, &parentInfo) != nil {
			unix.Close(parent)
			return nil, fmt.Errorf("inspect repository ancestors")
		}
		if sameFile(hereInfo, parentInfo) {
			unix.Close(parent)
			return nil, fmt.Errorf("Git repository unavailable")
		}
		unix.Close(current)
		current = parent
	}
	return nil, fmt.Errorf("Git repository discovery depth exceeded")
}

func linkedWorktreeConfig(marker unix.Stat_t, pointer []byte) ([]byte, error) {
	target, err := gitdirPointer(pointer)
	if err != nil {
		return nil, err
	}
	if filepath.Base(filepath.Dir(target)) != "worktrees" || filepath.Base(target) == "worktrees" {
		return nil, fmt.Errorf("invalid linked-worktree layout")
	}
	metadata, err := openDirectoryPath(target)
	if err != nil {
		return nil, err
	}
	defer unix.Close(metadata)

	commonPointer, err := readRegularAt(metadata, "commondir", gitPointerLimit)
	if err != nil || strings.TrimSpace(string(commonPointer)) != "../.." {
		return nil, fmt.Errorf("invalid linked-worktree common directory")
	}
	backPointer, err := readRegularAt(metadata, "gitdir", gitPointerLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid linked-worktree back pointer")
	}
	if err := validateBackPointer(marker, strings.TrimSpace(string(backPointer))); err != nil {
		return nil, err
	}

	worktrees, err := openParentDirectory(metadata)
	if err != nil {
		return nil, err
	}
	defer unix.Close(worktrees)
	common, err := openParentDirectory(worktrees)
	if err != nil {
		return nil, err
	}
	defer unix.Close(common)
	return readRegularAt(common, "config", gitConfigLimit)
}

func gitdirPointer(contents []byte) (string, error) {
	line := strings.TrimSpace(string(contents))
	if !strings.HasPrefix(strings.ToLower(line), "gitdir:") {
		return "", fmt.Errorf("invalid git directory pointer")
	}
	value := strings.TrimSpace(line[len("gitdir:"):])
	if !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("invalid git directory pointer")
	}
	return value, nil
}

func validateBackPointer(marker unix.Stat_t, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("invalid linked-worktree back pointer")
	}
	parent, err := openDirectoryPath(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("invalid linked-worktree back pointer")
	}
	defer unix.Close(parent)
	backMarker, err := regularFileStatAt(parent, filepath.Base(path))
	if err != nil || !sameFile(marker, backMarker) {
		return fmt.Errorf("invalid linked-worktree back pointer")
	}
	return nil
}

func originURL(contents []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(contents)))
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			inOrigin = section == `remote "origin"` || section == "remote.origin"
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || !strings.EqualFold(strings.TrimSpace(key), "url") {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return "", err
			}
			value = decoded
		}
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return "", fmt.Errorf("invalid origin URL")
		}
		return value, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("origin URL unavailable")
}
