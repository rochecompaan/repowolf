package github

import (
	"fmt"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseIssue(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("issue subcommand is required")
	}
	switch args[0] {
	case "list":
		flags, err := parseFlags(args[1:], operationFlags("--state", "--limit"))
		state, stateErr := issueState(valueOr(flags, "--state", "open"))
		limit, limitErr := listLimit(valueOr(flags, "--limit", "30"), 1001)
		err = firstError(err, stateErr, limitErr)
		return &repowolfv1.GitHubRequest_IssueList{IssueList: &repowolfv1.GitHubIssueListRequest{State: state, Limit: limit}}, flags, operationIssueList, err
	case "view":
		return parseIssueNumber(args[1:], operationIssueView)
	case "create":
		flags, err := parseFlags(args[1:], operationFlags("--title", "--body", "--label"))
		title, present := flags.values["--title"]
		if err == nil && (!present || !validTitle(title)) {
			err = fmt.Errorf("--title is required")
		}
		request := &repowolfv1.GitHubIssueCreateRequest{Title: title}
		if body, ok := flags.values["--body"]; ok {
			request.Body = &body
			if !validBody(body, false) && err == nil {
				err = fmt.Errorf("invalid --body")
			}
		}
		if labels, ok := flags.values["--label"]; ok {
			var labelErr error
			request.Labels, labelErr = labelCSV(labels)
			err = firstError(err, labelErr)
		}
		return &repowolfv1.GitHubRequest_IssueCreate{IssueCreate: request}, flags, operationIssueCreate, err
	case "edit":
		return parseIssueEdit(args[1:])
	case "comment":
		return parseIssueComment(args[1:])
	case "close":
		return parseIssueNumber(args[1:], operationIssueClose)
	case "reopen":
		return parseIssueNumber(args[1:], operationIssueReopen)
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported issue operation")
	}
}

func parseIssueEdit(args []string) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags("--title", "--body", "--add-label", "--remove-label"))
	if _, labelsChanged := flags.values["--add-label"]; labelsChanged {
		return parseIssueLabelChange(number, flags, flagErr)
	}
	if _, labelsChanged := flags.values["--remove-label"]; labelsChanged {
		return parseIssueLabelChange(number, flags, flagErr)
	}
	request := &repowolfv1.GitHubIssueEditRequest{Number: number}
	if value, ok := flags.values["--title"]; ok {
		request.Title = &value
		if !validTitle(value) {
			flagErr = firstError(flagErr, fmt.Errorf("invalid --title"))
		}
	}
	if value, ok := flags.values["--body"]; ok {
		request.Body = &value
		if !validBody(value, false) {
			flagErr = firstError(flagErr, fmt.Errorf("invalid --body"))
		}
	}
	if request.Title == nil && request.Body == nil {
		flagErr = firstError(flagErr, fmt.Errorf("issue edit requires a mutable field"))
	}
	return &repowolfv1.GitHubRequest_IssueEdit{IssueEdit: request}, flags, operationIssueEdit, flagErr
}

func parseIssueComment(args []string) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags("--body"))
	body, present := flags.values["--body"]
	if flagErr == nil && (!present || !validBody(body, true)) {
		flagErr = fmt.Errorf("--body is required")
	}
	return &repowolfv1.GitHubRequest_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentRequest{Number: number, Body: body}}, flags, operationIssueComment, flagErr
}

func parseIssueNumber(args []string, kind operationKind) (any, parsedFlags, operationKind, error) {
	number, rest, err := requiredNumber(args)
	if err != nil {
		return nil, parsedFlags{}, operationUnknown, err
	}
	flags, flagErr := parseFlags(rest, operationFlags())
	switch kind {
	case operationIssueView:
		return &repowolfv1.GitHubRequest_IssueView{IssueView: &repowolfv1.GitHubIssueViewRequest{Number: number}}, flags, kind, flagErr
	case operationIssueClose:
		return &repowolfv1.GitHubRequest_IssueClose{IssueClose: &repowolfv1.GitHubIssueCloseRequest{Number: number}}, flags, kind, flagErr
	case operationIssueReopen:
		return &repowolfv1.GitHubRequest_IssueReopen{IssueReopen: &repowolfv1.GitHubIssueReopenRequest{Number: number}}, flags, kind, flagErr
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("invalid issue operation")
	}
}

func requiredNumber(args []string) (uint64, []string, error) {
	if len(args) == 0 {
		return 0, nil, fmt.Errorf("positive number is required")
	}
	number, err := positiveDecimal(args[0])
	return number, args[1:], err
}

func issueState(value string) (repowolfv1.GitHubIssueState, error) {
	states := map[string]repowolfv1.GitHubIssueState{"open": 1, "closed": 2, "all": 3}
	state, ok := states[value]
	if !ok {
		return 0, fmt.Errorf("invalid issue state")
	}
	return state, nil
}

func parseIssueLabelChange(number uint64, flags parsedFlags, flagErr error) (any, parsedFlags, operationKind, error) {
	if _, titleSet := flags.values["--title"]; titleSet {
		flagErr = firstError(flagErr, fmt.Errorf("label changes cannot be combined with --title or --body"))
	}
	if _, bodySet := flags.values["--body"]; bodySet {
		flagErr = firstError(flagErr, fmt.Errorf("label changes cannot be combined with --title or --body"))
	}
	request := &repowolfv1.GitHubIssueLabelChangeRequest{Number: number}
	if value, ok := flags.values["--add-label"]; ok {
		var labelErr error
		request.AddLabels, labelErr = labelCSV(value)
		flagErr = firstError(flagErr, labelErr)
	}
	if value, ok := flags.values["--remove-label"]; ok {
		var labels []string
		var labelErr error
		labels, labelErr = labelCSV(value)
		flagErr = firstError(flagErr, labelErr)
		request.RemoveLabels = labels
	}
	for _, add := range request.AddLabels {
		for _, remove := range request.RemoveLabels {
			if add == remove {
				flagErr = firstError(flagErr, fmt.Errorf("label cannot be both added and removed"))
			}
		}
	}
	return &repowolfv1.GitHubRequest_IssueLabelChange{IssueLabelChange: request}, flags, operationIssueLabelChange, flagErr
}

func listLimit(value string, maximum uint64) (uint64, error) {
	limit, err := positiveDecimal(value)
	if err != nil || limit > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return limit, nil
}

func validTitle(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value)
}
func validBody(value string, required bool) bool {
	return (!required || value != "") && len(value) <= 65_536 && utf8.ValidString(value)
}
