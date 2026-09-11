// Package gitea implements the restricted, typed tea compatibility client.
package gitea

import (
	"fmt"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const (
	maximumArguments = 64
	maximumArgvBytes = 64 << 10
)

type outputFormat byte

const (
	outputSimple outputFormat = iota
	outputTable
	outputJSON
)

type command struct {
	request *repowolfv1.GiteaRequest
	format  outputFormat
}

// Parse converts one bounded supported native tea argv into a typed request.
func Parse(args []string) (command, error) {
	if len(args) < 4 || len(args) > maximumArguments {
		return command{}, fmt.Errorf("invalid argument count")
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if total > maximumArgvBytes || !utf8.ValidString(arg) || strings.IndexByte(arg, 0) >= 0 {
			return command{}, fmt.Errorf("invalid argument data")
		}
	}
	if args[0] != "repos" && args[0] != "repo" {
		return command{}, fmt.Errorf("unsupported command")
	}
	positional := args[1]
	if strings.HasPrefix(positional, "-") {
		return command{}, fmt.Errorf("missing repository")
	}
	owner, name, ok := parseSlug(positional)
	if !ok {
		return command{}, fmt.Errorf("invalid repository")
	}
	var explicit string
	format := outputSimple
	seenRepo, seenOutput := false, false
	for i := 2; i < len(args); {
		flag := args[i]
		if strings.Contains(flag, "=") || i+1 >= len(args) {
			return command{}, fmt.Errorf("invalid flag")
		}
		value := args[i+1]
		switch flag {
		case "--repo", "-r":
			if seenRepo {
				return command{}, fmt.Errorf("duplicate repository flag")
			}
			seenRepo, explicit = true, value
		case "--output", "-o":
			if seenOutput {
				return command{}, fmt.Errorf("duplicate output flag")
			}
			seenOutput = true
			switch value {
			case "simple":
				format = outputSimple
			case "table":
				format = outputTable
			case "json":
				format = outputJSON
			default:
				return command{}, fmt.Errorf("invalid output")
			}
		default:
			return command{}, fmt.Errorf("unsupported flag")
		}
		i += 2
	}
	if !seenRepo {
		return command{}, fmt.Errorf("missing repository flag")
	}
	if _, _, ok := parseSlug(explicit); !ok || !strings.EqualFold(positional, explicit) {
		return command{}, fmt.Errorf("repository selectors disagree")
	}
	request := &repowolfv1.GiteaRequest{
		Context:   &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: owner, Name: name}},
		Operation: &repowolfv1.GiteaRequest_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewRequest{}},
	}
	return command{request: request, format: format}, nil
}

func parseSlug(value string) (string, string, bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !validPart(parts[0]) || !validPart(parts[1]) || strings.HasSuffix(strings.ToLower(parts[1]), ".git") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func validPart(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." || !isAlphaNumeric(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isAlphaNumeric(value[i]) && value[i] != '.' && value[i] != '_' && value[i] != '-' {
			return false
		}
	}
	return true
}

func isAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
