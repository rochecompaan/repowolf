package github

import repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"

func repositoryRecord(value apiRepository) (*repowolfv1.GitHubRepositoryRecord, error) {
	name, e := required(value.Name, "name")
	if e != nil {
		return nil, e
	}
	owner, e := userLogin(value.Owner, "owner")
	if e != nil {
		return nil, e
	}
	full, e := required(value.FullName, "full_name")
	if e != nil {
		return nil, e
	}
	private, e := requiredBool(value.Private, "private")
	if e != nil {
		return nil, e
	}
	link, e := required(value.URL, "html_url")
	if e != nil {
		return nil, e
	}
	branch, e := required(value.DefaultBranch, "default_branch")
	if e != nil {
		return nil, e
	}
	return &repowolfv1.GitHubRepositoryRecord{Repository: name, Owner: owner, NameWithOwner: full, Description: value.Description, Private: private, Url: link, DefaultBranch: branch}, nil
}
func issueRecord(value apiIssue, includeBody bool) (*repowolfv1.GitHubIssueRecord, error) {
	number, e := requiredID(value.Number, "number")
	if e != nil {
		return nil, e
	}
	title, e := required(value.Title, "title")
	if e != nil {
		return nil, e
	}
	state, e := required(value.State, "state")
	if e != nil {
		return nil, e
	}
	author, e := userLogin(value.User, "user")
	if e != nil {
		return nil, e
	}
	assignees, e := users(value.Assignees, "assignees")
	if e != nil {
		return nil, e
	}
	labels, e := labelNames(value.Labels)
	if e != nil {
		return nil, e
	}
	link, e := required(value.URL, "html_url")
	if e != nil {
		return nil, e
	}
	created, e := required(value.CreatedAt, "created_at")
	if e != nil {
		return nil, e
	}
	updated, e := required(value.UpdatedAt, "updated_at")
	if e != nil {
		return nil, e
	}
	record := &repowolfv1.GitHubIssueRecord{Number: number, Title: title, State: state, Author: author, Assignees: assignees, Labels: labels, Url: link, CreatedAt: created, UpdatedAt: updated}
	if includeBody {
		record.Body, e = nullable(value.Body, "body")
		if e != nil {
			return nil, e
		}
	}
	return record, nil
}
func issueRecords(values []apiIssue, body bool) ([]*repowolfv1.GitHubIssueRecord, error) {
	out := make([]*repowolfv1.GitHubIssueRecord, 0, len(values))
	for _, v := range values {
		record, e := issueRecord(v, body)
		if e != nil {
			return nil, e
		}
		out = append(out, record)
	}
	return out, nil
}
func commentRecord(value apiComment) (*repowolfv1.GitHubCommentRecord, error) {
	id, e := requiredID(value.ID, "id")
	if e != nil {
		return nil, e
	}
	author, e := userLogin(value.User, "user")
	if e != nil {
		return nil, e
	}
	body, e := required(value.Body, "body")
	if e != nil {
		return nil, e
	}
	link, e := required(value.URL, "html_url")
	if e != nil {
		return nil, e
	}
	created, e := required(value.CreatedAt, "created_at")
	if e != nil {
		return nil, e
	}
	updated, e := required(value.UpdatedAt, "updated_at")
	if e != nil {
		return nil, e
	}
	return &repowolfv1.GitHubCommentRecord{Id: id, Author: author, Body: body, Url: link, CreatedAt: created, UpdatedAt: updated}, nil
}
func pullRecord(value apiPull, details bool) (*repowolfv1.GitHubPullRecord, error) {
	number, e := requiredID(value.Number, "number")
	if e != nil {
		return nil, e
	}
	title, e := required(value.Title, "title")
	if e != nil {
		return nil, e
	}
	body, e := nullable(value.Body, "body")
	if e != nil {
		return nil, e
	}
	state, e := required(value.State, "state")
	if e != nil {
		return nil, e
	}
	draft, e := requiredBool(value.Draft, "draft")
	if e != nil {
		return nil, e
	}
	author, e := userLogin(value.User, "user")
	if e != nil {
		return nil, e
	}
	if value.Head == nil || value.Base == nil {
		return nil, providerResponse(nil, "head/base")
	}
	head, e := required(value.Head.Ref, "head.ref")
	if e != nil {
		return nil, e
	}
	base, e := required(value.Base.Ref, "base.ref")
	if e != nil {
		return nil, e
	}
	sha, e := required(value.Head.SHA, "head.sha")
	if e != nil {
		return nil, e
	}
	link, e := required(value.URL, "html_url")
	if e != nil {
		return nil, e
	}
	created, e := required(value.CreatedAt, "created_at")
	if e != nil {
		return nil, e
	}
	updated, e := required(value.UpdatedAt, "updated_at")
	if e != nil {
		return nil, e
	}
	record := &repowolfv1.GitHubPullRecord{Number: number, Title: title, Body: body, State: state, Draft: draft, Author: author, Head: head, Base: base, HeadObjectId: sha, Url: link, CreatedAt: created, UpdatedAt: updated}
	if details {
		record.MergeableState = value.MergeableState
	}
	return record, nil
}
func pullRecords(values []apiPull, details bool) ([]*repowolfv1.GitHubPullRecord, error) {
	out := make([]*repowolfv1.GitHubPullRecord, 0, len(values))
	for _, v := range values {
		record, e := pullRecord(v, details)
		if e != nil {
			return nil, e
		}
		out = append(out, record)
	}
	return out, nil
}
func runRecord(value apiRun, details bool) (*repowolfv1.GitHubRunRecord, error) {
	id, e := requiredID(value.ID, "id")
	if e != nil {
		return nil, e
	}
	name, e := required(value.DisplayTitle, "display_title")
	if e != nil {
		return nil, e
	}
	workflow, e := required(value.WorkflowName, "name")
	if e != nil {
		return nil, e
	}
	state, e := required(value.Status, "status")
	if e != nil {
		return nil, e
	}
	event, e := required(value.Event, "event")
	if e != nil {
		return nil, e
	}
	branch, e := required(value.HeadBranch, "head_branch")
	if e != nil {
		return nil, e
	}
	sha, e := required(value.HeadSHA, "head_sha")
	if e != nil {
		return nil, e
	}
	link, e := required(value.URL, "html_url")
	if e != nil {
		return nil, e
	}
	created, e := required(value.CreatedAt, "created_at")
	if e != nil {
		return nil, e
	}
	updated, e := required(value.UpdatedAt, "updated_at")
	if e != nil {
		return nil, e
	}
	record := &repowolfv1.GitHubRunRecord{Id: id, Name: name, WorkflowName: workflow, Status: state, Conclusion: value.Conclusion, Event: event, HeadBranch: branch, HeadObjectId: sha, Url: link, CreatedAt: created, UpdatedAt: updated}
	if details {
		if value.Attempt == nil || value.JobsURL == nil {
			return nil, providerResponse(nil, "run details")
		}
		record.Attempt = value.Attempt
		record.JobsUrl = value.JobsURL
	}
	return record, nil
}
func runRecords(values []apiRun, details bool) ([]*repowolfv1.GitHubRunRecord, error) {
	out := make([]*repowolfv1.GitHubRunRecord, 0, len(values))
	for _, v := range values {
		record, e := runRecord(v, details)
		if e != nil {
			return nil, e
		}
		out = append(out, record)
	}
	return out, nil
}
func statusSummary(value apiStatuses) (*repowolfv1.GitHubStatusSummary, error) {
	state, e := required(value.State, "state")
	if e != nil {
		return nil, e
	}
	sha, e := required(value.SHA, "sha")
	if e != nil {
		return nil, e
	}
	if value.Statuses == nil {
		return nil, providerResponse(nil, "statuses")
	}
	records := make([]*repowolfv1.GitHubStatusRecord, 0, len(*value.Statuses))
	for _, v := range *value.Statuses {
		name, e := required(v.Context, "status.context")
		if e != nil {
			return nil, e
		}
		state, e := required(v.State, "status.state")
		if e != nil {
			return nil, e
		}
		records = append(records, &repowolfv1.GitHubStatusRecord{Name: name, State: state, Description: v.Description, TargetUrl: v.TargetURL, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
	}
	return &repowolfv1.GitHubStatusSummary{State: state, ObjectId: sha, Statuses: records}, nil
}
func checkRecord(value apiCheckRun) (*repowolfv1.GitHubCheckRecord, error) {
	name, e := required(value.Name, "check.name")
	if e != nil {
		return nil, e
	}
	state, e := required(value.Status, "check.status")
	if e != nil {
		return nil, e
	}
	var description *string
	if value.Output != nil {
		description = value.Output.Title
	}
	return &repowolfv1.GitHubCheckRecord{Name: name, State: state, Conclusion: value.Conclusion, DetailsUrl: value.DetailsURL, Description: description, StartedAt: value.StartedAt, CompletedAt: value.CompletedAt}, nil
}
func users(values *[]apiUser, field string) ([]string, error) {
	if values == nil {
		return nil, providerResponse(nil, field)
	}
	out := make([]string, 0, len(*values))
	for _, v := range *values {
		name, e := userLogin(&v, field)
		if e != nil {
			return nil, e
		}
		out = append(out, name)
	}
	return out, nil
}
func labelNames(values *[]apiLabel) ([]string, error) {
	if values == nil {
		return nil, providerResponse(nil, "labels")
	}
	out := make([]string, 0, len(*values))
	for _, v := range *values {
		name, e := required(v.Name, "label.name")
		if e != nil {
			return nil, e
		}
		out = append(out, name)
	}
	return out, nil
}
