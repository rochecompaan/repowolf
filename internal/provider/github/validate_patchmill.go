package github

import (
	"strings"
	"unicode"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const (
	maximumIssueListLimit = 1_001
	maximumLabelListLimit = 1_000
)

func validateCurrentUser(value *repowolfv1.GitHubCurrentUserRequest) error {
	if value == nil {
		return invalid("operation")
	}
	return nil
}

func validateLabelList(value *repowolfv1.GitHubLabelListRequest) error {
	if value == nil {
		return invalid("operation")
	}
	return limit(value.Limit, maximumLabelListLimit)
}

func validateLabelCreate(value *repowolfv1.GitHubLabelCreateRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := labelCreateName(value.Name); err != nil {
		return err
	}
	if err := labelDescription(value.Description); err != nil {
		return err
	}
	return labelColor(value.Color)
}

func validateIssueLabelChange(value *repowolfv1.GitHubIssueLabelChangeRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := number(value.Number); err != nil {
		return err
	}
	if len(value.AddLabels) == 0 && len(value.RemoveLabels) == 0 {
		return invalid("empty label change")
	}
	seen := make(map[string]struct{}, len(value.AddLabels)+len(value.RemoveLabels))
	if err := distinctLabels(value.AddLabels, seen); err != nil {
		return err
	}
	return distinctLabels(value.RemoveLabels, seen)
}

func labelCreateName(value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 50 {
		return invalid("label name")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return invalid("label name")
		}
	}
	return nil
}

func labelDescription(value string) error {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 || utf8.RuneCountInString(value) > 100 {
		return invalid("label description")
	}
	return nil
}

func labelColor(value string) error {
	if len(value) != 6 {
		return invalid("label color")
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return invalid("label color")
		}
	}
	return nil
}

func distinctLabels(values []string, seen map[string]struct{}) error {
	if err := labels(values); err != nil {
		return err
	}
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return invalid("duplicate label")
		}
		seen[value] = struct{}{}
	}
	return nil
}
