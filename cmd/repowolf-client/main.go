package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if _, ok := modeForBase(os.Args[0]); !ok {
		fmt.Fprintln(os.Stderr, "usage: gh | repowolf-git-ssh")
		os.Exit(2)
	}

	fmt.Fprintln(os.Stderr, "usage: gh | repowolf-git-ssh")
	os.Exit(2)
}

func modeForBase(name string) (string, bool) {
	switch base := filepath.Base(name); base {
	case "gh", "repowolf-git-ssh":
		return base, true
	default:
		return "", false
	}
}
