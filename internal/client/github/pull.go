package github

import (
	"fmt"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parsePull(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("pull-request subcommand is required")
	}
	switch args[0] {
	case "list":
		return parsePullList(args[1:])
	case "view":
		return parsePullNumber(args[1:], operationPullView)
	case "create":
		return parsePullCreate(args[1:])
	case "edit":
		return parsePullEdit(args[1:])
	case "comment":
		return parsePullComment(args[1:])
	case "close":
		return parsePullNumber(args[1:], operationPullClose)
	case "reopen":
		return parsePullNumber(args[1:], operationPullReopen)
	case "ready":
		return parsePullNumber(args[1:], operationPullReady)
	case "checks":
		return parsePullNumber(args[1:], operationPullChecks)
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported pull-request operation")
	}
}

func parsePullList(args []string) (any, parsedFlags, operationKind, error) {
	flags, err := parseFlags(args, operationFlags("--state", "--base", "--head", "--limit"))
	state, stateErr := pullState(valueOr(flags, "--state", "open"))
	limit, limitErr := listLimit(valueOr(flags, "--limit", "30"))
	request := &repowolfv1.GitHubPullListRequest{State: state, Limit: limit}
	for name, target := range map[string]**string{"--base": &request.Base, "--head": &request.Head} {
		if value, ok := flags.values[name]; ok {
			*target = &value
			if !validName(value, false) {
				err = firstError(err, fmt.Errorf("invalid %s", name))
			}
		}
	}
	err = firstError(err, stateErr, limitErr)
	return &repowolfv1.GitHubRequest_PullList{PullList: request}, flags, operationPullList, err
}

func parsePullCreate(args []string) (any, parsedFlags, operationKind, error) {
	flags, err := parseFlags(args, operationFlagsWithBools([]string{"--head", "--base", "--title", "--body"}, "--draft"))
	request := &repowolfv1.GitHubPullCreateRequest{
		Head: valueOr(flags, "--head", ""), Base: valueOr(flags, "--base", ""),
		Title: valueOr(flags, "--title", ""),
	}
	for name, value := range map[string]string{"--head": request.Head, "--base": request.Base} {
		if !validName(value, true) {
			err = firstError(err, fmt.Errorf("%s is required", name))
		}
	}
	if !validTitle(request.Title) {
		err = firstError(err, fmt.Errorf("--title is required"))
	}
	if body, ok := flags.values["--body"]; ok {
		request.Body = &body
		if !validBody(body, false) {
			err = firstError(err, fmt.Errorf("invalid --body"))
		}
	}
	if flags.bools["--draft"] {
		draft := true
		request.Draft = &draft
	}
	return &repowolfv1.GitHubRequest_PullCreate{PullCreate: request}, flags, operationPullCreate, err
}

func parsePullEdit(args []string) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags("--title", "--body", "--base"))
	request := &repowolfv1.GitHubPullEditRequest{Number: number}
	for name, target := range map[string]**string{"--title": &request.Title, "--body": &request.Body, "--base": &request.Base} {
		if value, ok := flags.values[name]; ok {
			*target = &value
			valid := validBody(value, false)
			if name == "--title" {
				valid = validTitle(value)
			} else if name == "--base" {
				valid = validName(value, true)
			}
			if !valid {
				flagErr = firstError(flagErr, fmt.Errorf("invalid %s", name))
			}
		}
	}
	if request.Title == nil && request.Body == nil && request.Base == nil {
		flagErr = firstError(flagErr, fmt.Errorf("pull-request edit requires a mutable field"))
	}
	return &repowolfv1.GitHubRequest_PullEdit{PullEdit: request}, flags, operationPullEdit, flagErr
}

func parsePullComment(args []string) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags("--body"))
	body, present := flags.values["--body"]
	if flagErr == nil && (!present || !validBody(body, true)) {
		flagErr = fmt.Errorf("--body is required")
	}
	operation := &repowolfv1.GitHubRequest_PullComment{PullComment: &repowolfv1.GitHubPullCommentRequest{Number: number, Body: body}}
	return operation, flags, operationPullComment, flagErr
}

func parsePullNumber(args []string, kind operationKind) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags())
	var operation any
	switch kind {
	case operationPullView:
		operation = &repowolfv1.GitHubRequest_PullView{PullView: &repowolfv1.GitHubPullViewRequest{Number: number}}
	case operationPullClose:
		operation = &repowolfv1.GitHubRequest_PullClose{PullClose: &repowolfv1.GitHubPullCloseRequest{Number: number}}
	case operationPullReopen:
		operation = &repowolfv1.GitHubRequest_PullReopen{PullReopen: &repowolfv1.GitHubPullReopenRequest{Number: number}}
	case operationPullReady:
		operation = &repowolfv1.GitHubRequest_PullReady{PullReady: &repowolfv1.GitHubPullReadyRequest{Number: number}}
	case operationPullChecks:
		operation = &repowolfv1.GitHubRequest_PullChecks{PullChecks: &repowolfv1.GitHubPullChecksRequest{Number: number}}
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("invalid pull-request operation")
	}
	return operation, flags, kind, flagErr
}

func pullState(value string) (repowolfv1.GitHubPullState, error) {
	states := map[string]repowolfv1.GitHubPullState{"open": 1, "closed": 2, "all": 3}
	state, ok := states[value]
	if !ok {
		return 0, fmt.Errorf("invalid pull-request state")
	}
	return state, nil
}

func validName(value string, required bool) bool {
	return (!required || value != "") && len(value) <= 255 && utf8.ValidString(value) && !strings.ContainsAny(value, " \t\r\n\x00")
}
