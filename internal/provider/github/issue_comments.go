package github

import (
	"context"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const (
	maximumIssueCommentPages = 10
	issueCommentPageSize     = 100
)

func (adapter *Adapter) executeIssueView(
	ctx context.Context,
	repository policy.ResolvedRepository,
	request *repowolfv1.GitHubRequest,
) (*repowolfv1.GitHubResponse, error) {
	operation := request.GetIssueView()
	if !operation.IncludeComments {
		raw, err := adapter.callIssueView(ctx, repository, operation.Number, nil)
		if err != nil {
			return nil, err
		}
		return normalizeBoundedIssueView(raw)
	}

	budget := &aggregateBudget{limit: maximumPaginatedReadBytes}
	raw, err := adapter.callIssueView(ctx, repository, operation.Number, budget)
	if err != nil {
		return nil, err
	}
	issue, err := normalizeIssueViewRecord(raw)
	if err != nil {
		return nil, err
	}
	comments, err := adapter.fetchIssueComments(ctx, repository, operation.Number, budget)
	if err != nil {
		return nil, err
	}
	issue.Comments = comments
	return boundedIssueViewResponse(issue)
}

func (adapter *Adapter) callIssueView(
	ctx context.Context,
	repository policy.ResolvedRepository,
	number uint64,
	budget *aggregateBudget,
) ([]byte, error) {
	command := adapter.issueViewCommand(repository, number, outputLimit("issue"))
	var result runner.Result
	var err error
	if budget == nil {
		result, err = adapter.call(ctx, command)
	} else {
		result, err = adapter.callBudgeted(ctx, command, budget)
	}
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func (adapter *Adapter) fetchIssueComments(
	ctx context.Context,
	repository policy.ResolvedRepository,
	number uint64,
	budget *aggregateBudget,
) ([]*repowolfv1.GitHubCommentRecord, error) {
	comments := make([]*repowolfv1.GitHubCommentRecord, 0)
	for page := 1; page <= maximumIssueCommentPages; page++ {
		records, err := adapter.callIssueCommentPage(ctx, repository, number, page, issueCommentPageSize, budget)
		if err != nil {
			return nil, err
		}
		comments = append(comments, records...)
		if len(records) < issueCommentPageSize {
			return comments, nil
		}
	}

	probe, err := adapter.callIssueCommentPage(ctx, repository, number, maximumIssueCommentPages*issueCommentPageSize+1, 1, budget)
	if err != nil {
		return nil, err
	}
	if len(probe) != 0 {
		return nil, runner.ErrOutputLimit
	}
	return comments, nil
}

func (adapter *Adapter) callIssueCommentPage(
	ctx context.Context,
	repository policy.ResolvedRepository,
	number uint64,
	page, perPage int,
	budget *aggregateBudget,
) ([]*repowolfv1.GitHubCommentRecord, error) {
	command := adapter.issueCommentsCommand(repository, number, page, perPage, maximumPaginatedReadBytes)
	result, err := adapter.callBudgeted(ctx, command, budget)
	if err != nil {
		return nil, err
	}
	return normalizeIssueCommentPage(result.Stdout, perPage)
}

func normalizeBoundedIssueView(raw []byte) (*repowolfv1.GitHubResponse, error) {
	record, err := normalizeIssueViewRecord(raw)
	if err != nil {
		return nil, err
	}
	return boundedIssueViewResponse(record)
}

func normalizeIssueViewRecord(raw []byte) (*repowolfv1.GitHubIssueRecord, error) {
	if err := rejectPullIssue(raw); err != nil {
		return nil, err
	}
	var value apiIssue
	if err := decode(raw, &value); err != nil {
		return nil, err
	}
	return issueRecord(value, true)
}

func normalizeIssueCommentPage(raw []byte, maximum int) ([]*repowolfv1.GitHubCommentRecord, error) {
	var values *[]apiComment
	if err := decode(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, providerResponse(nil, "comments")
	}
	if len(*values) > maximum {
		return nil, providerResponse(nil, "comments page size")
	}
	return commentRecords(*values)
}

func boundedIssueViewResponse(record *repowolfv1.GitHubIssueRecord) (*repowolfv1.GitHubResponse, error) {
	response := issueViewResponse(record)
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}
