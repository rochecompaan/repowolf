package github

import repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"

func currentUserResponse(value apiUser) (*repowolfv1.GitHubResponse, error) {
	login, err := required(value.Login, "login")
	if err != nil {
		return nil, err
	}
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_CurrentUser{CurrentUser: &repowolfv1.GitHubCurrentUserResult{User: &repowolfv1.GitHubUserRecord{Login: login}}}}, nil
}

func labelRecord(value apiLabel) (*repowolfv1.GitHubLabelRecord, error) {
	name, err := required(value.Name, "name")
	if err != nil {
		return nil, err
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
