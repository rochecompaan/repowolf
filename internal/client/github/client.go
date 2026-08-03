package github

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/clientconfig"
)

const operationTimeout = 2 * time.Minute

// Run parses, executes, and renders one restricted gh command.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		writeDiagnostic(stderr, "gh: unsupported or invalid command\n")
		return 2
	}
	parsed, err := parseArgs(args, cwd)
	if err != nil {
		writeDiagnostic(stderr, "gh: unsupported or invalid command\n")
		return 2
	}
	config, err := clientconfig.LoadEnv()
	if err != nil {
		writeDiagnostic(stderr, "gh: client configuration failed\n")
		return 1
	}
	connection, err := clientconfig.Dial(ctx, config)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return interrupted(ctx, stderr)
		}
		writeDiagnostic(stderr, "gh: connection failed\n")
		return 1
	}
	defer connection.Close()

	operationContext, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := executeCommand(operationContext, repowolfv1.NewGitHubServiceClient(connection), parsed, stdout); err != nil {
		if errors.Is(operationContext.Err(), context.Canceled) {
			return interrupted(operationContext, stderr)
		}
		writeDiagnostic(stderr, "gh: GitHub operation failed\n")
		return 1
	}
	return 0
}

func executeCommand(ctx context.Context, client repowolfv1.GitHubServiceClient, parsed command, stdout io.Writer) error {
	response, err := client.Execute(ctx, parsed.request)
	if err != nil {
		return err
	}
	output, err := render(parsed, response)
	if err != nil {
		return err
	}
	return writeExact(stdout, output)
}

func writeExact(writer io.Writer, value []byte) error {
	if writer == nil {
		return io.ErrClosedPipe
	}
	for len(value) != 0 {
		count, err := writer.Write(value)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}

func writeDiagnostic(writer io.Writer, message string) {
	if writer != nil {
		_, _ = io.WriteString(writer, message)
	}
}

func interrupted(ctx context.Context, stderr io.Writer) int {
	writeDiagnostic(stderr, "gh: interrupted\n")
	if cause, ok := context.Cause(ctx).(interface{ ExitCode() int }); ok {
		return cause.ExitCode()
	}
	return 130
}
