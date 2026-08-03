package main

import (
	"fmt"
	"io"
	"os"

	"github.com/rochecompaan/repowolf/internal/buildinfo"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, buildinfo.Version)
		return 0
	}

	fmt.Fprintln(stderr, "usage: repowolf <serve|config|token|cert>")
	return 2
}
