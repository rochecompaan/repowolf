package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
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

	waitOnce sync.Once
	result   Result
	waitErr  error
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
	processTableMu.Lock()
	contextErr := parent.Err()
	if contextErr == nil {
		contextErr = operation.Err()
	}
	if contextErr == nil {
		err = cmd.Start()
	}
	processTableMu.Unlock()
	if contextErr != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		cleanup()
		return nil, contextErr
	}
	if err != nil {
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
	process.input = newInputPipe(stdin, command.Stdin, int64(command.StdinLimit), process.inputLimitExceeded)
	process.stdout = &limitedReader{raw: stdout, limit: int64(command.StdoutLimit), overflow: process.limitExceeded}
	process.stderr = &stderrCapture{raw: stderr, limit: int64(command.StderrLimit), overflow: process.limitExceeded}
	process.Stdin = process.input
	process.Stdout = process.stdout
	return process
}

func (process *Process) monitor(operation context.Context) {
	go func() {
		err := waitWithoutReap(process.authority.pgid)
		process.authority.terminate(nil)
		close(process.exited)
		process.exitObserved <- err
	}()
	go func() { process.stderrDone <- process.stderr.drain() }()
	go process.input.sendPrefix()
	go func() {
		select {
		case <-operation.Done():
			if process.authority.terminate(operation.Err()) {
				_ = process.input.raw.Close()
			}
		case <-process.exited:
		case <-process.lifecycleDone:
		}
	}()
}

func (process *Process) limitExceeded() {
	if process.authority.terminate(ErrOutputLimit) {
		_ = process.input.raw.Close()
	}
}

func (process *Process) inputLimitExceeded() {
	if process.authority.terminate(ErrInputLimit) {
		_ = process.input.raw.Close()
	}
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
	waitErr, groupErr := process.authority.retireAndReap()
	_ = process.input.raw.Close()
	<-process.input.prefixDone
	stderrErr := <-process.stderrDone
	process.stderr.data = nil // discard raw provider diagnostics before returning
	process.cancel()
	close(process.lifecycleDone)
	cleanupErr := os.RemoveAll(process.workingDirectory)

	process.result = Result{
		ExitCode:    exitCode(waitErr),
		TimedOut:    errors.Is(process.authority.terminationReason(), context.DeadlineExceeded),
		StdinBytes:  process.input.count.Load(),
		StdoutBytes: process.stdout.count.Load(),
		StderrBytes: process.stderr.count.Load(),
	}
	process.waitErr = process.classifyWaitError(observationErr, waitErr, groupErr, stderrErr, cleanupErr)
}

func (process *Process) classifyWaitError(observationErr, waitErr, groupErr, stderrErr, cleanupErr error) error {
	interruption := process.authority.terminationReason()
	var primary error
	if errors.Is(interruption, ErrInputLimit) {
		primary = ErrInputLimit
	} else if errors.Is(interruption, ErrOutputLimit) {
		primary = ErrOutputLimit
	} else if errors.Is(interruption, context.DeadlineExceeded) {
		primary = context.DeadlineExceeded
	} else if errors.Is(interruption, context.Canceled) {
		primary = context.Canceled
	} else if observationErr != nil || (stderrErr != nil && !errors.Is(stderrErr, os.ErrClosed)) || waitErr != nil {
		primary = ErrCommandFailed
	}
	if groupErr != nil || cleanupErr != nil {
		return errors.Join(primary, ErrCleanupFailed)
	}
	return primary
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
