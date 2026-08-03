package clientconfig

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func readRegularFile(path string, limit int64) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		unix.Close(fd)
		return nil, fmt.Errorf("open file")
	}
	defer file.Close()

	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return nil, err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("file is not regular")
	}
	if info.Size < 0 || info.Size > limit {
		return nil, fmt.Errorf("file exceeds limit")
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, fmt.Errorf("file exceeds limit")
	}
	return contents, nil
}
