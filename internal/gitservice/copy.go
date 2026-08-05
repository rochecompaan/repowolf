package gitservice

import (
	"context"
	"errors"
	"io"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

type gitStream interface {
	Context() context.Context
	Recv() (*repowolfv1.GitFrame, error)
	Send(*repowolfv1.GitFrame) error
}

type frameResult struct {
	frame *repowolfv1.GitFrame
	err   error
}

func receiveWithTimeout(ctx context.Context, timeout time.Duration, receive func() (*repowolfv1.GitFrame, error)) (*repowolfv1.GitFrame, error) {
	result := make(chan frameResult, 1)
	go func() {
		frame, err := receive()
		result <- frameResult{frame: frame, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, context.DeadlineExceeded
	case received := <-result:
		return received.frame, received.err
	}
}

func copyFramesToInput(stream gitStream, input io.WriteCloser, state *clientFrameState, activity chan<- struct{}) (int64, error) {
	defer input.Close()
	var total int64
	for {
		frame, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return total, state.Close()
		}
		if err != nil {
			return total, err
		}
		if err := state.Accept(frame); err != nil {
			return total, err
		}
		data := frame.GetData().GetData()
		n, err := input.Write(data)
		total += int64(n)
		noteActivity(activity)
		if err != nil {
			return total, err
		}
		if n != len(data) {
			return total, io.ErrShortWrite
		}
	}
}

func copyOutputToFrames(ctx context.Context, output io.Reader, sender *sendPump, chunkSize int, activity chan<- struct{}) (int64, error) {
	if chunkSize <= 0 || chunkSize > maxChunkBytes {
		return 0, errChunkLimit
	}
	buffer := make([]byte, chunkSize)
	var total int64
	for {
		n, err := output.Read(buffer)
		if n > 0 {
			data := append([]byte(nil), buffer[:n]...)
			total += int64(n)
			if sendErr := sender.Send(ctx, dataFrameForWire(data)); sendErr != nil {
				return total, sendErr
			}
			noteActivity(activity)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
}

func dataFrameForWire(data []byte) *repowolfv1.GitFrame {
	return &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Data{Data: &repowolfv1.GitData{Data: data}}}
}

func noteActivity(activity chan<- struct{}) {
	select {
	case activity <- struct{}{}:
	default:
	}
}

func watchIdle(ctx context.Context, timeout time.Duration, activity <-chan struct{}, cancel context.CancelCauseFunc) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		case <-timer.C:
			cancel(context.DeadlineExceeded)
			return
		}
	}
}
