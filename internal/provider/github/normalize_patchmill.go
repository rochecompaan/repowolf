package github

import (
	"net/url"
	"strings"
	"unicode/utf8"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
)

func currentUserResponse(value apiUser) (*repowolfv1.GitHubResponse, error) {
	login, err := required(value.Login, "login")
	if err != nil {
		return nil, err
	}
	if login == "" || len(login) > maximumNameBytes || !utf8.ValidString(login) {
		return nil, providerResponse(nil, "login")
	}
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserResult{User: &repowolfv1.GitHubUserRecord{Login: login}}}}, nil
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
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, repository.Provider.GitHost) || parsed.User != nil || parsed.EscapedPath() != expectedPath || parsed.RawQuery != "" || parsed.Fragment != "" {
		return providerResponse(nil, "html_url")
	}
	expectedSSHURL := repository.Provider.SSHUser + "@" + repository.Provider.GitHost + ":" + repository.Repository.Owner + "/" + repository.Repository.Name + ".git"
	if record.GetSshUrl() != expectedSSHURL {
		return providerResponse(nil, "ssh_url")
	}
	return nil
}
