package github

import (
	"context"
	"encoding/json"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/runner"
	"google.golang.org/protobuf/proto"
)

const issueListQuery = `query IssueList($owner: String!, $name: String!, $states: [IssueState!], $cursor: String, $first: Int!) {
  repository(owner: $owner, name: $name) {
    issues(first: $first, after: $cursor, states: $states, orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        title
        body
        state
        labels(first: 100) { nodes { name } pageInfo { hasNextPage } }
        author { login }
        createdAt
        updatedAt
        url
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

type issueGraphQLRequest struct {
	Query     string                    `json:"query"`
	Variables issueGraphQLRequestValues `json:"variables"`
}

type issueGraphQLRequestValues struct {
	Owner  string   `json:"owner"`
	Name   string   `json:"name"`
	States []string `json:"states"`
	Cursor *string  `json:"cursor"`
	First  int      `json:"first"`
}

func (adapter *Adapter) executeIssueList(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GitHubRequest) (*repowolfv1.GitHubResponse, error) {
	operation := request.GetIssueList()
	budget := &aggregateBudget{limit: maximumPaginatedReadBytes}
	records := make([]*repowolfv1.GitHubIssueRecord, 0, operation.Limit)
	var cursor *string
	seenCursors := make(map[string]struct{})

	for len(records) < int(operation.Limit) {
		first := min(100, int(operation.Limit)-len(records))
		raw, err := adapter.callIssueGraphQLPage(ctx, repository, operation.State, cursor, first, budget)
		if err != nil {
			return nil, err
		}
		page, err := normalizeIssueGraphQLPage(raw, first)
		if err != nil {
			return nil, err
		}
		records = append(records, page.records...)
		if !page.hasNextPage {
			return boundedIssueListResponse(records)
		}
		if len(page.records) == 0 || page.endCursor == nil || *page.endCursor == "" {
			return nil, providerResponse(nil, "issues pagination")
		}
		if _, exists := seenCursors[*page.endCursor]; exists {
			return nil, providerResponse(nil, "issues cursor")
		}
		seenCursors[*page.endCursor] = struct{}{}
		cursor = page.endCursor
	}
	return boundedIssueListResponse(records)
}

func (adapter *Adapter) callIssueGraphQLPage(
	ctx context.Context,
	repository policy.ResolvedRepository,
	state repowolfv1.GitHubIssueState,
	cursor *string,
	first int,
	budget *aggregateBudget,
) ([]byte, error) {
	var states []string
	if state != repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_ALL {
		states = []string{map[repowolfv1.GitHubIssueState]string{
			repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_OPEN:   "OPEN",
			repowolfv1.GitHubIssueState_GIT_HUB_ISSUE_STATE_CLOSED: "CLOSED",
		}[state]}
	}
	body, err := json.Marshal(issueGraphQLRequest{
		Query: issueListQuery,
		Variables: issueGraphQLRequestValues{
			Owner: repository.Repository.Owner, Name: repository.Repository.Name,
			States: states, Cursor: cursor, First: first,
		},
	})
	if err != nil {
		return nil, err
	}
	command := adapter.apiCommand(repository.Provider.APIHost, "POST", "graphql", json.RawMessage(body), maximumPaginatedReadBytes)
	result, err := adapter.callBudgeted(ctx, command, budget)
	if err != nil {
		return nil, err
	}
	return result.Stdout, nil
}

func boundedIssueListResponse(records []*repowolfv1.GitHubIssueRecord) (*repowolfv1.GitHubResponse, error) {
	response := issueListResponse(records)
	if proto.Size(response) > maximumResponseBytes {
		return nil, runner.ErrOutputLimit
	}
	return response, nil
}
