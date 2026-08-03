package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCallUsesPinnedPathLiteralArgumentsExactEnvironmentAndPrivateCWD(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	argument := "; touch " + marker
	providerAuth := "provider-auth=preserved\nbyte-for-byte"
	result, err := (&Runner{}).Call(context.Background(), testCommand("inspect", argument, providerAuth))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args []string `json:"args"`
		Auth string   `json:"auth"`
		CWD  string   `json:"cwd"`
		Mode uint32   `json:"mode"`
		EOF  bool     `json:"eof"`
	}
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatalf("decode output: %v (%q)", err, result.Stdout)
	}
	if len(got.Args) != 1 || got.Args[0] != argument || got.Auth != providerAuth || !got.EOF {
		t.Fatalf("helper observation = %#v", got)
	}
	if got.Mode != 0o700 {
		t.Errorf("cwd mode = %#o, want 0700", got.Mode)
	}
	if _, err := os.Stat(got.CWD); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("private cwd remains after Call: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("argument was shell-interpreted: %v", err)
	}
	if result.StdinBytes != 0 || result.StdoutBytes != int64(len(result.Stdout)) || result.StderrBytes != 0 {
		t.Errorf("counts = stdin:%d stdout:%d stderr:%d", result.StdinBytes, result.StdoutBytes, result.StderrBytes)
	}
}

func TestCallDoesNotInheritEnvironmentWhenExactEnvironmentIsEmpty(t *testing.T) {
	envPath, err := exec.LookPath("env")
	if err != nil {
		t.Fatal(err)
	}
	envPath, err = filepath.Abs(envPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPOWOLF_MUST_NOT_BE_INHERITED", "service-secret")
	command := Command{
		Path: envPath, Env: []string{}, Timeout: 3 * time.Second,
		StdinLimit: 4096, StdoutLimit: 4096, StderrLimit: 4096,
	}
	result, err := (&Runner{}).Call(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("child inherited environment: %q", result.Stdout)
	}
}

func TestCallSendsBoundedInput(t *testing.T) {
	command := testCommand("stdin")
	command.Stdin = []byte("request-body")
	result, err := (&Runner{}).Call(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "request-body" || result.StdinBytes != int64(len(command.Stdin)) {
		t.Fatalf("result = %#v", result)
	}
}

func TestCallRejectsUnaryInputOverBudgetWithoutCreatingCWD(t *testing.T) {
	tempRoot := t.TempDir()
	command := testCommand("stdin")
	command.StdinLimit = 4
	command.Stdin = []byte("12345")

	result, err := (&Runner{tempRoot: tempRoot}).Call(context.Background(), command)
	if !errors.Is(err, ErrInputLimit) {
		t.Fatalf("error = %v, want input limit", err)
	}
	if result.StdinBytes != 0 {
		t.Fatalf("stdin bytes = %d, want 0", result.StdinBytes)
	}
	assertEmptyDirectory(t, tempRoot)
}

func TestCallBoundsStdoutAndStderr(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "stdout", mode: "stdout-overflow"},
		{name: "stderr", mode: "stderr-overflow"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := testCommand(test.mode)
			command.StdoutLimit = 16
			command.StderrLimit = 16
			result, err := (&Runner{}).Call(context.Background(), command)
			if !errors.Is(err, ErrOutputLimit) {
				t.Fatalf("error = %v, want output limit", err)
			}
			if len(result.Stdout) > command.StdoutLimit || result.StdoutBytes > int64(command.StdoutLimit) || result.StderrBytes > int64(command.StderrLimit) {
				t.Fatalf("unbounded result = %#v", result)
			}
		})
	}
}

func TestCallReturnsSafeFailureContextWithoutStderr(t *testing.T) {
	const secret = "provider-stderr-secret"
	command := testCommand("failure", secret)
	result, err := (&Runner{}).Call(context.Background(), command)
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("error = %v", err)
	}
	if result.ExitCode != 23 || string(result.Stdout) != "partial-output" || result.StderrBytes != int64(len(secret)) {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(fmt.Sprintf("%#v", result), secret) {
		t.Fatalf("raw stderr leaked: error=%q result=%#v", err, result)
	}
}

func TestCallTimeoutAndCancellationKillDescendants(t *testing.T) {
	for _, test := range []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		timeout time.Duration
		timed   bool
	}{
		{
			name: "timeout",
			context: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			timeout: 80 * time.Millisecond,
			timed:   true,
		},
		{
			name: "cancellation",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			timeout: 3 * time.Second,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "descendant.pid")
			command := testCommand("spawn", pidFile)
			command.Timeout = test.timeout
			ctx, cancel := test.context()
			defer cancel()
			done := make(chan struct {
				result Result
				err    error
			}, 1)
			go func() {
				result, err := (&Runner{}).Call(ctx, command)
				done <- struct {
					result Result
					err    error
				}{result, err}
			}()
			waitForFile(t, pidFile)
			if !test.timed {
				cancel()
			}
			got := <-done
			if got.err == nil || got.result.TimedOut != test.timed {
				t.Fatalf("result=%#v error=%v", got.result, got.err)
			}
			assertProcessGone(t, readPID(t, pidFile))
		})
	}
}

func TestCallCompletionCleansDescendants(t *testing.T) {
	for _, mode := range []string{"spawn-success", "spawn-failure"} {
		t.Run(mode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "descendant.pid")
			result, err := (&Runner{}).Call(context.Background(), testCommand(mode, pidFile))
			if mode == "spawn-success" && err != nil {
				t.Fatal(err)
			}
			if mode == "spawn-failure" && (!errors.Is(err, ErrCommandFailed) || result.ExitCode != 23) {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			assertProcessGone(t, readPID(t, pidFile))
		})
	}
}

func TestCallReapsAdoptedGroupDescendantsBeforeReturn(t *testing.T) {
	for _, scenario := range []string{"natural", "cancellation", "timeout", "stdout-overflow", "stderr-overflow", "concurrent"} {
		t.Run(scenario, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestSubreaperLifecycleHelper$", "--", scenario)
			command.Env = append(os.Environ(), "GO_WANT_SUBREAPER_HELPER=1")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("subreaper helper: %v\n%s", err, output)
			}
		})
	}
}

func TestSubreaperLifecycleHelper(t *testing.T) {
	if os.Getenv("GO_WANT_SUBREAPER_HELPER") != "1" {
		return
	}
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, 36, 1, 0, 0, 0, 0); errno != 0 { // PR_SET_CHILD_SUBREAPER
		t.Fatalf("enable subreaper: %v", errno)
	}
	scenario := os.Args[len(os.Args)-1]
	if scenario == "concurrent" {
		runConcurrentSubreaperCalls(t)
		return
	}
	runSubreaperCall(t, scenario)
}

func runSubreaperCall(t *testing.T, scenario string) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), scenario+".pid")
	mode := "spawn-success"
	command := testCommand(mode, pidFile)
	ctx := context.Background()
	cancel := func() {}

	switch scenario {
	case "cancellation":
		mode = "spawn"
		ctx, cancel = context.WithCancel(context.Background())
	case "timeout":
		mode = "spawn"
		command.Timeout = 80 * time.Millisecond
	case "stdout-overflow":
		mode = "spawn-stdout-overflow"
		command.StdoutLimit = 16
	case "stderr-overflow":
		mode = "spawn-stderr-overflow"
		command.StderrLimit = 16
	}
	command.Args[2] = mode
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := (&Runner{}).Call(ctx, command)
		done <- err
	}()
	waitForFile(t, pidFile)
	if scenario == "cancellation" {
		cancel()
	}
	err := <-done
	switch scenario {
	case "natural":
		if err != nil {
			t.Fatal(err)
		}
	case "cancellation":
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want cancellation", err)
		}
	case "timeout":
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want deadline", err)
		}
	default:
		if !errors.Is(err, ErrOutputLimit) {
			t.Fatalf("error = %v, want output limit", err)
		}
	}
	assertProcessGone(t, readPID(t, pidFile))
}

func runConcurrentSubreaperCalls(t *testing.T) {
	t.Helper()
	type call struct {
		pidFile string
		done    chan error
	}
	calls := make([]call, 2)
	for index := range calls {
		calls[index] = call{pidFile: filepath.Join(t.TempDir(), strconv.Itoa(index)+".pid"), done: make(chan error, 1)}
		command := testCommand("spawn-success", calls[index].pidFile)
		go func() {
			_, err := (&Runner{}).Call(context.Background(), command)
			calls[index].done <- err
		}()
	}
	for _, current := range calls {
		if err := <-current.done; err != nil {
			t.Fatal(err)
		}
		assertProcessGone(t, readPID(t, current.pidFile))
	}
}

func TestCallRejectsUnpinnedOrUnboundedCommand(t *testing.T) {
	valid := testCommand("stdin")
	for name, mutate := range map[string]func(*Command){
		"relative path": func(command *Command) { command.Path = "provider" },
		"zero timeout":  func(command *Command) { command.Timeout = 0 },
		"zero stdin":    func(command *Command) { command.StdinLimit = 0 },
		"zero stdout":   func(command *Command) { command.StdoutLimit = 0 },
		"zero stderr":   func(command *Command) { command.StderrLimit = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			command := valid
			mutate(&command)
			if _, err := (&Runner{}).Call(context.Background(), command); !errors.Is(err, ErrInvalidCommand) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestProcessAuthorityRetiresBeforeLeaderReap(t *testing.T) {
	leaderReaped := false
	kills := 0
	authority := processAuthority{
		pgid: 42,
		kill: func(pid int, signal syscall.Signal) error {
			if leaderReaped {
				t.Fatal("negative PGID signalled after leader reap")
			}
			if pid != -42 || signal != syscall.SIGKILL {
				t.Fatalf("kill(%d, %v)", pid, signal)
			}
			kills++
			return nil
		},
		reap: func() error {
			leaderReaped = true
			return nil
		},
		reapGroup: func(int) error { return nil },
	}
	if leaderErr, groupErr := authority.retireAndReap(); leaderErr != nil || groupErr != nil {
		t.Fatalf("leader error=%v group error=%v", leaderErr, groupErr)
	}
	authority.terminate(nil)
	if kills != 1 || !leaderReaped {
		t.Fatalf("kills=%d leaderReaped=%t", kills, leaderReaped)
	}
}

func TestProcessAuthorityPublishesReasonBeforeTerminationSignal(t *testing.T) {
	var authority processAuthority
	authority = processAuthority{
		pgid: 42,
		kill: func(int, syscall.Signal) error {
			if !errors.Is(authority.reason, context.Canceled) {
				t.Fatalf("termination signal observed before reason publication: %v", authority.reason)
			}
			return nil
		},
		reap:      func() error { return nil },
		reapGroup: func(int) error { return nil },
	}
	if !authority.terminate(context.Canceled) {
		t.Fatal("termination did not win")
	}
	if !errors.Is(authority.terminationReason(), context.Canceled) {
		t.Fatalf("termination reason = %v", authority.terminationReason())
	}
}

func testCommand(mode string, arguments ...string) Command {
	args := append([]string{"-test.run=TestRunnerHelperProcess", "--", mode}, arguments...)
	return Command{
		Path: os.Args[0], Args: args,
		Env: []string{"GO_WANT_RUNNER_HELPER=1"}, Timeout: 3 * time.Second,
		StdinLimit: 4096, StdoutLimit: 4096, StderrLimit: 4096,
	}
}

func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_RUNNER_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}
	arguments := os.Args[separator+2:]
	switch os.Args[separator+1] {
	case "inspect":
		cwd, _ := os.Getwd()
		info, _ := os.Stat(cwd)
		encoded, _ := json.Marshal(struct {
			Args []string `json:"args"`
			Auth string   `json:"auth"`
			CWD  string   `json:"cwd"`
			Mode uint32   `json:"mode"`
			EOF  bool     `json:"eof"`
		}{arguments[:1], arguments[1], cwd, uint32(info.Mode().Perm()), stdinEOF()})
		_, _ = os.Stdout.Write(encoded)
	case "stdin", "stream-echo":
		_, _ = io.Copy(os.Stdout, os.Stdin)
	case "failure":
		_, _ = os.Stdout.WriteString("partial-output")
		_, _ = os.Stderr.WriteString(arguments[0])
		os.Exit(23)
	case "stdout-overflow":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", 4096))
		time.Sleep(time.Second)
	case "stderr-overflow":
		_, _ = os.Stderr.WriteString(strings.Repeat("x", 4096))
		time.Sleep(time.Second)
	case "spawn", "spawn-success", "spawn-failure", "spawn-stdout-overflow", "spawn-stderr-overflow", "spawn-no-read":
		child := exec.Command(os.Args[0], "-test.run=TestRunnerHelperProcess", "--", "sleep")
		child.Env = []string{"GO_WANT_RUNNER_HELPER=1"}
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		_ = os.WriteFile(arguments[0], []byte(strconv.Itoa(child.Process.Pid)), 0o600)
		switch os.Args[separator+1] {
		case "spawn-success":
			os.Exit(0)
		case "spawn-failure":
			os.Exit(23)
		case "spawn-stdout-overflow":
			_, _ = os.Stdout.WriteString(strings.Repeat("x", 4096))
			time.Sleep(time.Second)
		case "spawn-stderr-overflow":
			_, _ = os.Stderr.WriteString(strings.Repeat("x", 4096))
			time.Sleep(time.Second)
		case "spawn-no-read":
			time.Sleep(5 * time.Second)
		default:
			_ = child.Wait()
		}
	case "sleep":
		time.Sleep(5 * time.Second)
	}
	os.Exit(0)
}

func stdinEOF() bool {
	var value [1]byte
	n, _ := os.Stdin.Read(value[:])
	return n == 0
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process %d remains immediately after Wait: %v", pid, err)
	}
}

func assertEmptyDirectory(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("residual provider directories: %v", entries)
	}
}
