// Package github implements the restricted, typed gh compatibility client.
package github

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	clientrepo "github.com/rochecompaan/repowolf/internal/client"
)

const (
	maximumArguments = 64
	maximumArgvBytes = 64 << 10
)

type operationKind byte

const (
	operationUnknown operationKind = iota
	operationRepositoryView
	operationAuthStatus
	operationCurrentUserLogin
	operationIssueList
	operationIssueView
	operationIssueCreate
	operationIssueEdit
	operationIssueComment
	operationIssueClose
	operationIssueReopen
	operationLabelList
	operationLabelCreate
	operationIssueLabelChange
	operationPullList
	operationPullView
	operationPullCreate
	operationPullEdit
	operationPullComment
	operationPullClose
	operationPullReopen
	operationPullReady
	operationPullChecks
	operationRunList
	operationRunView
	operationStatusView
)

type command struct {
	request *repowolfv1.GitHubRequest
	kind    operationKind
	fields  []string
}

// Parse converts one bounded supported native gh argv into a typed request.
func Parse(args []string, cwd string) (*repowolfv1.GitHubRequest, error) {
	parsed, err := parseArgs(args, cwd)
	if err != nil {
		return nil, err
	}
	return parsed.request, nil
}

func parseArgs(args []string, cwd string) (command, error) {
	if err := validateArgv(args); err != nil {
		return command{}, err
	}
	var operation any
	var flags parsedFlags
	var kind operationKind
	var err error
	switch args[0] {
	case "auth":
		operation, flags, kind, err = parseAuth(args[1:])
	case "api":
		operation, flags, kind, err = parseAPI(args[1:])
	case "repo":
		operation, flags, kind, err = parseRepository(args[1:])
	case "issue":
		operation, flags, kind, err = parseIssue(args[1:])
	case "label":
		operation, flags, kind, err = parseLabel(args[1:])
	case "pr":
		operation, flags, kind, err = parsePull(args[1:], cwd)
	case "run":
		operation, flags, kind, err = parseRun(args[1:])
	case "status":
		operation, flags, kind, err = parseStatus(args[1:])
	default:
		return command{}, fmt.Errorf("unsupported gh command %q", args[0])
	}
	if err != nil {
		return command{}, err
	}
	fields, err := selectedFields(kind, flags.values["--json"])
	if err != nil {
		return command{}, err
	}
	repository, err := clientrepo.RepositoryHint(cwd, flags.values["--repo"])
	if err != nil {
		return command{}, err
	}
	request := &repowolfv1.GitHubRequest{Context: &repowolfv1.RequestContext{Repository: repository}}
	if err := assignOperation(request, operation); err != nil {
		return command{}, err
	}
	if view := request.GetIssueView(); view != nil {
		view.IncludeComments = slices.Contains(fields, "comments")
	}
	return command{request: request, kind: kind, fields: fields}, nil
}

func validateArgv(args []string) error {
	if len(args) < 2 || len(args) > maximumArguments {
		return fmt.Errorf("an approved command and subcommand are required")
	}
	total := 0
	for _, argument := range args {
		total += len(argument)
		if total > maximumArgvBytes || !utf8.ValidString(argument) || strings.IndexByte(argument, 0) >= 0 {
			return fmt.Errorf("invalid argument data")
		}
	}
	return nil
}

func selectedFields(kind operationKind, value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	allowed, ok := responseFields[kind]
	if !ok {
		return nil, fmt.Errorf("operation does not support selected JSON fields")
	}
	parts := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(parts))
	for _, field := range parts {
		if _, exists := allowed[field]; !exists || field == "" {
			return nil, fmt.Errorf("unsupported JSON field %q", field)
		}
		if _, duplicate := seen[field]; duplicate {
			return nil, fmt.Errorf("duplicate JSON field %q", field)
		}
		seen[field] = struct{}{}
	}
	return parts, nil
}

func assignOperation(request *repowolfv1.GitHubRequest, operation any) error {
	switch value := operation.(type) {
	case *repowolfv1.GitHubRequest_CurrentUser:
		request.Operation = value
	case *repowolfv1.GitHubRequest_RepositoryView:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueList:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueView:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueCreate:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueEdit:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueComment:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueClose:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueReopen:
		request.Operation = value
	case *repowolfv1.GitHubRequest_LabelList:
		request.Operation = value
	case *repowolfv1.GitHubRequest_LabelCreate:
		request.Operation = value
	case *repowolfv1.GitHubRequest_IssueLabelChange:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullList:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullView:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullCreate:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullEdit:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullComment:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullClose:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullReopen:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullReady:
		request.Operation = value
	case *repowolfv1.GitHubRequest_PullChecks:
		request.Operation = value
	case *repowolfv1.GitHubRequest_RunList:
		request.Operation = value
	case *repowolfv1.GitHubRequest_RunView:
		request.Operation = value
	case *repowolfv1.GitHubRequest_StatusView:
		request.Operation = value
	default:
		return fmt.Errorf("unsupported typed operation")
	}
	return nil
}

func firstError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
