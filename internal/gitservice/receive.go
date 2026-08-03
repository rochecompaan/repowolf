package gitservice

import (
	"context"
	"errors"
	"io"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/audit"
	"github.com/rochecompaan/repowolf/internal/config"
	"github.com/rochecompaan/repowolf/internal/gitproto"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"github.com/rochecompaan/repowolf/internal/runner"
)

// ReceivePack relays a bounded receive-pack session after validating its update prefix.
func (service *Service) ReceivePack(stream repowolfv1.GitService_ReceivePackServer) error {
	return service.receivePack(stream)
}

func (service *Service) receivePack(stream gitStream) error {
	started := time.Now()
	state := &clientFrameState{}
	first, err := receiveWithTimeout(stream.Context(), service.options.Limits.InitialStreamTimeout, stream.Recv)
	if err == nil {
		err = state.Accept(first)
	}
	if err != nil {
		return service.finish(stream, policy.ResolvedRepository{}, "git.receive-pack", started, 0, 0, nil, 0, err)
	}
	repository, command, err := service.receiveCommand(stream.Context(), first.GetOpen())
	if err != nil {
		return service.finish(stream, repository, "git.receive-pack", started, 0, 0, nil, 0, err)
	}
	if err := service.writeAudit(stream.Context(), repository, "git.receive-pack", audit.OutcomeAccepted, "", started, 0, 0, nil, 0); err != nil {
		return service.finish(stream, repository, "git.receive-pack", started, 0, 0, nil, 0, rpcstatus.ErrServiceUnavailable)
	}

	processContext, cancel := context.WithCancelCause(stream.Context())
	process, err := service.options.Runner.Start(processContext, command)
	if err != nil {
		cancel(err)
		return service.finish(stream, repository, "git.receive-pack", started, 0, 0, nil, 0, err)
	}
	activity := make(chan struct{}, 1)
	go watchIdle(processContext, service.options.Limits.IdleStreamTimeout, activity, cancel)
	advertisement, err := gitproto.ParseAdvertisement(&activityReader{reader: process.Stdout, activity: activity}, service.options.Limits.MaxPushPrefixBytes)
	if err != nil {
		result, cleanupErr := stopProcess(cancel, process, err)
		return service.finish(stream, repository, "git.receive-pack", started, 0, result.StdoutBytes, nil, 0, firstError(context.Cause(processContext), err, cleanupErr))
	}
	if err := sendBytes(stream, advertisement.Raw, service.options.Limits.MaxStreamChunkBytes); err != nil {
		_, _ = stopProcess(cancel, process, err)
		return err
	}
	noteActivity(activity)

	client := &clientDataReader{
		stream: stream, state: state, timeout: service.options.Limits.IdleStreamTimeout,
		maxBytes: service.options.Limits.MaxGitBytesPerDirection, activity: activity,
	}
	parsed, err := gitproto.ParseReceivePack(client, gitproto.ReceiveOptions{
		MaxBytes: service.options.Limits.MaxPushPrefixBytes, MaxCommands: repository.Repository.Git.MaxRefUpdates,
		Policy: repository.Repository.Git, AdvertisedCaps: advertisement.Capabilities,
	})
	if err != nil {
		result, cleanupErr := stopProcess(cancel, process, err)
		requestErr := classifyReceiveRequestError(err)
		// The request prefix was never written to the provider, so audit zero
		// provider input rather than the bytes buffered for validation.
		return service.finish(stream, repository, "git.receive-pack", started, 0, result.StdoutBytes, nil, 0, firstError(requestErr, cleanupErr))
	}
	refs := make([]string, len(parsed.Updates))
	for index, update := range parsed.Updates {
		refs[index] = update.Ref
	}

	outputDone := make(chan struct {
		bytes int64
		err   error
	}, 1)
	go func() {
		count, copyErr := copyOutputToFrames(process.Stdout, stream, service.options.Limits.MaxStreamChunkBytes, activity)
		if copyErr != nil {
			cancel(copyErr)
		}
		outputDone <- struct {
			bytes int64
			err   error
		}{count, copyErr}
	}()
	inputBytes, inputErr := writeValidatedInput(process.Stdin, parsed.Prefix, client)
	if inputErr != nil {
		cancel(inputErr)
	}
	output := <-outputDone
	result, waitErr := process.Wait()
	cause := firstError(inputErr, context.Cause(processContext), output.err, waitErr)
	cancel(cause)
	return service.finish(stream, repository, "git.receive-pack", started, inputBytes, int64(len(advertisement.Raw))+output.bytes, refs, len(parsed.Updates), causeWithExit(cause, result.ExitCode))
}

func (service *Service) receiveCommand(ctx context.Context, open *repowolfv1.GitOpen) (policy.ResolvedRepository, runner.Command, error) {
	_, readRepository, err := service.command(ctx, open, config.GitRead, "git-receive-pack")
	if err != nil {
		return readRepository, runner.Command{}, err
	}
	command, writeRepository, err := service.command(ctx, open, config.GitWrite, "git-receive-pack")
	if err != nil || readRepository.ID != writeRepository.ID {
		if err == nil {
			err = policy.ErrDenied
		}
		return readRepository, runner.Command{}, err
	}
	return writeRepository, command, nil
}

func classifyReceiveRequestError(err error) error {
	switch {
	case errors.Is(err, policy.ErrRefPolicy), errors.Is(err, errInvalidFrame), errors.Is(err, errChunkLimit),
		errors.Is(err, runner.ErrInputLimit), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return errors.Join(rpcstatus.ErrInvalidArgument, err)
	}
}

func writeValidatedInput(input io.WriteCloser, prefix []byte, remaining io.Reader) (int64, error) {
	defer input.Close()
	n, err := input.Write(prefix)
	total := int64(n)
	if err != nil {
		return total, err
	}
	if n != len(prefix) {
		return total, io.ErrShortWrite
	}
	copied, err := io.Copy(input, remaining)
	return total + copied, err
}

func stopProcess(cancel context.CancelCauseFunc, process *runner.Process, reason error) (runner.Result, error) {
	cancel(reason)
	_ = process.Stdin.Close()
	_, _ = io.Copy(io.Discard, process.Stdout)
	return process.Wait()
}

func sendBytes(stream gitStream, data []byte, chunkSize int) error {
	if chunkSize <= 0 || chunkSize > maxChunkBytes {
		return errChunkLimit
	}
	for len(data) > 0 {
		size := len(data)
		if size > chunkSize {
			size = chunkSize
		}
		if err := stream.Send(dataFrameForWire(append([]byte(nil), data[:size]...))); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

type activityReader struct {
	reader   io.Reader
	activity chan<- struct{}
}

func (reader *activityReader) Read(data []byte) (int, error) {
	n, err := reader.reader.Read(data)
	if n > 0 {
		noteActivity(reader.activity)
	}
	return n, err
}

type clientDataReader struct {
	stream   gitStream
	state    *clientFrameState
	timeout  time.Duration
	pending  []byte
	total    int64
	maxBytes int64
	activity chan<- struct{}
}

func (reader *clientDataReader) Read(destination []byte) (int, error) {
	for len(reader.pending) == 0 {
		frame, err := receiveWithTimeout(reader.stream.Context(), reader.timeout, reader.stream.Recv)
		if errors.Is(err, io.EOF) {
			if closeErr := reader.state.Close(); closeErr != nil {
				return 0, closeErr
			}
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		if err := reader.state.Accept(frame); err != nil {
			return 0, err
		}
		data := frame.GetData().GetData()
		if int64(len(data)) > reader.maxBytes-reader.total {
			return 0, runner.ErrInputLimit
		}
		reader.total += int64(len(data))
		reader.pending = data
		noteActivity(reader.activity)
	}
	n := copy(destination, reader.pending)
	reader.pending = reader.pending[n:]
	return n, nil
}
