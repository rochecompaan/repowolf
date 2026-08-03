package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
)

// Runner owns provider process creation. Its zero value is ready for use.
type Runner struct {
	tempRoot string
}

// Process is a started provider stream. Callers must close Stdin, drain Stdout,
// and call Wait. Context cancellation remains active until Wait returns.
type Process struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser

	workingDirectory string
	authority        *processAuthority
	input            *inputPipe
	stdout           *limitedReader
	stderr           *stderrCapture
	stderrDone       chan error
	exitObserved     chan error
	exited           chan struct{}
	lifecycleDone    chan struct{}
	cancel           context.CancelFunc

	outputExceeded atomic.Bool
	reasonMu       sync.Mutex
	interruption   error
	waitOnce       sync.Once
	result         Result
	waitErr        error
}

// Start launches a generated command in a private working directory and
// process group. It never searches PATH or invokes a shell.
func (runner *Runner) Start(parent context.Context, requested Command) (*Process, error) {
	command, err := cloneAndValidate(requested)
	if err != nil {
		return nil, err
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	workingDirectory, err := os.MkdirTemp(runner.tempRoot, "repowolf-provider-")
	if err != nil {
		return nil, ErrStartFailed
	}
	cleanup := func() { _ = os.RemoveAll(workingDirectory) }

	operation, cancel := context.WithTimeout(parent, command.Timeout)
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Dir = workingDirectory
	cmd.Env = command.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cleanup()
		return nil, ErrStartFailed
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		cleanup()
		return nil, ErrStartFailed
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		cleanup()
		return nil, ErrStartFailed
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		cleanup()
		return nil, ErrStartFailed
	}

	process := newProcess(command, workingDirectory, cmd, stdin, stdout, stderr, cancel)
	process.monitor(operation)
	return process, nil
}

func newProcess(command Command, workingDirectory string, cmd *exec.Cmd, stdin io.WriteCloser, stdout, stderr io.ReadCloser, cancel context.CancelFunc) *Process {
	process := &Process{
		workingDirectory: workingDirectory,
		authority:        newProcessAuthority(cmd),
		stderrDone:       make(chan error, 1),
		exitObserved:     make(chan error, 1),
		exited:           make(chan struct{}),
		lifecycleDone:    make(chan struct{}),
		cancel:           cancel,
	}
	process.input = newInputPipe(stdin, command.Stdin)
	process.stdout = &limitedReader{raw: stdout, limit: int64(command.StdoutLimit), overflow: process.limitExceeded}
	process.stderr = &stderrCapture{raw: stderr, limit: int64(command.StderrLimit), overflow: process.limitExceeded}
	process.Stdin = process.input
	process.Stdout = process.stdout
	return process
}

func (process *Process) monitor(operation context.Context) {
	go func() {
		err := waitWithoutReap(process.authority.pgid)
		process.authority.terminate()
		close(process.exited)
		process.exitObserved <- err
	}()
	go func() { process.stderrDone <- process.stderr.drain() }()
	go process.input.sendPrefix()
	go func() {
		select {
		case <-operation.Done():
			if process.authority.terminate() {
				process.setInterruption(operation.Err())
				_ = process.input.raw.Close()
			}
		case <-process.exited:
		case <-process.lifecycleDone:
		}
	}()
}

func (process *Process) limitExceeded() {
	process.outputExceeded.Store(true)
	if process.authority.terminate() {
		_ = process.input.raw.Close()
	}
}

func (process *Process) setInterruption(err error) {
	process.reasonMu.Lock()
	process.interruption = err
	process.reasonMu.Unlock()
}

// Wait fully reaps the process group and removes its private directory.
func (process *Process) Wait() (Result, error) {
	process.waitOnce.Do(process.wait)
	result := process.result
	result.Stdout = append([]byte(nil), result.Stdout...)
	return result, process.waitErr
}

func (process *Process) wait() {
	observationErr := <-process.exitObserved
	waitErr := process.authority.retireAndReap()
	_ = process.input.raw.Close()
	<-process.input.prefixDone
	stderrErr := <-process.stderrDone
	process.stderr.data = nil // discard raw provider diagnostics before returning
	process.cancel()
	close(process.lifecycleDone)
	cleanupErr := os.RemoveAll(process.workingDirectory)

	process.result = Result{
		ExitCode:    exitCode(waitErr),
		TimedOut:    process.wasTimedOut(),
		StdinBytes:  process.input.count.Load(),
		StdoutBytes: process.stdout.count.Load(),
		StderrBytes: process.stderr.count.Load(),
	}
	process.waitErr = process.classifyWaitError(observationErr, waitErr, stderrErr, cleanupErr)
}

func (process *Process) wasTimedOut() bool {
	process.reasonMu.Lock()
	defer process.reasonMu.Unlock()
	return errors.Is(process.interruption, context.DeadlineExceeded)
}

func (process *Process) classifyWaitError(observationErr, waitErr, stderrErr, cleanupErr error) error {
	if process.outputExceeded.Load() {
		return ErrOutputLimit
	}
	process.reasonMu.Lock()
	interruption := process.interruption
	process.reasonMu.Unlock()
	if errors.Is(interruption, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(interruption, context.Canceled) {
		return context.Canceled
	}
	if observationErr != nil || (stderrErr != nil && !errors.Is(stderrErr, os.ErrClosed)) {
		return ErrCommandFailed
	}
	if cleanupErr != nil {
		return ErrCleanupFailed
	}
	if waitErr != nil {
		return ErrCommandFailed
	}
	return nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() > 0 {
		return exitError.ExitCode()
	}
	return 1
}

// inputPipe serializes the configured prefix before stream writes.
type inputPipe struct {
	raw        io.WriteCloser
	prefix     []byte
	prefixOnce sync.Once
	prefixDone chan struct{}
	prefixErr  error
	count      atomic.Int64
}

func newInputPipe(raw io.WriteCloser, prefix []byte) *inputPipe {
	return &inputPipe{raw: raw, prefix: prefix, prefixDone: make(chan struct{})}
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
	input.sendPrefix()
	if input.prefixErr != nil {
		return 0, input.prefixErr
	}
	n, err := input.raw.Write(value)
	input.count.Add(int64(n))
	return n, err
}

func (input *inputPipe) Close() error {
	input.sendPrefix()
	return input.raw.Close()
}

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
