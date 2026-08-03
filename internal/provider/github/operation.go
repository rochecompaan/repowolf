// Package github implements the fixed typed GitHub provider operation set.
package github

import (
	"errors"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/config"
)

var ErrInvalidRequest = errors.New("invalid GitHub request")

// Capability returns the one canonical capability required by request.
func Capability(request *repowolfv1.GitHubRequest) (config.Capability, error) {
	if request == nil {
		return "", ErrInvalidRequest
	}
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_RepositoryView:
		return config.RepositoryRead, nil
	case *repowolfv1.GitHubRequest_IssueList, *repowolfv1.GitHubRequest_IssueView:
		return config.IssuesRead, nil
	case *repowolfv1.GitHubRequest_IssueCreate, *repowolfv1.GitHubRequest_IssueEdit,
		*repowolfv1.GitHubRequest_IssueComment, *repowolfv1.GitHubRequest_IssueClose,
		*repowolfv1.GitHubRequest_IssueReopen:
		return config.IssuesWrite, nil
	case *repowolfv1.GitHubRequest_PullList, *repowolfv1.GitHubRequest_PullView:
		return config.PullRequestsRead, nil
	case *repowolfv1.GitHubRequest_PullCreate, *repowolfv1.GitHubRequest_PullEdit,
		*repowolfv1.GitHubRequest_PullComment, *repowolfv1.GitHubRequest_PullClose,
		*repowolfv1.GitHubRequest_PullReopen, *repowolfv1.GitHubRequest_PullReady:
		return config.PullRequestsWrite, nil
	case *repowolfv1.GitHubRequest_PullChecks, *repowolfv1.GitHubRequest_StatusView:
		return config.StatusesRead, nil
	case *repowolfv1.GitHubRequest_RunList, *repowolfv1.GitHubRequest_RunView:
		return config.ActionsRead, nil
	default:
		return "", ErrInvalidRequest
	}
}

// OperationName returns the safe canonical audit operation name.
func OperationName(request *repowolfv1.GitHubRequest) (string, error) {
	if request == nil {
		return "", ErrInvalidRequest
	}
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_RepositoryView:
		return "github.repository_view", nil
	case *repowolfv1.GitHubRequest_IssueList:
		return "github.issue_list", nil
	case *repowolfv1.GitHubRequest_IssueView:
		return "github.issue_view", nil
	case *repowolfv1.GitHubRequest_IssueCreate:
		return "github.issue_create", nil
	case *repowolfv1.GitHubRequest_IssueEdit:
		return "github.issue_edit", nil
	case *repowolfv1.GitHubRequest_IssueComment:
		return "github.issue_comment", nil
	case *repowolfv1.GitHubRequest_IssueClose:
		return "github.issue_close", nil
	case *repowolfv1.GitHubRequest_IssueReopen:
		return "github.issue_reopen", nil
	case *repowolfv1.GitHubRequest_PullList:
		return "github.pull_list", nil
	case *repowolfv1.GitHubRequest_PullView:
		return "github.pull_view", nil
	case *repowolfv1.GitHubRequest_PullCreate:
		return "github.pull_create", nil
	case *repowolfv1.GitHubRequest_PullEdit:
		return "github.pull_edit", nil
	case *repowolfv1.GitHubRequest_PullComment:
		return "github.pull_comment", nil
	case *repowolfv1.GitHubRequest_PullClose:
		return "github.pull_close", nil
	case *repowolfv1.GitHubRequest_PullReopen:
		return "github.pull_reopen", nil
	case *repowolfv1.GitHubRequest_PullReady:
		return "github.pull_ready", nil
	case *repowolfv1.GitHubRequest_PullChecks:
		return "github.pull_checks", nil
	case *repowolfv1.GitHubRequest_RunList:
		return "github.run_list", nil
	case *repowolfv1.GitHubRequest_RunView:
		return "github.run_view", nil
	case *repowolfv1.GitHubRequest_StatusView:
		return "github.status_view", nil
	default:
		return "", ErrInvalidRequest
	}
}
