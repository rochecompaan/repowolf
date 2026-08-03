package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
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
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	waitForGone(t, pid)
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
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	waitForGone(t, pid)
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
