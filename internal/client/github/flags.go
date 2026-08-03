package github

import (
	"fmt"
	"strconv"
	"strings"
)

type flagKind byte

const (
	valueFlag flagKind = iota
	boolFlag
)

type parsedFlags struct {
	values map[string]string
	bools  map[string]bool
}

func parseFlags(args []string, allowed map[string]flagKind) (parsedFlags, error) {
	parsed := parsedFlags{values: make(map[string]string), bools: make(map[string]bool)}
	seen := make(map[string]struct{}, len(args))
	for index := 0; index < len(args); index++ {
		name := args[index]
		kind, ok := allowed[name]
		if !ok || !strings.HasPrefix(name, "--") || strings.Contains(name, "=") {
			return parsedFlags{}, fmt.Errorf("unsupported argument %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return parsedFlags{}, fmt.Errorf("duplicate flag %s", name)
		}
		seen[name] = struct{}{}
		if kind == boolFlag {
			parsed.bools[name] = true
			continue
		}
		index++
		if index == len(args) || strings.HasPrefix(args[index], "--") {
			return parsedFlags{}, fmt.Errorf("flag %s requires a value", name)
		}
		parsed.values[name] = args[index]
	}
	return parsed, nil
}

func operationFlags(values ...string) map[string]flagKind {
	allowed := map[string]flagKind{"--repo": valueFlag, "--json": valueFlag}
	for _, name := range values {
		allowed[name] = valueFlag
	}
	return allowed
}

func operationFlagsWithBools(values []string, bools ...string) map[string]flagKind {
	allowed := operationFlags(values...)
	for _, name := range bools {
		allowed[name] = boolFlag
	}
	return allowed
}

func positiveDecimal(value string) (uint64, error) {
	if value == "" || value[0] == '+' || value[0] == '-' || len(value) > 1 && value[0] == '0' {
		return 0, fmt.Errorf("expected a positive decimal integer")
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("expected a positive decimal integer")
	}
	return number, nil
}

func valueOr(flags parsedFlags, name, fallback string) string {
	if value, ok := flags.values[name]; ok {
		return value
	}
	return fallback
}

func fieldSet(names ...string) map[string]struct{} {
	fields := make(map[string]struct{}, len(names))
	for _, name := range names {
		fields[name] = struct{}{}
	}
	return fields
}

var responseFields = map[operationKind]map[string]struct{}{
	operationRepositoryView: fieldSet("repository", "owner", "nameWithOwner", "description", "private", "url", "defaultBranch"),
	operationIssueList:      fieldSet("number", "title", "state", "author", "assignees", "labels", "url", "createdAt", "updatedAt"),
	operationIssueView:      issueFields(), operationIssueCreate: issueFields(), operationIssueEdit: issueFields(),
	operationIssueClose: issueFields(), operationIssueReopen: issueFields(),
	operationIssueComment: fieldSet("id", "author", "body", "url", "createdAt", "updatedAt"),
	operationPullList:     pullFields(false), operationPullView: pullFields(true), operationPullCreate: pullFields(true),
	operationPullEdit: pullFields(true), operationPullClose: pullFields(true), operationPullReopen: pullFields(true), operationPullReady: pullFields(true),
	operationPullComment: fieldSet("id", "author", "body", "url", "createdAt", "updatedAt"),
	operationPullChecks:  fieldSet("name", "state", "conclusion", "detailsUrl", "description", "startedAt", "completedAt"),
	operationRunList:     runFields(false), operationRunView: runFields(true),
	operationStatusView: fieldSet("state", "objectId", "statuses"),
}

func issueFields() map[string]struct{} {
	return fieldSet("number", "title", "body", "state", "author", "assignees", "labels", "url", "createdAt", "updatedAt")
}
func pullFields(details bool) map[string]struct{} {
	fields := fieldSet("number", "title", "body", "state", "draft", "author", "head", "base", "headObjectId", "url", "createdAt", "updatedAt")
	if details {
		fields["mergeableState"] = struct{}{}
	}
	return fields
}
func runFields(details bool) map[string]struct{} {
	fields := fieldSet("id", "name", "workflowName", "status", "conclusion", "event", "headBranch", "headObjectId", "url", "createdAt", "updatedAt")
	if details {
		fields["attempt"] = struct{}{}
		fields["jobsUrl"] = struct{}{}
	}
	return fields
}
