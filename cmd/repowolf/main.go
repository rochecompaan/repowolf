package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rochecompaan/repowolf/internal/app"
	"github.com/rochecompaan/repowolf/internal/buildinfo"
	"github.com/rochecompaan/repowolf/internal/cli"
)

var serveCommand = app.Serve

func main() {
	ctx, stop := shutdownContext()
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func shutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}
	if len(args) == 2 && args[0] == "token" && args[1] == "generate" {
		if err := cli.RunTokenGenerate(stdout, rand.Reader); err != nil {
			fmt.Fprintln(stderr, "token generation failed")
			return 1
		}
		return 0
	}
	if len(args) >= 2 && args[0] == "config" && args[1] == "validate" {
		if err := cli.RunConfigValidate(args[2:]); err != nil {
			if errors.Is(err, cli.ErrInvalidConfigArguments) {
				fmt.Fprintln(stderr, "invalid config validate arguments")
				return 2
			}
			fmt.Fprintln(stderr, "configuration validation failed")
			return 1
		}
		fmt.Fprintln(stdout, "configuration valid")
		return 0
	}
	if len(args) >= 1 && args[0] == "serve" {
		path, err := cli.ConfigPath(args[1:])
		if err != nil {
			fmt.Fprintln(stderr, "invalid serve arguments")
			return 2
		}
		if err := serveCommand(ctx, path, stdout); err != nil {
			fmt.Fprintln(stderr, "service failed")
			return 1
		}
		return 0
	}
	if len(args) >= 2 && args[0] == "cert" && args[1] == "init" {
		if err := cli.RunCertInit(args[2:], stdout, time.Now, rand.Reader); err != nil {
			if errors.Is(err, cli.ErrInvalidCertArguments) {
				fmt.Fprintln(stderr, "invalid cert init arguments")
				return 2
			}
			fmt.Fprintln(stderr, "certificate initialization failed")
			return 1
		}
		return 0
	}

	fmt.Fprintln(stderr, "usage: repowolf <serve|config|token|cert>")
	return 2
}
