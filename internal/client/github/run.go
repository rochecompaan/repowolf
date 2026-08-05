package github

import (
	"fmt"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseRun(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("run subcommand is required")
	}
	switch args[0] {
	case "list":
		flags, err := parseFlags(args[1:], operationFlags("--branch", "--status", "--limit"))
		limit, limitErr := listLimit(valueOr(flags, "--limit", "30"))
		request := &repowolfv1.GitHubRunListRequest{Limit: limit}
		if branch, ok := flags.values["--branch"]; ok {
			request.Branch = &branch
			if !validName(branch, false) {
				err = firstError(err, fmt.Errorf("invalid --branch"))
			}
		}
		if raw, ok := flags.values["--status"]; ok {
			status, statusErr := runStatus(raw)
			request.Status = &status
			err = firstError(err, statusErr)
		}
		err = firstError(err, limitErr)
		return &repowolfv1.GitHubRequest_RunList{RunList: request}, flags, operationRunList, err
	case "view":
		number, rest, err := requiredNumber(args[1:])
		if err != nil {
			return nil, parsedFlags{}, operationUnknown, fmt.Errorf("run ID is required")
		}
		flags, flagErr := parseFlags(rest, operationFlags())
		operation := &repowolfv1.GitHubRequest_RunView{RunView: &repowolfv1.GitHubRunViewRequest{RunId: number}}
		return operation, flags, operationRunView, flagErr
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported run operation")
	}
}

func parseStatus(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) < 2 || args[0] != "get" || !validObjectID(args[1]) {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("status get requires a full object ID")
	}
	flags, err := parseFlags(args[2:], operationFlags())
	operation := &repowolfv1.GitHubRequest_StatusView{StatusView: &repowolfv1.GitHubStatusViewRequest{ObjectId: args[1]}}
	return operation, flags, operationStatusView, err
}

func runStatus(value string) (repowolfv1.GitHubRunStatus, error) {
	statuses := map[string]repowolfv1.GitHubRunStatus{
		"queued": 1, "in_progress": 2, "completed": 3, "success": 4,
		"failure": 5, "cancelled": 6, "skipped": 7, "timed_out": 8,
		"action_required": 9, "neutral": 10, "stale": 11, "startup_failure": 12,
		"requested": 13, "waiting": 14, "pending": 15,
	}
	status, ok := statuses[value]
	if !ok {
		return 0, fmt.Errorf("invalid run status")
	}
	return status, nil
}

func validObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}
