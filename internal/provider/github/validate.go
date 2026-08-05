package github

import (
	"fmt"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

const (
	maximumTitleBytes = 256
	maximumBodyBytes  = 65_536
	maximumNameBytes  = 255
	maximumListLimit  = 100
)

// ValidateGitHubRequest validates only typed operation data and has no effects.
func ValidateGitHubRequest(request *repowolfv1.GitHubRequest) error {
	if request == nil {
		return invalid("request")
	}
	var err error
	switch operation := request.Operation.(type) {
	case *repowolfv1.GitHubRequest_RepositoryView:
		if operation.RepositoryView == nil {
			err = invalid("operation")
		}
	case *repowolfv1.GitHubRequest_IssueList:
		err = validateIssueList(operation.IssueList)
	case *repowolfv1.GitHubRequest_IssueView:
		err = number(operation.IssueView.GetNumber())
	case *repowolfv1.GitHubRequest_IssueCreate:
		err = validateIssueCreate(operation.IssueCreate)
	case *repowolfv1.GitHubRequest_IssueEdit:
		err = validateIssueEdit(operation.IssueEdit)
	case *repowolfv1.GitHubRequest_IssueComment:
		err = validateComment(operation.IssueComment.GetNumber(), operation.IssueComment.GetBody())
	case *repowolfv1.GitHubRequest_IssueClose:
		err = number(operation.IssueClose.GetNumber())
	case *repowolfv1.GitHubRequest_IssueReopen:
		err = number(operation.IssueReopen.GetNumber())
	case *repowolfv1.GitHubRequest_PullList:
		err = validatePullList(operation.PullList)
	case *repowolfv1.GitHubRequest_PullView:
		err = number(operation.PullView.GetNumber())
	case *repowolfv1.GitHubRequest_PullCreate:
		err = validatePullCreate(operation.PullCreate)
	case *repowolfv1.GitHubRequest_PullEdit:
		err = validatePullEdit(operation.PullEdit)
	case *repowolfv1.GitHubRequest_PullComment:
		err = validateComment(operation.PullComment.GetNumber(), operation.PullComment.GetBody())
	case *repowolfv1.GitHubRequest_PullClose:
		err = number(operation.PullClose.GetNumber())
	case *repowolfv1.GitHubRequest_PullReopen:
		err = number(operation.PullReopen.GetNumber())
	case *repowolfv1.GitHubRequest_PullReady:
		err = number(operation.PullReady.GetNumber())
	case *repowolfv1.GitHubRequest_PullChecks:
		err = number(operation.PullChecks.GetNumber())
	case *repowolfv1.GitHubRequest_RunList:
		err = validateRunList(operation.RunList)
	case *repowolfv1.GitHubRequest_RunView:
		err = number(operation.RunView.GetRunId())
	case *repowolfv1.GitHubRequest_StatusView:
		err = objectID(operation.StatusView.GetObjectId())
	default:
		err = invalid("operation")
	}
	return err
}

func validateIssueList(value *repowolfv1.GitHubIssueListRequest) error {
	if value == nil || value.State < repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN || value.State > repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL {
		return invalid("state")
	}
	return limit(value.Limit)
}
func validateIssueCreate(value *repowolfv1.GitHubIssueCreateRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := title(value.Title); err != nil {
		return err
	}
	if value.Body != nil {
		if err := body(*value.Body, false); err != nil {
			return err
		}
	}
	if err := labels(value.Labels); err != nil {
		return err
	}
	return names(value.Assignees)
}
func validateIssueEdit(value *repowolfv1.GitHubIssueEditRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := number(value.Number); err != nil {
		return err
	}
	if value.Title == nil && value.Body == nil && value.Labels == nil && value.Assignees == nil {
		return invalid("empty edit")
	}
	if value.Title != nil {
		if err := title(*value.Title); err != nil {
			return err
		}
	}
	if value.Body != nil {
		if err := body(*value.Body, false); err != nil {
			return err
		}
	}
	if value.Labels != nil {
		if err := labels(value.Labels.Values); err != nil {
			return err
		}
	}
	if value.Assignees != nil {
		return names(value.Assignees.Values)
	}
	return nil
}
func validatePullList(value *repowolfv1.GitHubPullListRequest) error {
	if value == nil || value.State < repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_OPEN || value.State > repowolfv1.GitHubPullState_GIT_HUB_PULL_STATE_ALL {
		return invalid("state")
	}
	if err := limit(value.Limit); err != nil {
		return err
	}
	for _, item := range []*string{value.Base, value.Head} {
		if item != nil {
			if err := name(*item, false); err != nil {
				return err
			}
		}
	}
	return nil
}
func validatePullCreate(value *repowolfv1.GitHubPullCreateRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := title(value.Title); err != nil {
		return err
	}
	if err := name(value.Head, true); err != nil {
		return err
	}
	if err := name(value.Base, true); err != nil {
		return err
	}
	if value.Body != nil {
		return body(*value.Body, false)
	}
	return nil
}
func validatePullEdit(value *repowolfv1.GitHubPullEditRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := number(value.Number); err != nil {
		return err
	}
	if value.Title == nil && value.Body == nil && value.Base == nil {
		return invalid("empty edit")
	}
	if value.Title != nil {
		if err := title(*value.Title); err != nil {
			return err
		}
	}
	if value.Body != nil {
		if err := body(*value.Body, false); err != nil {
			return err
		}
	}
	if value.Base != nil {
		return name(*value.Base, true)
	}
	return nil
}
func validateComment(numberValue uint64, value string) error {
	if err := number(numberValue); err != nil {
		return err
	}
	return body(value, true)
}
func validateRunList(value *repowolfv1.GitHubRunListRequest) error {
	if value == nil {
		return invalid("operation")
	}
	if err := limit(value.Limit); err != nil {
		return err
	}
	if value.Branch != nil {
		if err := name(*value.Branch, false); err != nil {
			return err
		}
	}
	if value.Status != nil && (*value.Status < repowolfv1.GitHubRunStatus_GIT_HUB_RUN_STATUS_QUEUED || *value.Status > repowolfv1.GitHubRunStatus_GIT_HUB_RUN_STATUS_PENDING) {
		return invalid("status")
	}
	return nil
}
func number(value uint64) error {
	if value == 0 {
		return invalid("number")
	}
	return nil
}
func limit(value uint64) error {
	if value == 0 || value > maximumListLimit {
		return invalid("limit")
	}
	return nil
}
func title(value string) error {
	if len(value) == 0 || len(value) > maximumTitleBytes || !utf8.ValidString(value) {
		return invalid("title")
	}
	return nil
}
func body(value string, required bool) error {
	if required && value == "" || len(value) > maximumBodyBytes || !utf8.ValidString(value) {
		return invalid("body")
	}
	return nil
}
func names(values []string) error {
	if len(values) > 100 {
		return invalid("names")
	}
	for _, value := range values {
		if err := name(value, true); err != nil {
			return err
		}
	}
	return nil
}
func labels(values []string) error {
	if len(values) > 100 {
		return invalid("labels")
	}
	for _, value := range values {
		if value == "" || len(value) > maximumNameBytes || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
			return invalid("label")
		}
	}
	return nil
}
func name(value string, required bool) error {
	if required && value == "" || len(value) > maximumNameBytes || !utf8.ValidString(value) || strings.ContainsAny(value, " \t\r\n\x00") {
		return invalid("name")
	}
	return nil
}
func objectID(value string) error {
	if len(value) != 40 && len(value) != 64 {
		return invalid("object id")
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return invalid("object id")
		}
	}
	return nil
}
func invalid(field string) error { return fmt.Errorf("%w: %s", ErrInvalidRequest, field) }
