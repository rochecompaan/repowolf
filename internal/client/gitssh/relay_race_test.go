package gitssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func TestRelayPrefersPreexistingSenderFailureOverCompletedTerminal(t *testing.T) {
	senderFailure := errors.New("synchronized sender failure")
	for _, test := range []struct {
		name   string
		input  io.Reader
		stream *terminalAfterSenderFailureStream
		want   error
	}{
		{
			name:   "stdin",
			input:  errorReader{err: senderFailure},
			stream: &terminalAfterSenderFailureStream{failure: senderFailure},
			want:   senderFailure,
		},
		{
			name:   "stdin no progress",
			input:  zeroReader{},
			stream: &terminalAfterSenderFailureStream{},
			want:   io.ErrNoProgress,
		},
		{
			name:  "Send",
			input: bytes.NewReader([]byte("pack data")),
			stream: &terminalAfterSenderFailureStream{
				failure:  senderFailure,
				failSend: true,
			},
			want: senderFailure,
		},
		{
			name:  "CloseSend",
			input: bytes.NewReader(nil),
			stream: &terminalAfterSenderFailureStream{
				failure:       senderFailure,
				failCloseSend: true,
			},
			want: senderFailure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			opener := func(ctx context.Context, _ Operation) (gitClientStream, error) {
				test.stream.ctx = ctx
				return test.stream, nil
			}
			terminal, err := relay(context.Background(), opener, testRequest(UploadPack), test.input, io.Discard)
			if terminal != nil || !errors.Is(err, test.want) {
				t.Fatalf("relay() terminal=%v err=%v, want sender failure", terminal, err)
			}
		})
	}
}

func TestRelayIgnoresInputCloseCausedByNormalServerTerminal(t *testing.T) {
	input := newBlockingCloseReader()
	stream := &normalTerminalStream{inputStarted: input.started}
	opener := func(ctx context.Context, _ Operation) (gitClientStream, error) {
		stream.ctx = ctx
		return stream, nil
	}
	terminal, err := relay(context.Background(), opener, testRequest(UploadPack), input, io.Discard)
	if err != nil || terminal.GetCategory() != repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED {
		t.Fatalf("relay() terminal=%v err=%v", terminal, err)
	}
}

type terminalAfterSenderFailureStream struct {
	ctx           context.Context
	failure       error
	failSend      bool
	failCloseSend bool
	sends         int
	receives      int
}

func (stream *terminalAfterSenderFailureStream) Send(*repowolfv1.GitFrame) error {
	stream.sends++
	if stream.sends > 1 && stream.failSend {
		return stream.failure
	}
	return nil
}

func (stream *terminalAfterSenderFailureStream) CloseSend() error {
	if stream.failCloseSend {
		return stream.failure
	}
	return nil
}

func (stream *terminalAfterSenderFailureStream) Recv() (*repowolfv1.GitFrame, error) {
	stream.receives++
	switch stream.receives {
	case 1:
		return dataFrame([]byte("advertisement")), nil
	case 2:
		<-stream.ctx.Done()
		return terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0), nil
	default:
		return nil, io.EOF
	}
}

type normalTerminalStream struct {
	ctx          context.Context
	inputStarted <-chan struct{}
	receives     int
}

func (*normalTerminalStream) Send(*repowolfv1.GitFrame) error { return nil }
func (*normalTerminalStream) CloseSend() error                { return nil }

func (stream *normalTerminalStream) Recv() (*repowolfv1.GitFrame, error) {
	stream.receives++
	switch stream.receives {
	case 1:
		return dataFrame([]byte("advertisement")), nil
	case 2:
		<-stream.inputStarted
		return terminalFrame(repowolfv1.GitTerminalCategory_GIT_TERMINAL_CATEGORY_COMPLETED, 0), nil
	default:
		return nil, io.EOF
	}
}

type errorReader struct{ err error }

func (reader errorReader) Read([]byte) (int, error) { return 0, reader.err }

type zeroReader struct{}

func (zeroReader) Read([]byte) (int, error) { return 0, nil }

type blockingCloseReader struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newBlockingCloseReader() *blockingCloseReader {
	return &blockingCloseReader{started: make(chan struct{}), closed: make(chan struct{})}
}

func (reader *blockingCloseReader) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	<-reader.closed
	return 0, io.ErrClosedPipe
}

func (reader *blockingCloseReader) Close() error {
	select {
	case <-reader.closed:
	default:
		close(reader.closed)
	}
	return nil
}
