package providerhttp

import (
	"context"
	"errors"
	"io"
	"sync"
)

type boundedResponseBody struct {
	body          io.ReadCloser
	context       context.Context
	caller        context.Context
	callerShorter bool
	cancel        context.CancelFunc
	remaining     int64
	done          chan struct{}
	closeOnce     sync.Once
	finishOnce    sync.Once
	terminal      error
	finishErr     error
}

func newBoundedResponseBody(body io.ReadCloser, operation, caller context.Context, callerShorter bool, cancel context.CancelFunc, limit int64) *boundedResponseBody {
	bounded := &boundedResponseBody{body: body, context: operation, caller: caller, callerShorter: callerShorter, cancel: cancel, remaining: limit, done: make(chan struct{})}
	go func() {
		select {
		case <-operation.Done():
			bounded.closeUnderlying()
		case <-bounded.done:
		}
	}()
	return bounded
}

func (body *boundedResponseBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.terminal != nil {
		return 0, body.terminal
	}
	if body.remaining == 0 {
		var probe [1]byte
		n, err := body.body.Read(probe[:])
		if n > 0 {
			body.terminal = ErrResponseLimit
			body.finish()
			return 0, body.terminal
		}
		if err == nil {
			return 0, nil
		}
		body.terminal = body.mapError(err)
		body.finish()
		return 0, body.terminal
	}

	readSize := int64(len(buffer))
	if readSize > body.remaining+1 {
		readSize = body.remaining + 1
	}
	target := buffer
	if int64(len(buffer)) < readSize {
		target = make([]byte, readSize)
	} else {
		target = buffer[:readSize]
	}
	n, err := body.body.Read(target)
	inBudget := n
	if int64(inBudget) > body.remaining {
		inBudget = int(body.remaining)
	}
	if len(target) > len(buffer) && inBudget > 0 {
		copy(buffer, target[:inBudget])
	}
	body.remaining -= int64(inBudget)
	if n > inBudget {
		body.terminal = ErrResponseLimit
		body.finish()
		return inBudget, body.terminal
	}
	if err != nil {
		body.terminal = body.mapError(err)
		body.finish()
		if inBudget > 0 {
			return inBudget, nil
		}
		return 0, body.terminal
	}
	return inBudget, nil
}

func (body *boundedResponseBody) Close() error {
	return body.finish()
}

func (body *boundedResponseBody) finish() error {
	body.finishOnce.Do(func() {
		close(body.done)
		body.cancel()
		body.finishErr = body.closeUnderlying()
	})
	return body.finishErr
}

func (body *boundedResponseBody) closeUnderlying() error {
	var err error
	body.closeOnce.Do(func() { err = body.body.Close() })
	return err
}

func (body *boundedResponseBody) mapError(err error) error {
	if callerErr := body.caller.Err(); callerErr != nil && (body.callerShorter || errors.Is(callerErr, context.Canceled)) {
		return callerErr
	}
	if errors.Is(body.context.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	if contextErr := body.context.Err(); contextErr != nil {
		return contextErr
	}
	return err
}
