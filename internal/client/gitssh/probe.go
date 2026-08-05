package gitssh

import "fmt"

// validateVariantProbe accepts only Git's no-command OpenSSH capability probe.
// Receive-pack probes omit SendEnv because that protocol has no version
// negotiation environment to forward.
func validateVariantProbe(args []string) error {
	if err := validateArguments(args); err != nil {
		return err
	}
	if args[0] != "-G" {
		return fmt.Errorf("not an SSH variant probe")
	}

	position := 1
	if len(args)-position >= 2 && args[position] == "-o" {
		if args[position+1] != "SendEnv=GIT_PROTOCOL" {
			return fmt.Errorf("unsupported SSH probe option")
		}
		position += 2
	}
	if len(args)-position >= 2 && args[position] == "-p" {
		if _, err := parsePort(args[position+1]); err != nil {
			return err
		}
		position += 2
	}
	if len(args)-position != 1 {
		return fmt.Errorf("unsupported SSH probe argument shape")
	}
	_, err := parseAuthority(args[position])
	return err
}
