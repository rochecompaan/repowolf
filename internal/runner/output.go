package runner

import (
	"errors"
	"io"
	"sync/atomic"
)

// limitedReader returns at most limit bytes and kills the process on overflow.
type limitedReader struct {
	raw      io.ReadCloser
	limit    int64
	count    atomic.Int64
	overflow func()
}

func (reader *limitedReader) Read(value []byte) (int, error) {
	remaining := reader.limit - reader.count.Load()
	if remaining <= 0 {
		var probe [1]byte
		n, err := reader.raw.Read(probe[:])
		if n > 0 {
			reader.overflow()
			return 0, ErrOutputLimit
		}
		return 0, err
	}
	readSize := int64(len(value))
	if readSize > remaining+1 {
		readSize = remaining + 1
	}
	n, err := reader.raw.Read(value[:readSize])
	if int64(n) > remaining {
		accepted := int(remaining)
		reader.count.Add(remaining)
		reader.overflow()
		return accepted, ErrOutputLimit
	}
	reader.count.Add(int64(n))
	return n, err
}

func (reader *limitedReader) Close() error { return reader.raw.Close() }

type stderrCapture struct {
	raw      io.ReadCloser
	limit    int64
	count    atomic.Int64
	overflow func()
	data     []byte
}

func (capture *stderrCapture) drain() error {
	buffer := make([]byte, 32*1024)
	for {
		n, err := capture.raw.Read(buffer)
		if n > 0 {
			remaining := capture.limit - capture.count.Load()
			accepted := int64(n)
			if accepted > remaining {
				accepted = remaining
			}
			if accepted > 0 {
				capture.data = append(capture.data, buffer[:accepted]...)
				capture.count.Add(accepted)
			}
			if accepted < int64(n) {
				capture.overflow()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
