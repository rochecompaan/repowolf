package github

import (
	"fmt"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

func parseLabel(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("label subcommand is required")
	}
	switch args[0] {
	case "list":
		return parseLabelList(args[1:])
	case "create":
		return parseLabelCreate(args[1:])
	default:
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("unsupported label operation")
	}
}

func parseLabelList(args []string) (any, parsedFlags, operationKind, error) {
	flags, err := parseFlags(args, operationFlags("--limit"))
	limit, present := flags.values["--limit"]
	if err == nil && !present {
		err = fmt.Errorf("--limit is required")
	}
	if err == nil && flags.values["--json"] != "name" {
		err = fmt.Errorf("--json name is required")
	}
	parsedLimit, limitErr := listLimit(limit, 1000)
	err = firstError(err, limitErr)
	return &repowolfv1.GitHubRequest_LabelList{LabelList: &repowolfv1.GitHubLabelListRequest{Limit: parsedLimit}}, flags, operationLabelList, err
}

func parseLabelCreate(args []string) (any, parsedFlags, operationKind, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return nil, parsedFlags{}, operationUnknown, fmt.Errorf("label name is required")
	}
	name, rest := args[0], args[1:]
	flags, err := parseFlags(rest, operationFlags("--color", "--description"))
	color, colorPresent := flags.values["--color"]
	description, descriptionPresent := flags.values["--description"]
	if err == nil && !validLabelName(name) {
		err = fmt.Errorf("invalid label name")
	}
	if err == nil && (!colorPresent || !validLabelColor(color)) {
		err = fmt.Errorf("valid --color is required")
	}
	if err == nil && (!descriptionPresent || !validLabelDescription(description)) {
		err = fmt.Errorf("valid --description is required")
	}
	request := &repowolfv1.GitHubLabelCreateRequest{Name: name, Color: color, Description: description}
	return &repowolfv1.GitHubRequest_LabelCreate{LabelCreate: request}, flags, operationLabelCreate, err
}

func labelCSV(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) > 100 {
		return nil, fmt.Errorf("too many labels")
	}
	seen := make(map[string]struct{}, len(parts))
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		label := strings.Trim(part, " \t")
		if label == "" || len(label) > 255 || !utf8.ValidString(label) || strings.IndexByte(label, 0) >= 0 {
			return nil, fmt.Errorf("invalid label list")
		}
		if _, duplicate := seen[label]; duplicate {
			return nil, fmt.Errorf("duplicate label")
		}
		seen[label] = struct{}{}
		labels = append(labels, label)
	}
	return labels, nil
}

func validLabelName(value string) bool {
	return value != "" && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0 && utf8.RuneCountInString(value) <= 50
}

func validLabelColor(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func validLabelDescription(value string) bool {
	return utf8.ValidString(value) && strings.IndexByte(value, 0) < 0 && utf8.RuneCountInString(value) <= 100
}
