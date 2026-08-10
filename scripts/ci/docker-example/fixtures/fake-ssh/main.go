package main

import (
	"fmt"
	"os"
)

func main() {
	path := os.Getenv("FAKE_SSH_LOG")
	if path == "" {
		fmt.Fprintln(os.Stderr, "FAKE_SSH_LOG is unset")
		os.Exit(90)
	}
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	for _, argument := range os.Args[1:] {
		fmt.Fprintln(output, argument)
	}
	if err := output.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(92)
	}
	os.Exit(42)
}
