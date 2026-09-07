package github

import (
	"fmt"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseAuth(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) != 1 || args[0] != "status" {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported auth operation")
	}
	return currentUserOperation(), parsedFlags{}, operationAuthStatus, nil
}

func parseAPI(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) != 3 || args[0] != "user" || args[1] != "--jq" || args[2] != ".login" {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported API operation")
	}
	return currentUserOperation(), parsedFlags{}, operationCurrentUserLogin, nil
}

func currentUserOperation() *repowolfv1.GitHubRequest_CurrentUser {
	return &repowolfv1.GitHubRequest_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserRequest{}}
}
