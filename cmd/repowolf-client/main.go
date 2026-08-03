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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runClient(ctx, os.Args[0], os.Args[1:], os.Stdout, os.Stderr))
}

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
