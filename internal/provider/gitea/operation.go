package gitea

import (
	"errors"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
)

var ErrInvalidRequest = errors.New("invalid Gitea request")

const maximumMutationBodyBytes = 64 << 10

func ValidateRequest(request *repowolfv1.GiteaRequest) error {
	if request == nil {
		return ErrInvalidRequest
	}
	switch operation := request.Operation.(type) {
	case *repowolfv1.GiteaRequest_RepositoryView:
		if operation.RepositoryView == nil {
			return ErrInvalidRequest
		}
		return nil
	case *repowolfv1.GiteaRequest_IssueList:
		return validateIssueList(operation.IssueList)
	case *repowolfv1.GiteaRequest_IssueView:
		if operation.IssueView == nil || operation.IssueView.Index <= 0 {
			return ErrInvalidRequest
		}
		return nil
	case *repowolfv1.GiteaRequest_IssueCreate:
		return validateIssueCreate(operation.IssueCreate)
	case *repowolfv1.GiteaRequest_IssueComment:
		if operation.IssueComment == nil || operation.IssueComment.Index <= 0 || !validMutationText(operation.IssueComment.Body, false, maximumMutationBodyBytes, 0) {
			return ErrInvalidRequest
		}
		return nil
	case *repowolfv1.GiteaRequest_IssueClose:
		if operation.IssueClose == nil || operation.IssueClose.Index <= 0 {
			return ErrInvalidRequest
		}
		return nil
	case *repowolfv1.GiteaRequest_IssueReopen:
		if operation.IssueReopen == nil || operation.IssueReopen.Index <= 0 {
			return ErrInvalidRequest
		}
		return nil
	case *repowolfv1.GiteaRequest_IssueEdit:
		return validateIssueEdit(operation.IssueEdit)
	default:
		return ErrInvalidRequest
	}
}

func validateIssueCreate(r *repowolfv1.GiteaIssueCreateRequest) error {
	if r == nil || !validMutationText(r.Title, false, 0, 255) || r.Description != nil && !validMutationText(r.GetDescription(), true, maximumMutationBodyBytes, 0) || !validMutationList(r.Assignees) || !validMutationList(r.Labels) {
		return ErrInvalidRequest
	}
	return nil
}

func validateIssueEdit(r *repowolfv1.GiteaIssueEditRequest) error {
	if r == nil || r.Index <= 0 || r.Title == nil && r.Description == nil && r.AssigneeAction == nil && r.LabelAction == nil {
		return ErrInvalidRequest
	}
	if r.Title != nil && !validMutationText(r.GetTitle(), false, 0, 255) || r.Description != nil && !validMutationText(r.GetDescription(), true, maximumMutationBodyBytes, 0) {
		return ErrInvalidRequest
	}
	var assignees *repowolfv1.GiteaStringList
	allowEmpty := false
	switch action := r.AssigneeAction.(type) {
	case nil:
	case *repowolfv1.GiteaIssueEditRequest_SetAssignees:
		if action != nil {
			assignees, allowEmpty = action.SetAssignees, true
		}
	case *repowolfv1.GiteaIssueEditRequest_AddAssignees:
		if action != nil {
			assignees = action.AddAssignees
		}
	case *repowolfv1.GiteaIssueEditRequest_RemoveAssignees:
		if action != nil {
			assignees = action.RemoveAssignees
		}
	default:
		return ErrInvalidRequest
	}
	if assignees != nil && (!allowEmpty && len(assignees.Values) == 0 || !validMutationList(assignees.Values)) || r.AssigneeAction != nil && assignees == nil {
		return ErrInvalidRequest
	}
	var labels *repowolfv1.GiteaStringList
	switch action := r.LabelAction.(type) {
	case nil:
	case *repowolfv1.GiteaIssueEditRequest_AddLabels:
		if action != nil {
			labels = action.AddLabels
		}
	case *repowolfv1.GiteaIssueEditRequest_RemoveLabels:
		if action != nil {
			labels = action.RemoveLabels
		}
	default:
		return ErrInvalidRequest
	}
	if labels != nil && (len(labels.Values) == 0 || !validMutationList(labels.Values)) || r.LabelAction != nil && labels == nil {
		return ErrInvalidRequest
	}
	return nil
}

func validMutationText(value string, allowEmpty bool, maxBytes, maxRunes int) bool {
	if (!allowEmpty && value == "") || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || maxBytes > 0 && len(value) > maxBytes || maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	return true
}

func validMutationList(values []string) bool {
	if len(values) > 25 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validMutationText(value, false, 0, 255) {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func validateIssueList(r *repowolfv1.GiteaIssueListRequest) error {
	if r == nil || r.State < 1 || r.State > 3 || r.Page <= 0 || r.Limit <= 0 || r.Limit > 50 || len(r.Fields) == 0 {
		return ErrInvalidRequest
	}
	for _, v := range []*string{r.Keyword, r.Author, r.Assignee, r.Mentions, r.Owner} {
		if v != nil && (*v == "" || len(*v) > 255 || !utf8.ValidString(*v) || strings.ContainsRune(*v, 0)) {
			return ErrInvalidRequest
		}
	}
	if r.From != nil && !r.From.IsValid() || r.Until != nil && !r.Until.IsValid() || r.From != nil && r.Until != nil && r.From.AsTime().After(r.Until.AsTime()) {
		return ErrInvalidRequest
	}
	seen := map[repowolfv1.GiteaIssueField]bool{}
	for _, f := range r.Fields {
		if f < 1 || f > 17 || seen[f] {
			return ErrInvalidRequest
		}
		seen[f] = true
	}
	return nil
}

func Capability(request *repowolfv1.GiteaRequest) (config.Capability, error) {
	if err := ValidateRequest(request); err != nil {
		return "", err
	}
	switch request.Operation.(type) {
	case *repowolfv1.GiteaRequest_RepositoryView:
		return config.RepositoryRead, nil
	case *repowolfv1.GiteaRequest_IssueList, *repowolfv1.GiteaRequest_IssueView:
		return config.IssuesRead, nil
	case *repowolfv1.GiteaRequest_IssueCreate, *repowolfv1.GiteaRequest_IssueComment, *repowolfv1.GiteaRequest_IssueClose, *repowolfv1.GiteaRequest_IssueReopen, *repowolfv1.GiteaRequest_IssueEdit:
		return config.IssuesWrite, nil
	default:
		return "", ErrInvalidRequest
	}
}

func OperationName(request *repowolfv1.GiteaRequest) (string, error) {
	if err := ValidateRequest(request); err != nil {
		return "", err
	}
	switch request.Operation.(type) {
	case *repowolfv1.GiteaRequest_RepositoryView:
		return "gitea.repository_view", nil
	case *repowolfv1.GiteaRequest_IssueList:
		return "gitea.issue_list", nil
	case *repowolfv1.GiteaRequest_IssueView:
		return "gitea.issue_view", nil
	case *repowolfv1.GiteaRequest_IssueCreate:
		return "gitea.issue_create", nil
	case *repowolfv1.GiteaRequest_IssueComment:
		return "gitea.issue_comment", nil
	case *repowolfv1.GiteaRequest_IssueClose:
		return "gitea.issue_close", nil
	case *repowolfv1.GiteaRequest_IssueReopen:
		return "gitea.issue_reopen", nil
	case *repowolfv1.GiteaRequest_IssueEdit:
		return "gitea.issue_edit", nil
	default:
		return "", ErrInvalidRequest
	}
}
