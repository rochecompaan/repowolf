package gitea

import (
	"errors"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
	"strings"
	"unicode/utf8"
)

var ErrInvalidRequest = errors.New("invalid Gitea request")

func ValidateRequest(request *repowolfv1.GiteaRequest) error {
	if request == nil {
		return ErrInvalidRequest
	}
	switch {
	case request.GetRepositoryView() != nil:
		return nil
	case request.GetIssueList() != nil:
		return validateIssueList(request.GetIssueList())
	case request.GetIssueView() != nil:
		if request.GetIssueView().Index <= 0 {
			return ErrInvalidRequest
		}
		return nil
	default:
		return ErrInvalidRequest
	}
}
func validateIssueList(r *repowolfv1.GiteaIssueListRequest) error {
	if r.State < 1 || r.State > 3 || r.Page <= 0 || r.Limit <= 0 || r.Limit > 50 || len(r.Fields) == 0 {
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
	if request.GetRepositoryView() != nil {
		return config.RepositoryRead, nil
	}
	return config.IssuesRead, nil
}
func OperationName(request *repowolfv1.GiteaRequest) (string, error) {
	if err := ValidateRequest(request); err != nil {
		return "", err
	}
	switch {
	case request.GetRepositoryView() != nil:
		return "gitea.repository_view", nil
	case request.GetIssueList() != nil:
		return "gitea.issue_list", nil
	default:
		return "gitea.issue_view", nil
	}
}
