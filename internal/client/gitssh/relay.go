package gitssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const (
	maximumChunkBytes       = 64 << 10
	maximumGitBytes   int64 = 8 << 30
)

type gitClientStream interface {
	Send(*repowolfv1.GitFrame) error
	Recv() (*repowolfv1.GitFrame, error)
	CloseSend() error
}

type streamOpener func(context.Context, Operation) (gitClientStream, error)

func openerFor(client repowolfv1.GitServiceClient) streamOpener {
	return func(ctx context.Context, operation Operation) (gitClientStream, error) {
		switch operation {
		case UploadPack:
			return client.UploadPack(ctx)
		case ReceivePack:
			return client.ReceivePack(ctx)
		default:
			return nil, fmt.Errorf("unsupported Git operation")
		}
	}
}

type relayLimits struct {
	chunkBytes int
	maxBytes   int64
}

func relay(ctx context.Context, open streamOpener, request Request, stdin io.Reader, stdout io.Writer) (*repowolfv1.GitTerminal, error) {
	return relayWithLimits(ctx, open, request, stdin, stdout, relayLimits{chunkBytes: maximumChunkBytes, maxBytes: maximumGitBytes})
}

func relayWithLimits(ctx context.Context, open streamOpener, request Request, stdin io.Reader, stdout io.Writer, limits relayLimits) (*repowolfv1.GitTerminal, error) {
	if request.Repository == nil || stdin == nil || stdout == nil || limits.chunkBytes <= 0 || limits.chunkBytes > maximumChunkBytes || limits.maxBytes <= 0 {
		return nil, fmt.Errorf("invalid Git relay")
	}
	relayContext, cancel := context.WithCancelCause(ctx)
	stream, err := open(relayContext, request.Operation)
	if err != nil {
		cancel(err)
		return nil, err
	}

	startInput := make(chan struct{})
	stopInput := make(chan struct{})
	opened := make(chan error, 1)
	senderDone := make(chan error, 1)
	var startOnce, stopOnce sync.Once
	stopSender := func() {
		stopOnce.Do(func() {
			close(stopInput)
			closeReader(stdin)
		})
	}
	stopOnCancel := context.AfterFunc(relayContext, stopSender)
	go func() {
		sendErr := sendInput(relayContext, stream, request, stdin, limits, opened, startInput, stopInput)
		if sendErr != nil {
			cancel(sendErr)
		}
		senderDone <- sendErr
	}()
	defer func() {
		cancel(context.Canceled)
		stopSender()
		if !stopOnCancel() {
			<-relayContext.Done()
		}
		<-senderDone
	}()

	select {
	case err := <-opened:
		if err != nil {
			return nil, err
		}
	case <-relayContext.Done():
		return nil, context.Cause(relayContext)
	}

	var state serverFrameState
	var terminal *repowolfv1.GitTerminal
	var outputBytes int64
	for {
		frame, receiveErr := stream.Recv()
		if receiveErr != nil {
			if errors.Is(receiveErr, io.EOF) && terminal != nil {
				return terminal, nil
			}
			select {
			case sendErr := <-senderDone:
				senderDone <- sendErr
				if sendErr != nil {
					return nil, sendErr
				}
			default:
			}
			return nil, receiveErr
		}
		isTerminal, acceptErr := state.Accept(frame)
		if acceptErr != nil {
			return nil, acceptErr
		}
		if isTerminal {
			terminal = frame.GetTerminal()
			stopSender()
			continue
		}
		startOnce.Do(func() { close(startInput) })
		data := frame.GetData().GetData()
		if int64(len(data)) > limits.maxBytes-outputBytes {
			return nil, fmt.Errorf("Git output exceeds local limit")
		}
		if err := writeAll(stdout, data); err != nil {
			return nil, err
		}
		outputBytes += int64(len(data))
	}
}

func sendInput(ctx context.Context, stream gitClientStream, request Request, input io.Reader, limits relayLimits, opened chan<- error, start, stop <-chan struct{}) error {
	err := stream.Send(&repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Open{Open: &repowolfv1.GitOpen{Repository: request.Repository}}})
	opened <- err
	if err != nil {
		return err
	}
	select {
	case <-start:
	case <-stop:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}

	buffer := make([]byte, limits.chunkBytes)
	var total int64
	for {
		count, readErr := input.Read(buffer)
		if count > 0 {
			if int64(count) > limits.maxBytes-total {
				return fmt.Errorf("Git input exceeds local limit")
			}
			select {
			case <-stop:
				return nil
			case <-ctx.Done():
				return context.Cause(ctx)
			default:
			}
			frame := &repowolfv1.GitFrame{Payload: &repowolfv1.GitFrame_Data{Data: &repowolfv1.GitData{Data: buffer[:count]}}}
			if err := stream.Send(frame); err != nil {
				return err
			}
			total += int64(count)
		}
		if errors.Is(readErr, io.EOF) {
			return stream.CloseSend()
		}
		if readErr != nil {
			return readErr
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
}

func closeReader(reader io.Reader) {
	if closer, ok := reader.(io.Closer); ok {
		_ = closer.Close()
	}
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		count, err := writer.Write(data)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		data = data[count:]
	}
	return nil
}
