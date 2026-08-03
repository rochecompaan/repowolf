package gitservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

var errSendBlocked = errors.New("git stream send unavailable")

type sendRequest struct {
	ctx    context.Context
	frame  *repowolfv1.GitFrame
	result chan error
}

// sendPump is the sole owner of stream.Send. Callers can abandon backpressured
// delivery when their session context expires; returning the RPC then releases
// the underlying gRPC Send.
type sendPump struct {
	ctx      context.Context
	cancel   context.CancelCauseFunc
	stream   gitStream
	requests chan sendRequest
	done     chan struct{}
	busy     atomic.Bool

	mu  sync.Mutex
	err error
}

func newSendPump(stream gitStream) *sendPump {
	ctx, cancel := context.WithCancelCause(stream.Context())
	pump := &sendPump{
		ctx: ctx, cancel: cancel, stream: stream,
		requests: make(chan sendRequest), done: make(chan struct{}),
	}
	go pump.run()
	return pump
}

func (pump *sendPump) run() {
	defer close(pump.done)
	for {
		if pump.ctx.Err() != nil {
			return
		}
		select {
		case <-pump.ctx.Done():
			return
		case request := <-pump.requests:
			if request.ctx.Err() != nil {
				request.result <- context.Cause(request.ctx)
				continue
			}
			pump.busy.Store(true)
			err := pump.stream.Send(request.frame)
			pump.busy.Store(false)
			request.result <- err
			if err != nil {
				pump.setError(err)
				pump.cancel(err)
				return
			}
		}
	}
}

func (pump *sendPump) Send(ctx context.Context, frame *repowolfv1.GitFrame) error {
	request := sendRequest{ctx: ctx, frame: frame, result: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-pump.done:
		return pump.failure()
	case pump.requests <- request:
	}
	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-pump.done:
		return pump.failure()
	}
}

func (pump *sendPump) SendFinal(ctx context.Context, timeout time.Duration, frame *repowolfv1.GitFrame) error {
	if pump.busy.Load() {
		return errSendBlocked
	}
	deliveryContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pump.Send(deliveryContext, frame)
}

func (pump *sendPump) Stop() {
	pump.cancel(context.Canceled)
}

func (pump *sendPump) setError(err error) {
	pump.mu.Lock()
	pump.err = err
	pump.mu.Unlock()
}

func (pump *sendPump) failure() error {
	pump.mu.Lock()
	defer pump.mu.Unlock()
	if pump.err != nil {
		return pump.err
	}
	return context.Cause(pump.ctx)
}
