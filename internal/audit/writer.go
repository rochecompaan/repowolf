package audit

import (
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

var ErrSink = errors.New("audit sink unavailable")

// Sink accepts terminal audit events.
type Sink interface {
	Write(Event) error
}

// Writer serializes JSON Lines writes so concurrent records cannot interleave.
type Writer struct {
	mu      sync.Mutex
	output  io.Writer
	encoder *json.Encoder
}

func NewWriter(output io.Writer) *Writer {
	writer := &Writer{output: output}
	if output != nil {
		writer.encoder = json.NewEncoder(output)
		writer.encoder.SetEscapeHTML(true)
	}
	return writer
}

// Write emits exactly one complete JSON object followed by a newline.
func (writer *Writer) Write(event Event) error {
	if writer == nil {
		return ErrSink
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.encoder == nil {
		return ErrSink
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := writer.encoder.Encode(event); err != nil {
		return ErrSink
	}
	return nil
}

// Flush flushes sinks that provide an explicit flush operation.
func (writer *Writer) Flush() error {
	if writer == nil {
		return ErrSink
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.flushLocked()
}

// FlushIfIdle flushes only when no write is in progress. Forced shutdown uses
// it to avoid waiting forever on a blocked output sink.
func (writer *Writer) FlushIfIdle() error {
	if writer == nil {
		return ErrSink
	}
	if _, ok := writer.output.(interface{ Flush() error }); !ok {
		return nil
	}
	if !writer.mu.TryLock() {
		return ErrSink
	}
	defer writer.mu.Unlock()
	return writer.flushLocked()
}

func (writer *Writer) flushLocked() error {
	flusher, ok := writer.output.(interface{ Flush() error })
	if !ok {
		return nil
	}
	if err := flusher.Flush(); err != nil {
		return ErrSink
	}
	return nil
}
