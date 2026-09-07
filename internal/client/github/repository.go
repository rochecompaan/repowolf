package github

import (
	"fmt"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseRepository(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 || args[0] != "view" {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported repository operation")
	}
	arguments := args[1:]
	var slug string
	if len(arguments) != 0 && !strings.HasPrefix(arguments[0], "--") {
		if arguments[0] == "" {
			return nil, parsedFlags{}, operationUnknown, fmt.Errorf("repository slug cannot be empty")
		}
		slug, arguments = arguments[0], arguments[1:]
	}
	flags, err := parseFlags(arguments, operationFlags())
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	if slug != "" {
		if _, exists := flags.values["--repo"]; exists {
			return nil, parsedFlags{}, operationUnknown, fmt.Errorf("repository slug and --repo cannot be combined")
		}
		flags.values["--repo"] = slug
	}
	return &repowolfv1.GitHubRequest_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewRequest{}}, flags, operationRepositoryView, nil
}
