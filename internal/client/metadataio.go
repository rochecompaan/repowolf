package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func openDirectoryPath(path string) (int, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	clean := filepath.Clean(absolute)
	current, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		if component == "" {
			continue
		}
		next, openErr := openDirectoryAt(current, component)
		unix.Close(current)
		if openErr != nil {
			return -1, openErr
		}
		current = next
	}
	return current, nil
}

func openDirectoryAt(directory int, name string) (int, error) {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return -1, fmt.Errorf("invalid metadata directory component")
	}
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func openParentDirectory(directory int) (int, error) {
	return unix.Openat(directory, "..", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}

func readRegularAt(directory int, name string, limit int64) ([]byte, error) {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid metadata file component")
	}
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("open metadata file")
	}
	defer file.Close()

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("metadata is not a regular file")
	}
	if info.Size < 0 || info.Size > limit {
		return nil, fmt.Errorf("metadata exceeds client limit")
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("metadata exceeds client limit")
	}
	return contents, nil
}

func regularFileStatAt(directory int, name string) (unix.Stat_t, error) {
	var info unix.Stat_t
	err := unix.Fstatat(directory, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		return info, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return info, fmt.Errorf("metadata is not a regular file")
	}
	return info, nil
}

func sameFile(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino
}
