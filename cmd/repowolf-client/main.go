package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	clientgithub "github.com/rochecompaan/repowolf/internal/client/github"
)

func main() {
	os.Exit(runMain(os.Args, os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	finished := make(chan struct{})
	handlerDone := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(handlerDone)
		select {
		case received := <-signals:
			if processSignal, ok := received.(syscall.Signal); ok {
				cancel(signalCause{signal: processSignal})
			} else {
				cancel(context.Canceled)
			}
		case <-finished:
		}
	}()

	status := runClient(ctx, args[0], args[1:], stdout, stderr)
	signal.Stop(signals)
	close(finished)
	cancel(context.Canceled)
	<-handlerDone
	return status
}

type signalCause struct{ signal syscall.Signal }

func (cause signalCause) Error() string { return "interrupted" }
func (cause signalCause) ExitCode() int { return 128 + int(cause.signal) }

func runClient(ctx context.Context, name string, args []string, stdout, stderr io.Writer) int {
	mode, ok := modeForBase(name)
	if !ok {
		fmt.Fprintln(stderr, "usage: gh | repowolf-git-ssh")
		return 2
	}
	if mode == "gh" {
		return clientgithub.Run(ctx, args, stdout, stderr)
	}
	fmt.Fprintln(stderr, "usage: repowolf-git-ssh")
	return 2
}

func modeForBase(name string) (string, bool) {
	switch base := filepath.Base(name); base {
	case "gh", "repowolf-git-ssh":
		return base, true
	default:
		return "", false
	}
}
