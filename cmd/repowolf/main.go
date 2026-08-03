package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"

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

	fmt.Fprintln(stderr, "usage: repowolf <serve|config|token|cert>")
	return 2
}
