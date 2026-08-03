// Package cli implements service administration command handlers.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/rochecompaan/repowolf/internal/auth"
)

var errWriteGeneratedToken = errors.New("failed to write generated token")

// RunTokenGenerate writes one newly generated opaque token to stdout.
func RunTokenGenerate(stdout io.Writer, random io.Reader) error {
	token, err := auth.Generate(random)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, token); err != nil {
		return errWriteGeneratedToken
	}
	return nil
}
