package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartStreamsInputAndOutputWithBoundedStderr(t *testing.T) {
	command := testCommand("stream-echo")
	command.Stdin = []byte("prefix-")
	process, err := (&Runner{}).Start(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := process.Stdin.Write([]byte("suffix")); err != nil {
		t.Fatal(err)
	}
	if err := process.Stdin.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(process.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	result, err := process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if string(stdout) != "prefix-suffix" {
		t.Fatalf("stdout = %q", stdout)
	}
	if result.StdinBytes != int64(len("prefix-suffix")) || result.StdoutBytes != int64(len(stdout)) {
		t.Fatalf("result = %#v", result)
	}
}

func TestStartRejectsCumulativeStreamInputOverflowAndCleansDescendant(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "input-overflow-descendant.pid")
	command := testCommand("spawn-no-read", pidFile)
	command.Stdin = []byte("pre")
	command.StdinLimit = 6
	process, err := (&Runner{}).Start(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	cwd := process.workingDirectory
	waitForFile(t, pidFile)
	if n, err := process.Stdin.Write([]byte("four")); n != 0 || !errors.Is(err, ErrInputLimit) {
		t.Fatalf("write = (%d, %v), want (0, input limit)", n, err)
	}
	result, err := process.Wait()
	if !errors.Is(err, ErrInputLimit) {
		t.Fatalf("wait error = %v, want input limit", err)
	}
	if result.StdinBytes > int64(command.StdinLimit) {
		t.Fatalf("stdin count exceeded limit: %#v", result)
	}
	assertProcessGone(t, readPID(t, pidFile))
	if _, err := os.Stat(cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private cwd remains: %v", err)
	}
}

func TestStartInputOverflowUnblocksInFlightWriter(t *testing.T) {
	command := testCommand("spawn-no-read", filepath.Join(t.TempDir(), "blocked-writer-descendant.pid"))
	command.StdinLimit = 1024 * 1024
	process, err := (&Runner{}).Start(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, err := process.Stdin.Write(make([]byte, command.StdinLimit))
		writeDone <- err
	}()
	waitForInputReservation(t, process.input, int64(command.StdinLimit))
	if _, err := process.Stdin.Write([]byte{1}); !errors.Is(err, ErrInputLimit) {
		t.Fatalf("overflow write error = %v", err)
	}
	select {
	case <-writeDone:
	case <-time.After(time.Second):
		t.Fatal("in-flight writer remained blocked")
	}
	if _, err := process.Wait(); !errors.Is(err, ErrInputLimit) {
		t.Fatalf("wait error = %v", err)
	}
}

func waitForInputReservation(t *testing.T, input *inputPipe, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		input.budgetMu.Lock()
		reserved := input.reserved
		input.budgetMu.Unlock()
		if reserved == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for input reservation")
}

func TestStartBlockedPrefixUnblocksOnCancellationAndTimeout(t *testing.T) {
	for _, test := range []struct {
		name   string
		cancel bool
	}{
		{name: "cancellation", cancel: true},
		{name: "timeout"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tempRoot := t.TempDir()
			command := testCommand("spawn-no-read", filepath.Join(t.TempDir(), "prefix-descendant.pid"))
			command.Stdin = make([]byte, 1024*1024)
			command.StdinLimit = len(command.Stdin)
			command.Timeout = 80 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			process, err := (&Runner{tempRoot: tempRoot}).Start(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			if test.cancel {
				cancel()
			}
			_, err = process.Wait()
			if test.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want canceled", err)
			}
			if !test.cancel && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want deadline", err)
			}
			assertEmptyDirectory(t, tempRoot)
		})
	}
}

func TestStartFailureRemovesPrivateCWD(t *testing.T) {
	tempRoot := t.TempDir()
	command := testCommand("stdin")
	command.Path = filepath.Join(tempRoot, "missing-provider")
	if _, err := (&Runner{tempRoot: tempRoot}).Start(context.Background(), command); !errors.Is(err, ErrStartFailed) {
		t.Fatalf("error = %v, want start failed", err)
	}
	assertEmptyDirectory(t, tempRoot)
}

func TestStartDiscardsBoundedStderrBeforeWaitReturns(t *testing.T) {
	process, err := (&Runner{}).Start(context.Background(), testCommand("failure", "raw-provider-secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, process.Stdout)
	if _, err := process.Wait(); !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("error = %v", err)
	}
	if process.stderr.data != nil {
		t.Fatalf("raw stderr retained after Wait: %q", process.stderr.data)
	}
}

func TestStartCancellationKillsDescendantsAndWaitReaps(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "stream-descendant.pid")
	ctx, cancel := context.WithCancel(context.Background())
	process, err := (&Runner{}).Start(ctx, testCommand("spawn", pidFile))
	if err != nil {
		t.Fatal(err)
	}
	waitForFile(t, pidFile)
	cancel()
	result, err := process.Wait()
	if !errors.Is(err, context.Canceled) || result.ExitCode == 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	pid := readPID(t, pidFile)
	assertProcessGone(t, pid)
}

func TestStartOutputLimitTerminatesProcessGroup(t *testing.T) {
	command := testCommand("stdout-overflow")
	command.StdoutLimit = 16
	process, err := (&Runner{}).Start(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(process.Stdout)
	if !errors.Is(readErr, ErrOutputLimit) {
		t.Fatalf("read error = %v", readErr)
	}
	result, waitErr := process.Wait()
	if !errors.Is(waitErr, ErrOutputLimit) {
		t.Fatalf("wait error = %v", waitErr)
	}
	if len(data) > command.StdoutLimit || result.StdoutBytes > int64(command.StdoutLimit) {
		t.Fatalf("data=%d result=%#v", len(data), result)
	}
}

func TestStartLeaderCompletionCleansDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "stream-completion-descendant.pid")
	process, err := (&Runner{}).Start(context.Background(), testCommand("spawn-success", pidFile))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, process.Stdout)
	result, err := process.Wait()
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	pid := readPID(t, pidFile)
	assertProcessGone(t, pid)
}

func TestStartTimeoutUnblocksStreamReadAndRemovesCWD(t *testing.T) {
	command := testCommand("sleep")
	command.Timeout = 60 * time.Millisecond
	process, err := (&Runner{}).Start(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	cwd := process.workingDirectory
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, process.Stdout)
		readDone <- err
	}()
	result, err := process.Wait()
	if !errors.Is(err, context.DeadlineExceeded) || !result.TimedOut {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("stdout reader remained blocked")
	}
	if _, err := os.Stat(cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private cwd remains: %v", err)
	}
}
