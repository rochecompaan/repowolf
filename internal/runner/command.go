package runner

import (
	"errors"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrInvalidCommand = errors.New("invalid provider command")
	ErrStartFailed    = errors.New("provider command start failed")
	ErrCommandFailed  = errors.New("provider command failed")
	ErrInputLimit     = errors.New("provider input limit exceeded")
	ErrOutputLimit    = errors.New("provider output limit exceeded")
	ErrCleanupFailed  = errors.New("provider command cleanup failed")
)

// Command is a trusted, generated provider invocation using a pinned path.
type Command struct {
	Path        string
	Args        []string
	Stdin       []byte
	Env         []string
	Timeout     time.Duration
	StdinLimit  int
	StdoutLimit int
	StderrLimit int
}

// Result exposes only bounded output, process status, timeout state, and safe
// byte counts. Raw stderr is never returned.
type Result struct {
	Stdout      []byte
	ExitCode    int
	TimedOut    bool
	StdinBytes  int64
	StdoutBytes int64
	StderrBytes int64
}

func cloneAndValidate(command Command) (Command, error) {
	if !filepath.IsAbs(command.Path) || command.Timeout <= 0 || command.StdinLimit <= 0 || command.StdoutLimit <= 0 || command.StderrLimit <= 0 {
		return Command{}, ErrInvalidCommand
	}
	if len(command.Stdin) > command.StdinLimit {
		return Command{}, ErrInputLimit
	}
	if strings.IndexByte(command.Path, 0) >= 0 {
		return Command{}, ErrInvalidCommand
	}
	for _, argument := range command.Args {
		if strings.IndexByte(argument, 0) >= 0 {
			return Command{}, ErrInvalidCommand
		}
	}
	for _, entry := range command.Env {
		if strings.IndexByte(entry, 0) >= 0 {
			return Command{}, ErrInvalidCommand
		}
	}
	command.Args = append([]string(nil), command.Args...)
	command.Stdin = append([]byte(nil), command.Stdin...)
	// A non-nil empty slice is significant: exec.Cmd otherwise inherits the
	// service environment instead of using the requested exact environment.
	command.Env = append([]string{}, command.Env...)
	return command, nil
}
