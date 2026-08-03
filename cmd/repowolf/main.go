package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rochecompaan/repowolf/internal/buildinfo"
	"github.com/rochecompaan/repowolf/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
