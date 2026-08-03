package gitssh

import (
	"context"
	"errors"
	"io"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/clientconfig"
)

const fixedDiagnostic = "repowolf git transport failed\n"

// Run parses one Git-generated SSH invocation and relays it over the shared
// TLS and bearer-authenticated RepoWolf client transport.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	request, err := Parse(args)
	if err != nil {
		writeDiagnostic(stderr)
		return 2
	}
	config, err := clientconfig.LoadEnv()
	if err != nil {
		writeDiagnostic(stderr)
		return 1
	}
	connection, err := clientconfig.Dial(ctx, config)
	if err != nil {
		writeDiagnostic(stderr)
		return contextStatus(ctx)
	}
	defer connection.Close()

	terminal, err := relay(ctx, openerFor(repowolfv1.NewGitServiceClient(connection)), request, stdin, stdout)
	if err != nil {
		writeDiagnostic(stderr)
		return contextStatus(ctx)
	}
	if terminal.ExitCode != 0 {
		writeDiagnostic(stderr)
		return shellStatus(terminal.ExitCode)
	}
	return 0
}

func writeDiagnostic(stderr io.Writer) {
	if stderr != nil {
		_, _ = io.WriteString(stderr, fixedDiagnostic)
	}
}

func contextStatus(ctx context.Context) int {
	if !errors.Is(ctx.Err(), context.Canceled) {
		return 1
	}
	if cause, ok := context.Cause(ctx).(interface{ ExitCode() int }); ok {
		return shellStatus(int32(cause.ExitCode()))
	}
	return 130
}

func shellStatus(status int32) int {
	if status < 1 || status > 255 {
		return 1
	}
	return int(status)
}
