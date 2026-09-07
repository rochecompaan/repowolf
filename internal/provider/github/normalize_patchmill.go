package github

import (
	"encoding/json"
	"net/url"
	"strings"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
)

type graphQLIssueListResponse struct {
	Data   *graphQLIssueListData `json:"data"`
	Errors []json.RawMessage     `json:"errors"`
}

type graphQLIssueListData struct {
	Repository *graphQLIssueRepository `json:"repository"`
}

type graphQLIssueRepository struct {
	Issues *graphQLIssueConnection `json:"issues"`
}

type graphQLIssueConnection struct {
	Nodes    *[]graphQLIssue  `json:"nodes"`
	PageInfo *graphQLPageInfo `json:"pageInfo"`
}

type graphQLPageInfo struct {
	HasNextPage *bool   `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type graphQLIssue struct {
	Number    *uint64                 `json:"number"`
	Title     *string                 `json:"title"`
	Body      *string                 `json:"body"`
	State     *string                 `json:"state"`
	Labels    *graphQLLabelConnection `json:"labels"`
	Author    *apiUser                `json:"author"`
	CreatedAt *string                 `json:"createdAt"`
	UpdatedAt *string                 `json:"updatedAt"`
	URL       *string                 `json:"url"`
}

type graphQLLabelConnection struct {
	Nodes    *[]apiLabel      `json:"nodes"`
	PageInfo *graphQLPageInfo `json:"pageInfo"`
}

type issueGraphQLPage struct {
	records     []*repowolfv1.GitHubIssueRecord
	hasNextPage bool
	endCursor   *string
}

func normalizeIssueGraphQLPage(raw []byte, first int) (issueGraphQLPage, error) {
	var response graphQLIssueListResponse
	if err := decode(raw, &response); err != nil {
		return issueGraphQLPage{}, err
	}
	if len(response.Errors) > 0 {
		return issueGraphQLPage{}, providerResponse(nil, "GraphQL errors")
	}
	if response.Data == nil || response.Data.Repository == nil || response.Data.Repository.Issues == nil {
		return issueGraphQLPage{}, providerResponse(nil, "issues data")
	}
	connection := response.Data.Repository.Issues
	if connection.Nodes == nil || connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil {
		return issueGraphQLPage{}, providerResponse(nil, "issues page")
	}
	if len(*connection.Nodes) > first {
		return issueGraphQLPage{}, providerResponse(nil, "issues page size")
	}
	records := make([]*repowolfv1.GitHubIssueRecord, 0, len(*connection.Nodes))
	for _, issue := range *connection.Nodes {
		record, err := graphQLIssueRecord(issue)
		if err != nil {
			return issueGraphQLPage{}, err
		}
		records = append(records, record)
	}
	return issueGraphQLPage{records: records, hasNextPage: *connection.PageInfo.HasNextPage, endCursor: connection.PageInfo.EndCursor}, nil
}

func graphQLIssueRecord(value graphQLIssue) (*repowolfv1.GitHubIssueRecord, error) {
	number, err := requiredID(value.Number, "issue.number")
	if err != nil {
		return nil, err
	}
	title, err := required(value.Title, "issue.title")
	if err != nil {
		return nil, err
	}
	body, err := required(value.Body, "issue.body")
	if err != nil {
		return nil, err
	}
	state, err := required(value.State, "issue.state")
	if err != nil {
		return nil, err
	}
	author, err := userLogin(value.Author, "issue.author")
	if err != nil {
		return nil, err
	}
	labels, err := graphQLIssueLabels(value.Labels)
	if err != nil {
		return nil, err
	}
	createdAt, err := required(value.CreatedAt, "issue.createdAt")
	if err != nil {
		return nil, err
	}
	updatedAt, err := required(value.UpdatedAt, "issue.updatedAt")
	if err != nil {
		return nil, err
	}
	link, err := required(value.URL, "issue.url")
	if err != nil {
		return nil, err
	}
	return &repowolfv1.GitHubIssueRecord{
		Number: number, Title: title, Body: &body, State: strings.ToLower(state), Author: author,
		Assignees: []string{}, Labels: labels, Url: link, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func graphQLIssueLabels(connection *graphQLLabelConnection) ([]string, error) {
	if connection == nil || connection.Nodes == nil || connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil {
		return nil, providerResponse(nil, "issue.labels")
	}
	if *connection.PageInfo.HasNextPage {
		return nil, providerResponse(nil, "issue labels pagination")
	}
	return labelNames(connection.Nodes)
}

func currentUserResponse(value apiUser) (*repowolfv1.GitHubResponse, error) {
	login, err := required(value.Login, "login")
	if err != nil {
		return nil, err
	}
	if !githubLogin(login) {
		return nil, providerResponse(nil, "login")
	}
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserResult{User: &repowolfv1.GitHubUserRecord{Login: login}}}}, nil
}

func githubLogin(value string) bool {
	if len(value) == 0 || len(value) > 39 {
		return false
	}
	for index, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || index > 0 && character == '-' {
			continue
		}
		return false
	}
	return true
}

func labelRecord(value apiLabel) (*repowolfv1.GitHubLabelRecord, error) {
	name, err := required(value.Name, "name")
	if err != nil {
		return nil, err
	}
	if err := labelCreateName(name); err != nil {
		return nil, providerResponse(nil, "name")
	}
	return &repowolfv1.GitHubLabelRecord{Name: name}, nil
}

func labelCreateResponse(value apiLabel) (*repowolfv1.GitHubResponse, error) {
	record, err := labelRecord(value)
	if err != nil {
		return nil, err
	}
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_LabelCreate{LabelCreate: &repowolfv1.GitHubLabelCreateResult{Label: record}}}, nil
}

func repositoryTarget(record *repowolfv1.GitHubRepositoryRecord, repository policy.ResolvedRepository) error {
	if record.GetRepository() != repository.Repository.Name || record.GetOwner() != repository.Repository.Owner || record.GetNameWithOwner() != repository.Repository.Owner+"/"+repository.Repository.Name {
		return providerResponse(nil, "repository")
	}
	expectedPath := "/" + repository.Repository.Owner + "/" + repository.Repository.Name
	parsed, err := url.Parse(record.GetUrl())
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, repository.Provider.APIHost) || parsed.User != nil || parsed.EscapedPath() != expectedPath || parsed.RawQuery != "" || parsed.Fragment != "" {
		return providerResponse(nil, "html_url")
	}
	expectedSSHURL := repository.Provider.SSHUser + "@" + repository.Provider.GitHost + ":" + repository.Repository.Owner + "/" + repository.Repository.Name + ".git"
	if record.GetSshUrl() != expectedSSHURL {
		return providerResponse(nil, "ssh_url")
	}
	return nil
}
