package github

import repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"

func issueListResponse(records []*repowolfv1.GitHubIssueRecord) *repowolfv1.GitHubResponse {
	return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueList{IssueList: &repowolfv1.GitHubIssueListResult{Issues: records}}}
}
func issueResponse(request *repowolfv1.GitHubRequest, record *repowolfv1.GitHubIssueRecord) (*repowolfv1.GitHubResponse, error) {
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_IssueView:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueView{IssueView: &repowolfv1.GitHubIssueViewResult{Issue: record}}}, nil
	case *repowolfv1.GitHubRequest_IssueCreate:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueCreate{IssueCreate: &repowolfv1.GitHubIssueCreateResult{Issue: record}}}, nil
	case *repowolfv1.GitHubRequest_IssueEdit:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueEdit{IssueEdit: &repowolfv1.GitHubIssueEditResult{Issue: record}}}, nil
	case *repowolfv1.GitHubRequest_IssueClose:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueClose{IssueClose: &repowolfv1.GitHubIssueCloseResult{Issue: record}}}, nil
	case *repowolfv1.GitHubRequest_IssueReopen:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueReopen{IssueReopen: &repowolfv1.GitHubIssueReopenResult{Issue: record}}}, nil
	default:
		return nil, ErrInvalidRequest
	}
}
func commentResponse(request *repowolfv1.GitHubRequest, record *repowolfv1.GitHubCommentRecord) (*repowolfv1.GitHubResponse, error) {
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_IssueComment:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_IssueComment{IssueComment: &repowolfv1.GitHubIssueCommentResult{Comment: record}}}, nil
	case *repowolfv1.GitHubRequest_PullComment:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullComment{PullComment: &repowolfv1.GitHubPullCommentResult{Comment: record}}}, nil
	default:
		return nil, ErrInvalidRequest
	}
}
func pullResponse(request *repowolfv1.GitHubRequest, record *repowolfv1.GitHubPullRecord) (*repowolfv1.GitHubResponse, error) {
	switch request.Operation.(type) {
	case *repowolfv1.GitHubRequest_PullView:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullView{PullView: &repowolfv1.GitHubPullViewResult{Pull: record}}}, nil
	case *repowolfv1.GitHubRequest_PullCreate:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullCreate{PullCreate: &repowolfv1.GitHubPullCreateResult{Pull: record}}}, nil
	case *repowolfv1.GitHubRequest_PullEdit:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullEdit{PullEdit: &repowolfv1.GitHubPullEditResult{Pull: record}}}, nil
	case *repowolfv1.GitHubRequest_PullClose:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullClose{PullClose: &repowolfv1.GitHubPullCloseResult{Pull: record}}}, nil
	case *repowolfv1.GitHubRequest_PullReopen:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullReopen{PullReopen: &repowolfv1.GitHubPullReopenResult{Pull: record}}}, nil
	case *repowolfv1.GitHubRequest_PullReady:
		return &repowolfv1.GitHubResponse{Result: &repowolfv1.GitHubResponse_PullReady{PullReady: &repowolfv1.GitHubPullReadyResult{Pull: record}}}, nil
	default:
		return nil, ErrInvalidRequest
	}
}
