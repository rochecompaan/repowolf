package gitea

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdk "gitea.dev/sdk"
	repowolfv1 "github.com/rochecompaan/repowolf/gen/repowolf/v1"
	"github.com/rochecompaan/repowolf/internal/policy"
	"github.com/rochecompaan/repowolf/internal/rpcstatus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type issueCommentPage struct {
	entryCount int
	comments   []*sdk.Comment
}

type giteaAPI interface {
	GetRepo(context.Context, string, string) (*sdk.Repository, error)
	ListRepoIssues(context.Context, string, string, sdk.ListIssueOption) ([]*sdk.Issue, error)
	GetIssue(context.Context, string, string, int64) (*sdk.Issue, int, error)
	ListIssueTimeline(context.Context, string, string, int64, sdk.ListIssueCommentOptions) (issueCommentPage, error)
}

type repositorySDKClient interface {
	GetRepo(context.Context, string, string) (*sdk.Repository, *sdk.Response, error)
}

type issueSDKClient interface {
	ListRepoIssues(context.Context, string, string, sdk.ListIssueOption) ([]*sdk.Issue, *sdk.Response, error)
	GetIssue(context.Context, string, string, int64) (*sdk.Issue, *sdk.Response, error)
	ListIssueTimeline(context.Context, string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.TimelineComment, *sdk.Response, error)
}

type sdkAPI struct {
	repositories repositorySDKClient
	issues       issueSDKClient
}

func (a *sdkAPI) GetRepo(ctx context.Context, owner, repo string) (*sdk.Repository, error) {
	value, _, err := a.repositories.GetRepo(ctx, owner, repo)
	return value, err
}

func (a *sdkAPI) ListRepoIssues(ctx context.Context, owner, repo string, options sdk.ListIssueOption) ([]*sdk.Issue, error) {
	values, _, err := a.issues.ListRepoIssues(ctx, owner, repo, options)
	return values, err
}

func (a *sdkAPI) GetIssue(ctx context.Context, owner, repo string, index int64) (*sdk.Issue, int, error) {
	value, response, err := a.issues.GetIssue(ctx, owner, repo, index)
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	return value, status, err
}

func (a *sdkAPI) ListIssueTimeline(ctx context.Context, owner, repo string, index int64, options sdk.ListIssueCommentOptions) (issueCommentPage, error) {
	values, _, err := a.issues.ListIssueTimeline(ctx, owner, repo, index, options)
	page := issueCommentPage{entryCount: len(values), comments: make([]*sdk.Comment, 0, len(values))}
	for _, value := range values {
		if value == nil {
			page.comments = append(page.comments, nil)
			continue
		}
		if value.Type != "comment" {
			continue
		}
		page.comments = append(page.comments, &sdk.Comment{
			ID: value.ID, HTMLURL: value.HTMLURL, PRURL: value.PRURL,
			IssueURL: value.IssueURL, Poster: value.Poster,
			OriginalAuthor: value.OriginalAuthor, OriginalAuthorID: value.OriginalAuthorID,
			Body: value.Body, Created: value.Created, Updated: value.Updated,
		})
	}
	return page, err
}

type RepositoryAdapter struct {
	api giteaAPI
}

func NewRepositoryAdapter(client *sdk.Client) (*RepositoryAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil client")
	}
	return &RepositoryAdapter{api: &sdkAPI{repositories: client.Repositories, issues: client.Issues}}, nil
}
func newRepositoryAdapter(api giteaAPI) (*RepositoryAdapter, error) {
	if api == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil api")
	}
	return &RepositoryAdapter{api: api}, nil
}
func newIssueAdapter(api giteaAPI) (*RepositoryAdapter, error) {
	return newRepositoryAdapter(api)
}

func (a *RepositoryAdapter) Execute(ctx context.Context, repo policy.ResolvedRepository, request *repowolfv1.GiteaRequest) (*repowolfv1.GiteaResponse, error) {
	if a == nil || a.api == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	if ValidateRequest(request) != nil {
		return nil, ErrInvalidRequest
	}
	switch {
	case request.GetRepositoryView() != nil:
		return a.repository(ctx, repo)
	case request.GetIssueList() != nil:
		return a.issueList(ctx, repo, request.GetIssueList())
	case request.GetIssueView() != nil:
		return a.issueView(ctx, repo, request.GetIssueView())
	}
	return nil, ErrInvalidRequest
}
func (a *RepositoryAdapter) repository(ctx context.Context, repository policy.ResolvedRepository) (*repowolfv1.GiteaResponse, error) {
	result, err := a.api.GetRepo(ctx, repository.Repository.Owner, repository.Repository.Name)
	if err != nil {
		return nil, classifyProviderError(ctx, err)
	}
	if validateRepository(result, repository.Repository.Owner, repository.Repository.Name) != nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	record := &repowolfv1.GiteaRepositoryRecord{FullName: result.FullName, Description: result.Description, DefaultBranch: result.DefaultBranch, Url: result.HTMLURL, SshUrl: result.SSHURL, CloneUrl: result.CloneURL, Private: result.Private, Archived: result.Archived, Fork: result.Fork, Mirror: result.Mirror, Empty: result.Empty, Stars: uint64(result.Stars), Forks: uint64(result.Forks), OpenIssues: uint64(result.OpenIssues), Size: uint64(result.Size), Topics: append([]string{}, result.Topics...), Created: timestamppb.New(result.Created), Updated: timestamppb.New(result.Updated)}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_RepositoryView{RepositoryView: &repowolfv1.GiteaRepositoryViewResult{Repository: record}}}, nil
}
func (a *RepositoryAdapter) issueList(ctx context.Context, repository policy.ResolvedRepository, r *repowolfv1.GiteaIssueListRequest) (*repowolfv1.GiteaResponse, error) {
	owner, name := repository.Repository.Owner, repository.Repository.Name
	if r.Owner != nil && !strings.EqualFold(r.GetOwner(), owner) {
		return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: []*repowolfv1.GiteaIssueRecord{}}}}, nil
	}
	states := map[repowolfv1.GiteaIssueState]sdk.StateType{
		repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_OPEN:   sdk.StateOpen,
		repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_CLOSED: sdk.StateClosed,
		repowolfv1.GiteaIssueState_GITEA_ISSUE_STATE_ALL:    sdk.StateAll,
	}
	opt := sdk.ListIssueOption{ListOptions: sdk.ListOptions{Page: int(r.Page), PageSize: int(r.Limit)}, State: states[r.State], Type: sdk.IssueTypeIssue, KeyWord: r.GetKeyword(), CreatedBy: r.GetAuthor(), AssignedBy: r.GetAssignee(), MentionedBy: r.GetMentions(), Owner: canonicalOwnerFilter(r, owner)}
	if r.From != nil {
		opt.Since = r.From.AsTime()
	}
	if r.Until != nil {
		opt.Before = r.Until.AsTime()
	}
	issues, err := a.api.ListRepoIssues(ctx, owner, name, opt)
	if err != nil {
		return nil, classifyProviderError(ctx, err)
	}
	if len(issues) > int(r.Limit) {
		return nil, rpcstatus.ErrProviderFailure
	}
	records := make([]*repowolfv1.GiteaIssueRecord, len(issues))
	for i, v := range issues {
		n, e := normalizeIssue(v, owner, name)
		if e != nil {
			return nil, rpcstatus.ErrProviderFailure
		}
		records[i] = projectIssue(n, r.Fields)
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueList{IssueList: &repowolfv1.GiteaIssueListResult{Issues: records}}}, nil
}
func (a *RepositoryAdapter) issueView(ctx context.Context, repository policy.ResolvedRepository, r *repowolfv1.GiteaIssueViewRequest) (*repowolfv1.GiteaResponse, error) {
	owner, name := repository.Repository.Owner, repository.Repository.Name
	issue, statusCode, err := a.api.GetIssue(ctx, owner, name, r.Index)
	if err != nil {
		if statusCode == 404 {
			return nil, rpcstatus.ErrNotFound
		}
		return nil, classifyProviderError(ctx, err)
	}
	if issue == nil {
		return nil, rpcstatus.ErrProviderFailure
	}
	if issue.PullRequest != nil {
		return nil, rpcstatus.ErrIssueKind
	}
	n, e := normalizeIssue(issue, owner, name)
	if e != nil || n.index != r.Index {
		return nil, rpcstatus.ErrProviderFailure
	}
	record := projectIssue(n, allIssueFields)
	if r.IncludeComments {
		comments, e := loadIssueComments(ctx, a.api, owner, name, r.Index, record)
		if e != nil {
			return nil, e
		}
		record.Comments = comments
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_IssueView{IssueView: &repowolfv1.GiteaIssueViewResult{Issue: record}}}, nil
}
func canonicalOwnerFilter(request *repowolfv1.GiteaIssueListRequest, owner string) string {
	if request.Owner != nil {
		return owner
	}
	return ""
}

func classifyProviderError(ctx context.Context, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	return rpcstatus.ErrProviderFailure
}
func validateRepository(repository *sdk.Repository, owner, name string) error {
	if repository == nil || repository.FullName != owner+"/"+name || repository.HTMLURL == "" || repository.SSHURL == "" || repository.CloneURL == "" || repository.Created.IsZero() || repository.Updated.IsZero() || repository.Updated.Before(repository.Created) || repository.Stars < 0 || repository.Forks < 0 || repository.OpenIssues < 0 || repository.Size < 0 {
		return fmt.Errorf("invalid repository")
	}
	values := []string{repository.FullName, repository.Description, repository.DefaultBranch, repository.HTMLURL, repository.SSHURL, repository.CloneURL}
	values = append(values, repository.Topics...)
	for _, value := range values {
		if !validProviderString(value) {
			return fmt.Errorf("invalid repository string")
		}
	}
	return nil
}
