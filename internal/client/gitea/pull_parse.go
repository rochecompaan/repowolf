package gitea

import (
	"fmt"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parsePulls(args []string) (command, error) {
	start := 1
	if len(args) > 1 && (args[1] == "list" || args[1] == "ls") {
		start = 2
	} else if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		index, err := parsePositive(args[1], int64(^uint64(0)>>1))
		if err != nil {
			return command{}, fmt.Errorf("invalid pull index")
		}
		return parsePullView(args, index)
	}
	return parsePullList(args, start)
}

func parsePullList(args []string, start int) (command, error) {
	req := &repowolfv1.GiteaPullListRequest{State: repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN, Page: 1, Limit: 30, Fields: append([]repowolfv1.GiteaPullField(nil), defaultPullFields...)}
	format := outputTable
	seen := map[string]bool{}
	var repo string
	aliases := map[string]string{"-r": "repo", "--repo": "repo", "-o": "output", "--output": "output", "-p": "page", "--page": "page", "--lm": "limit", "--limit": "limit", "--state": "state", "--fields": "fields"}
	for i := start; i < len(args); i += 2 {
		if i+1 >= len(args) || strings.Contains(args[i], "=") {
			return command{}, fmt.Errorf("invalid flag")
		}
		key, ok := aliases[args[i]]
		if !ok || seen[key] {
			return command{}, fmt.Errorf("unsupported or duplicate flag")
		}
		seen[key] = true
		value := args[i+1]
		switch key {
		case "repo":
			repo = value
		case "output":
			var err error
			format, err = parseOutput(value, outputTable)
			if err != nil {
				return command{}, err
			}
		case "state":
			switch value {
			case "open":
				req.State = repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN
			case "closed":
				req.State = repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED
			case "all":
				req.State = repowolfv1.GiteaPullState_GITEA_PULL_STATE_ALL
			default:
				return command{}, fmt.Errorf("invalid state")
			}
		case "page":
			n, err := parsePositive(value, int64(^uint32(0)>>1))
			if err != nil {
				return command{}, fmt.Errorf("invalid page")
			}
			req.Page = int32(n)
		case "limit":
			n, err := parsePositive(value, 50)
			if err != nil {
				return command{}, fmt.Errorf("invalid limit")
			}
			req.Limit = int32(n)
		case "fields":
			fields, err := parsePullFields(value)
			if err != nil {
				return command{}, err
			}
			req.Fields = fields
		}
	}
	if !seen["repo"] {
		return command{}, fmt.Errorf("missing repository")
	}
	owner, name, ok := parseSlug(repo)
	if !ok {
		return command{}, fmt.Errorf("invalid repository")
	}
	request := requestFor(owner, name)
	request.Operation = &repowolfv1.GiteaRequest_PullList{PullList: req}
	return command{request: request, format: format, pullFields: append([]repowolfv1.GiteaPullField(nil), req.Fields...)}, nil
}

func parsePullView(args []string, index int64) (command, error) {
	req := &repowolfv1.GiteaPullViewRequest{Index: index}
	format := outputSimple
	seen := map[string]bool{}
	var repo string
	for i := 2; i < len(args); {
		flag := args[i]
		key := map[string]string{"-r": "repo", "--repo": "repo", "-o": "output", "--output": "output", "--comments": "comments"}[flag]
		if key == "" || seen[key] || strings.Contains(flag, "=") {
			return command{}, fmt.Errorf("unsupported or duplicate flag")
		}
		seen[key] = true
		if key == "comments" {
			req.IncludeComments = true
			i++
			continue
		}
		if i+1 >= len(args) {
			return command{}, fmt.Errorf("missing flag value")
		}
		if key == "repo" {
			repo = args[i+1]
		} else {
			var err error
			format, err = parseOutput(args[i+1], outputSimple)
			if err != nil {
				return command{}, err
			}
		}
		i += 2
	}
	if !seen["repo"] {
		return command{}, fmt.Errorf("missing repository")
	}
	owner, name, ok := parseSlug(repo)
	if !ok {
		return command{}, fmt.Errorf("invalid repository")
	}
	request := requestFor(owner, name)
	request.Operation = &repowolfv1.GiteaRequest_PullView{PullView: req}
	return command{request: request, format: format}, nil
}

func parsePullFields(value string) ([]repowolfv1.GiteaPullField, error) {
	parts := strings.Split(value, ",")
	out := make([]repowolfv1.GiteaPullField, 0, len(parts))
	seen := map[repowolfv1.GiteaPullField]bool{}
	for _, part := range parts {
		field, ok := pullFieldByName[part]
		if !ok || seen[field] {
			return nil, fmt.Errorf("invalid fields")
		}
		seen[field] = true
		out = append(out, field)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid fields")
	}
	return out, nil
}
