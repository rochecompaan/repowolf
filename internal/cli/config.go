package cli

import (
	"errors"

	"github.com/rochecompaan/repowolf/internal/config"
)

var ErrInvalidConfigArguments = errors.New("invalid config arguments")

// RunConfigValidate decodes and validates static configuration without loading
// token environments, TLS files, or provider executables.
func RunConfigValidate(args []string) error {
	path, err := ConfigPath(args)
	if err != nil {
		return err
	}
	_, err = config.LoadFile(path)
	return err
}

// ConfigPath accepts exactly one --config path argument.
func ConfigPath(args []string) (string, error) {
	if len(args) != 2 || args[0] != "--config" || args[1] == "" {
		return "", ErrInvalidConfigArguments
	}
	return args[1], nil
}
