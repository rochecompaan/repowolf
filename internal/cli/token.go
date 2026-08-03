// Package cli implements service administration command handlers.
package cli

import (
	"fmt"
	"io"

	"github.com/rochecompaan/repowolf/internal/auth"
)

// RunTokenGenerate writes one newly generated opaque token to stdout.
func RunTokenGenerate(stdout io.Writer, random io.Reader) error {
	token, err := auth.Generate(random)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, token)
	return err
}
