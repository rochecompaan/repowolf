package runner

import (
	"io"
	"sync"
	"sync/atomic"
)

// inputPipe serializes the configured prefix before stream writes and reserves
// every accepted write against one cumulative budget before touching the pipe.
type inputPipe struct {
	raw        io.WriteCloser
	prefix     []byte
	limit      int64
	overflow   func()
	prefixOnce sync.Once
	prefixDone chan struct{}
	prefixErr  error
	writeMu    sync.Mutex
	budgetMu   sync.Mutex
	reserved   int64
	count      atomic.Int64
}

func newInputPipe(raw io.WriteCloser, prefix []byte, limit int64, overflow func()) *inputPipe {
	return &inputPipe{
		raw:        raw,
		prefix:     prefix,
		limit:      limit,
		overflow:   overflow,
		prefixDone: make(chan struct{}),
		reserved:   int64(len(prefix)),
	}
}

func (input *inputPipe) sendPrefix() {
	input.prefixOnce.Do(func() {
		n, err := input.raw.Write(input.prefix)
		input.count.Add(int64(n))
		input.prefixErr = err
		close(input.prefixDone)
	})
}

func (input *inputPipe) Write(value []byte) (int, error) {
	if !input.reserve(int64(len(value))) {
		input.overflow()
		return 0, ErrInputLimit
	}
	input.sendPrefix()
	if input.prefixErr != nil {
		return 0, input.prefixErr
	}
	input.writeMu.Lock()
	defer input.writeMu.Unlock()
	n, err := input.raw.Write(value)
	input.count.Add(int64(n))
	return n, err
}

func (input *inputPipe) reserve(size int64) bool {
	input.budgetMu.Lock()
	defer input.budgetMu.Unlock()
	if size > input.limit-input.reserved {
		return false
	}
	input.reserved += size
	return true
}

func (input *inputPipe) Close() error {
	input.sendPrefix()
	return input.raw.Close()
}
