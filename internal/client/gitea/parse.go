// Package gitea implements the restricted, typed tea compatibility client.
package gitea

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maximumArguments   = 64
	maximumArgvBytes   = 64 << 10
	maximumFilterBytes = 255
)

type outputFormat byte

const (
	outputSimple outputFormat = iota
	outputTable
	outputJSON
)

var defaultIssueFields = []repowolfv1.GiteaIssueField{
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_INDEX,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_TITLE,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_STATE,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_AUTHOR,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_MILESTONE,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_LABELS,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_OWNER,
	repowolfv1.GiteaIssueField_GITEA_ISSUE_FIELD_REPO,
}

type command struct {
	request *repowolfv1.GiteaRequest
	format  outputFormat
	fields  []repowolfv1.GiteaIssueField
}

// Parse converts one bounded supported native tea argv into a typed request.
func Parse(args []string) (command, error) {
	if err := validateArgv(args); err != nil {
		return command{}, err
	}
	switch args[0] {
	case "repos", "repo":
		return parseRepository(args)
	case "issues", "issue", "i":
		return parseIssues(args)
	default:
		return command{}, fmt.Errorf("unsupported command")
	}
}

func validateArgv(args []string) error {
	if len(args) == 0 || len(args) > maximumArguments {
		return fmt.Errorf("invalid argument count")
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if total > maximumArgvBytes || !utf8.ValidString(arg) || strings.IndexByte(arg, 0) >= 0 {
			return fmt.Errorf("invalid argument data")
		}
	}
	return nil
}

func parseRepository(args []string) (command, error) {
	if len(args) < 4 {
		return command{}, fmt.Errorf("invalid argument count")
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
	seen := map[string]bool{}
	for i := 2; i < len(args); i += 2 {
		if i+1 >= len(args) || strings.Contains(args[i], "=") {
			return command{}, fmt.Errorf("invalid flag")
		}
		key := args[i]
		if key == "-r" {
			key = "--repo"
		}
		if key == "-o" {
			key = "--output"
		}
		if seen[key] {
			return command{}, fmt.Errorf("duplicate flag")
		}
		seen[key] = true
		switch key {
		case "--repo":
			explicit = args[i+1]
		case "--output":
			var err error
			format, err = parseOutput(args[i+1], outputSimple)
			if err != nil {
				return command{}, err
			}
		default:
			return command{}, fmt.Errorf("unsupported flag")
		}
	}
	if !seen["--repo"] {
		return command{}, fmt.Errorf("missing repository flag")
	}
	if _, _, ok := parseSlug(explicit); !ok || !strings.EqualFold(positional, explicit) {
		return command{}, fmt.Errorf("repository selectors disagree")
	}
	return command{request: requestFor(owner, name, &repowolfv1.GiteaRequest_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewRequest{}}), format: format}, nil
}

func parseIssues(args []string) (command, error) {
	start := 1
	if len(args) > 1 && (args[1] == "list" || args[1] == "ls") {
		start = 2
	}
	if len(args) > 1 && args[1] != "list" && args[1] != "ls" && !strings.HasPrefix(args[1], "-") {
		index, err := parsePositive(args[1], int64(^uint64(0)>>1))
		if err != nil {
			return command{}, fmt.Errorf("invalid issue index")
		}
		return parseIssueView(args, index)
	}
	return parseIssueList(args, start)
}

func parseIssueList(args []string, start int) (command, error) {
	req := &repowolfv1.GiteaIssueListRequest{State: repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN, Page: 1, Limit: 30, Fields: append([]repowolfv1.GiteaIssueField(nil), defaultIssueFields...)}
	format := outputTable
	seen := map[string]bool{}
	var repo string
	aliases := map[string]string{"-r": "repo", "--repo": "repo", "-o": "output", "--output": "output", "-k": "keyword", "--keyword": "keyword", "-A": "author", "--author": "author", "-a": "assignee", "--assignee": "assignee", "-M": "mentions", "--mentions": "mentions", "-F": "from", "--from": "from", "-u": "until", "--until": "until", "--owner": "owner", "--org": "owner", "-p": "page", "--page": "page", "--lm": "limit", "--limit": "limit", "--state": "state", "--fields": "fields"}
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
				req.State = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN
			case "closed":
				req.State = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED
			case "all":
				req.State = repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_ALL
			default:
				return command{}, fmt.Errorf("invalid state")
			}
		case "keyword", "author", "assignee", "mentions", "owner":
			if err := validateFilter(value); err != nil {
				return command{}, err
			}
			v := value
			switch key {
			case "keyword":
				req.Keyword = &v
			case "author":
				req.Author = &v
			case "assignee":
				req.Assignee = &v
			case "mentions":
				req.Mentions = &v
			case "owner":
				req.Owner = &v
			}
		case "from", "until":
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return command{}, fmt.Errorf("invalid time")
			}
			if key == "from" {
				req.From = timestamppb.New(parsed)
			} else {
				req.Until = timestamppb.New(parsed)
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
			fields, err := parseIssueFields(value)
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
	if req.From != nil && req.Until != nil && req.From.AsTime().After(req.Until.AsTime()) {
		return command{}, fmt.Errorf("invalid time range")
	}
	return command{request: requestFor(owner, name, &repowolfv1.GiteaRequest_IssueList{IssueList: req}), format: format, fields: append([]repowolfv1.GiteaIssueField(nil), req.Fields...)}, nil
}

func parseIssueView(args []string, index int64) (command, error) {
	req := &repowolfv1.GiteaIssueViewRequest{Index: index}
	format := outputSimple
	seen := map[string]bool{}
	var repo string
	for i := 2; i < len(args); {
		flag := args[i]
		key := flag
		if flag == "-r" {
			key = "--repo"
		}
		if flag == "-o" {
			key = "--output"
		}
		if seen[key] || strings.Contains(flag, "=") {
			return command{}, fmt.Errorf("duplicate or invalid flag")
		}
		seen[key] = true
		if key == "--comments" {
			req.IncludeComments = true
			i++
			continue
		}
		if i+1 >= len(args) {
			return command{}, fmt.Errorf("missing flag value")
		}
		value := args[i+1]
		switch key {
		case "--repo":
			repo = value
		case "--output":
			var err error
			format, err = parseOutput(value, outputSimple)
			if err != nil {
				return command{}, err
			}
		default:
			return command{}, fmt.Errorf("unsupported flag")
		}
		i += 2
	}
	if !seen["--repo"] {
		return command{}, fmt.Errorf("missing repository")
	}
	owner, name, ok := parseSlug(repo)
	if !ok {
		return command{}, fmt.Errorf("invalid repository")
	}
	return command{request: requestFor(owner, name, &repowolfv1.GiteaRequest_IssueView{IssueView: req}), format: format}, nil
}

func requestFor(owner, name string, operation any) *repowolfv1.GiteaRequest {
	r := &repowolfv1.GiteaRequest{Context: &repowolfv1.RequestContext{Repository: &repowolfv1.RepositorySelector{Owner: owner, Name: name}}}
	switch value := operation.(type) {
	case *repowolfv1.GiteaRequest_RepositoryView:
		r.Operation = value
	case *repowolfv1.GiteaRequest_IssueList:
		r.Operation = value
	case *repowolfv1.GiteaRequest_IssueView:
		r.Operation = value
	}
	return r
}

func parseIssueFields(value string) ([]repowolfv1.GiteaIssueField, error) {
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid fields")
	}
	out := make([]repowolfv1.GiteaIssueField, 0, len(parts))
	seen := map[repowolfv1.GiteaIssueField]bool{}
	for _, part := range parts {
		field, ok := issueFieldByName[part]
		if !ok || seen[field] {
			return nil, fmt.Errorf("invalid fields")
		}
		seen[field] = true
		out = append(out, field)
	}
	return out, nil
}
func parsePositive(value string, maximum int64) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 || n > maximum {
		return 0, fmt.Errorf("invalid positive integer")
	}
	return n, nil
}
func validateFilter(value string) error {
	if value == "" || len(value) > maximumFilterBytes || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("invalid filter")
	}
	return nil
}
func parseOutput(value string, _ outputFormat) (outputFormat, error) {
	switch value {
	case "simple":
		return outputSimple, nil
	case "table":
		return outputTable, nil
	case "json":
		return outputJSON, nil
	default:
		return 0, fmt.Errorf("invalid output")
	}
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
