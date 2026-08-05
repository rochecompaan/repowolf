package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
)

type apiUser struct {
	Login *string `json:"login"`
}
type apiLabel struct {
	Name *string `json:"name"`
}
type apiIssue struct {
	Number      *uint64         `json:"number"`
	Title       *string         `json:"title"`
	Body        json.RawMessage `json:"body"`
	State       *string         `json:"state"`
	User        *apiUser        `json:"user"`
	Assignees   *[]apiUser      `json:"assignees"`
	Labels      *[]apiLabel     `json:"labels"`
	URL         *string         `json:"html_url"`
	CreatedAt   *string         `json:"created_at"`
	UpdatedAt   *string         `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}
type apiComment struct {
	ID        *uint64  `json:"id"`
	User      *apiUser `json:"user"`
	Body      *string  `json:"body"`
	URL       *string  `json:"html_url"`
	CreatedAt *string  `json:"created_at"`
	UpdatedAt *string  `json:"updated_at"`
}
type apiRef struct {
	Ref *string `json:"ref"`
	SHA *string `json:"sha"`
}
type apiPull struct {
	Number         *uint64         `json:"number"`
	Title          *string         `json:"title"`
	Body           json.RawMessage `json:"body"`
	State          *string         `json:"state"`
	Draft          *bool           `json:"draft"`
	User           *apiUser        `json:"user"`
	Head           *apiRef         `json:"head"`
	Base           *apiRef         `json:"base"`
	URL            *string         `json:"html_url"`
	CreatedAt      *string         `json:"created_at"`
	UpdatedAt      *string         `json:"updated_at"`
	MergeableState *string         `json:"mergeable_state"`
}
type apiRun struct {
	ID           *uint64 `json:"id"`
	WorkflowName *string `json:"name"`
	DisplayTitle *string `json:"display_title"`
	Status       *string `json:"status"`
	Conclusion   *string `json:"conclusion"`
	Event        *string `json:"event"`
	HeadBranch   *string `json:"head_branch"`
	HeadSHA      *string `json:"head_sha"`
	URL          *string `json:"html_url"`
	CreatedAt    *string `json:"created_at"`
	UpdatedAt    *string `json:"updated_at"`
	Attempt      *uint64 `json:"run_attempt"`
	JobsURL      *string `json:"jobs_url"`
}
type apiRepository struct {
	Name          *string  `json:"name"`
	Owner         *apiUser `json:"owner"`
	FullName      *string  `json:"full_name"`
	Description   *string  `json:"description"`
	Private       *bool    `json:"private"`
	URL           *string  `json:"html_url"`
	DefaultBranch *string  `json:"default_branch"`
}
type apiStatus struct {
	Context     *string `json:"context"`
	State       *string `json:"state"`
	Description *string `json:"description"`
	TargetURL   *string `json:"target_url"`
	CreatedAt   *string `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`
}
type apiStatuses struct {
	State    *string      `json:"state"`
	SHA      *string      `json:"sha"`
	Statuses *[]apiStatus `json:"statuses"`
}
type apiCheckOutput struct {
	Title *string `json:"title"`
}
type apiCheckRun struct {
	Name        *string         `json:"name"`
	Status      *string         `json:"status"`
	Conclusion  *string         `json:"conclusion"`
	DetailsURL  *string         `json:"details_url"`
	Output      *apiCheckOutput `json:"output"`
	StartedAt   *string         `json:"started_at"`
	CompletedAt *string         `json:"completed_at"`
}

func normalize(request *repowolfv1.GitHubRequest, kind string, raw []byte) (*repowolfv1.GitHubResponse, error) {
	switch kind {
	case "repository":
		var value apiRepository
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := repositoryRecord(value)
		if err != nil {
			return nil, err
		}
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_RepositoryView{RepositoryView: &repowolfv1.GitHubRepositoryViewResult{Repository: record}}}, nil
	case "issue_list":
		var value struct {
			Items *[]apiIssue `json:"items"`
		}
		if err := decode(raw, &value); err != nil || value.Items == nil {
			return nil, providerResponse(err, "items")
		}
		records, err := issueRecords(*value.Items, false)
		if err != nil {
			return nil, err
		}
		return issueListResponse(records), nil
	case "issue":
		var value apiIssue
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := issueRecord(value, true)
		if err != nil {
			return nil, err
		}
		return issueResponse(request, record)
	case "comment":
		var value apiComment
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := commentRecord(value)
		if err != nil {
			return nil, err
		}
		return commentResponse(request, record)
	case "pull_list":
		var values []apiPull
		if err := decode(raw, &values); err != nil {
			return nil, err
		}
		records, err := pullRecords(values, false)
		if err != nil {
			return nil, err
		}
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullList{PullList: &repowolfv1.GitHubPullListResult{Pulls: records}}}, nil
	case "pull":
		var value apiPull
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := pullRecord(value, true)
		if err != nil {
			return nil, err
		}
		return pullResponse(request, record)
	case "run_list":
		var value struct {
			Runs *[]apiRun `json:"workflow_runs"`
		}
		if err := decode(raw, &value); err != nil || value.Runs == nil {
			return nil, providerResponse(err, "workflow_runs")
		}
		records, err := runRecords(*value.Runs, false)
		if err != nil {
			return nil, err
		}
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_RunList{RunList: &repowolfv1.GitHubRunListResult{Runs: records}}}, nil
	case "run":
		var value apiRun
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := runRecord(value, true)
		if err != nil {
			return nil, err
		}
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_RunView{RunView: &repowolfv1.GitHubRunViewResult{Run: record}}}, nil
	case "status":
		var value apiStatuses
		if err := decode(raw, &value); err != nil {
			return nil, err
		}
		record, err := statusSummary(value)
		if err != nil {
			return nil, err
		}
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_StatusView{StatusView: &repowolfv1.GitHubStatusViewResult{Status: record}}}, nil
	default:
		return nil, ErrInvalidRequest
	}
}

func decode(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(value); err != nil {
		return providerResponse(err, "json")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return providerResponse(nil, "json")
	}
	return nil
}
func providerResponse(cause error, field string) error {
	if cause != nil {
		return fmt.Errorf("invalid GitHub response %s: %w", field, cause)
	}
	return fmt.Errorf("invalid GitHub response: missing %s", field)
}
func required(value *string, field string) (string, error) {
	if value == nil {
		return "", providerResponse(nil, field)
	}
	return *value, nil
}
func requiredID(value *uint64, field string) (uint64, error) {
	if value == nil {
		return 0, providerResponse(nil, field)
	}
	return *value, nil
}
func requiredBool(value *bool, field string) (bool, error) {
	if value == nil {
		return false, providerResponse(nil, field)
	}
	return *value, nil
}
func nullable(raw json.RawMessage, field string) (*string, error) {
	if len(raw) == 0 {
		return nil, providerResponse(nil, field)
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, providerResponse(err, field)
	}
	return &value, nil
}
func userLogin(value *apiUser, field string) (string, error) {
	if value == nil {
		return "", providerResponse(nil, field)
	}
	return required(value.Login, field+".login")
}
