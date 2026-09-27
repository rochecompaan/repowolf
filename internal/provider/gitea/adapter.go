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
	ListRepoLabels(context.Context, string, string, sdk.ListLabelsOptions) ([]*sdk.Label, error)
	CreateIssue(context.Context, string, string, sdk.CreateIssueOption) (*sdk.Issue, error)
	CreateIssueComment(context.Context, string, string, int64, sdk.CreateIssueCommentOption) (*sdk.Comment, error)
	EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, error)
	GetAssignees(context.Context, string, string) ([]*sdk.User, error)
	AddIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error)
	DeleteIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, error)
	AddIssueLabels(context.Context, string, string, int64, sdk.IssueLabelsOption) ([]*sdk.Label, error)
	DeleteIssueLabel(context.Context, string, string, int64, int64) error
}

type pullAPI interface {
	ListRepoPullRequests(context.Context, string, string, sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, map[int64]pullPresence, error)
	GetPullRequest(context.Context, string, string, int64) (*sdk.PullRequest, map[int64]pullPresence, error)
	ListPullReviews(context.Context, string, string, int64, sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error)
}

type repositorySDKClient interface {
	GetRepo(context.Context, string, string) (*sdk.Repository, *sdk.Response, error)
	ListRepoLabels(context.Context, string, string, sdk.ListLabelsOptions) ([]*sdk.Label, *sdk.Response, error)
	GetAssignees(context.Context, string, string) ([]*sdk.User, *sdk.Response, error)
}

type issueSDKClient interface {
	ListRepoIssues(context.Context, string, string, sdk.ListIssueOption) ([]*sdk.Issue, *sdk.Response, error)
	GetIssue(context.Context, string, string, int64) (*sdk.Issue, *sdk.Response, error)
	ListIssueTimeline(context.Context, string, string, int64, sdk.ListIssueCommentOptions) ([]*sdk.TimelineComment, *sdk.Response, error)
	CreateIssue(context.Context, string, string, sdk.CreateIssueOption) (*sdk.Issue, *sdk.Response, error)
	CreateIssueComment(context.Context, string, string, int64, sdk.CreateIssueCommentOption) (*sdk.Comment, *sdk.Response, error)
	EditIssue(context.Context, string, string, int64, sdk.EditIssueOption) (*sdk.Issue, *sdk.Response, error)
	AddIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, *sdk.Response, error)
	DeleteIssueAssignees(context.Context, string, string, int64, sdk.IssueAssigneesOption) (*sdk.Issue, *sdk.Response, error)
	AddIssueLabels(context.Context, string, string, int64, sdk.IssueLabelsOption) ([]*sdk.Label, *sdk.Response, error)
	DeleteIssueLabel(context.Context, string, string, int64, int64) (*sdk.Response, error)
}

type pullRequestSDKClient interface {
	ListRepoPullRequests(context.Context, string, string, sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, *sdk.Response, error)
	GetPullRequest(context.Context, string, string, int64) (*sdk.PullRequest, *sdk.Response, error)
	ListPullReviews(context.Context, string, string, int64, sdk.ListPullReviewsOptions) ([]*sdk.PullReview, *sdk.Response, error)
}

type sdkAPI struct {
	repositories repositorySDKClient
	issues       issueSDKClient
	pulls        pullRequestSDKClient
}

func (a *sdkAPI) ListRepoPullRequests(ctx context.Context, owner, repo string, options sdk.ListPullRequestsOptions) ([]*sdk.PullRequest, map[int64]pullPresence, error) {
	callCtx, collector := withPullPresenceCollector(ctx)
	values, _, err := a.pulls.ListRepoPullRequests(callCtx, owner, repo, options)
	if err != nil {
		_, _ = collector.finish(pullPresenceArray)
		return nil, nil, err
	}
	presence, parseErr := collector.finish(pullPresenceArray)
	return values, presence, parseErr
}
func (a *sdkAPI) GetPullRequest(ctx context.Context, owner, repo string, index int64) (*sdk.PullRequest, map[int64]pullPresence, error) {
	callCtx, collector := withPullPresenceCollector(ctx)
	value, _, err := a.pulls.GetPullRequest(callCtx, owner, repo, index)
	if err != nil {
		_, _ = collector.finish(pullPresenceObject)
		return nil, nil, err
	}
	presence, parseErr := collector.finish(pullPresenceObject)
	return value, presence, parseErr
}
func (a *sdkAPI) ListPullReviews(ctx context.Context, owner, repo string, index int64, options sdk.ListPullReviewsOptions) ([]*sdk.PullReview, error) {
	values, _, err := a.pulls.ListPullReviews(ctx, owner, repo, index, options)
	return values, err
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

func (a *sdkAPI) ListRepoLabels(ctx context.Context, owner, repo string, options sdk.ListLabelsOptions) ([]*sdk.Label, error) {
	values, _, err := a.repositories.ListRepoLabels(ctx, owner, repo, options)
	return values, err
}
func (a *sdkAPI) CreateIssue(ctx context.Context, owner, repo string, options sdk.CreateIssueOption) (*sdk.Issue, error) {
	value, _, err := a.issues.CreateIssue(ctx, owner, repo, options)
	return value, err
}
func (a *sdkAPI) CreateIssueComment(ctx context.Context, owner, repo string, index int64, options sdk.CreateIssueCommentOption) (*sdk.Comment, error) {
	value, _, err := a.issues.CreateIssueComment(ctx, owner, repo, index, options)
	return value, err
}
func (a *sdkAPI) EditIssue(ctx context.Context, owner, repo string, index int64, options sdk.EditIssueOption) (*sdk.Issue, error) {
	value, _, err := a.issues.EditIssue(ctx, owner, repo, index, options)
	return value, err
}
func (a *sdkAPI) GetAssignees(ctx context.Context, owner, repo string) ([]*sdk.User, error) {
	values, _, err := a.repositories.GetAssignees(ctx, owner, repo)
	return values, err
}
func (a *sdkAPI) AddIssueAssignees(ctx context.Context, owner, repo string, index int64, options sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	value, _, err := a.issues.AddIssueAssignees(ctx, owner, repo, index, options)
	return value, err
}
func (a *sdkAPI) DeleteIssueAssignees(ctx context.Context, owner, repo string, index int64, options sdk.IssueAssigneesOption) (*sdk.Issue, error) {
	value, _, err := a.issues.DeleteIssueAssignees(ctx, owner, repo, index, options)
	return value, err
}
func (a *sdkAPI) AddIssueLabels(ctx context.Context, owner, repo string, index int64, options sdk.IssueLabelsOption) ([]*sdk.Label, error) {
	values, _, err := a.issues.AddIssueLabels(ctx, owner, repo, index, options)
	return values, err
}
func (a *sdkAPI) DeleteIssueLabel(ctx context.Context, owner, repo string, index, label int64) error {
	_, err := a.issues.DeleteIssueLabel(ctx, owner, repo, index, label)
	return err
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
	api   giteaAPI
	pulls pullAPI
}

func NewRepositoryAdapter(client *sdk.Client) (*RepositoryAdapter, error) {
	if client == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil client")
	}
	api := &sdkAPI{repositories: client.Repositories, issues: client.Issues, pulls: client.PullRequests}
	return &RepositoryAdapter{api: api, pulls: api}, nil
}
func newRepositoryAdapter(api giteaAPI) (*RepositoryAdapter, error) {
	if api == nil {
		return nil, fmt.Errorf("construct Gitea repository adapter: nil api")
	}
	adapter := &RepositoryAdapter{api: api}
	if pulls, ok := api.(pullAPI); ok {
		adapter.pulls = pulls
	}
	return adapter, nil
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
	case request.GetIssueCreate() != nil:
		return a.issueCreate(ctx, repo, request.GetIssueCreate())
	case request.GetIssueComment() != nil:
		return a.issueComment(ctx, repo, request.GetIssueComment())
	case request.GetIssueClose() != nil:
		return a.issueState(ctx, repo, request.GetIssueClose().Index, issueStateClose)
	case request.GetIssueReopen() != nil:
		return a.issueState(ctx, repo, request.GetIssueReopen().Index, issueStateReopen)
	case request.GetIssueEdit() != nil:
		return a.issueEdit(ctx, repo, request.GetIssueEdit())
	case request.GetPullList() != nil:
		return a.pullList(ctx, repo, request.GetPullList())
	case request.GetPullView() != nil:
		return a.pullView(ctx, repo, request.GetPullView())
	}
	return nil, ErrInvalidRequest
}
func (a *RepositoryAdapter) pullView(context.Context, policy.ResolvedRepository, *repowolfv1.GiteaPullViewRequest) (*repowolfv1.GiteaResponse, error) {
	return nil, rpcstatus.ErrServiceUnavailable
}

func (a *RepositoryAdapter) pullList(ctx context.Context, repository policy.ResolvedRepository, request *repowolfv1.GiteaPullListRequest) (*repowolfv1.GiteaResponse, error) {
	if a.pulls == nil {
		return nil, rpcstatus.ErrServiceUnavailable
	}
	states := map[repowolfv1.GiteaPullState]sdk.StateType{repowolfv1.GiteaPullState_GITEA_PULL_STATE_OPEN: sdk.StateOpen, repowolfv1.GiteaPullState_GITEA_PULL_STATE_CLOSED: sdk.StateClosed, repowolfv1.GiteaPullState_GITEA_PULL_STATE_ALL: sdk.StateAll}
	owner, name := repository.Repository.Owner, repository.Repository.Name
	values, presence, err := a.pulls.ListRepoPullRequests(ctx, owner, name, sdk.ListPullRequestsOptions{ListOptions: sdk.ListOptions{Page: int(request.Page), PageSize: int(request.Limit)}, State: states[request.State]})
	if err != nil {
		return nil, classifyProviderError(ctx, err)
	}
	if len(values) > int(request.Limit) || len(values) != len(presence) {
		return nil, rpcstatus.ErrProviderFailure
	}
	normalized := make([]*normalizedPull, len(values))
	seen := map[int64]bool{}
	for i, value := range values {
		if value == nil || seen[value.Index] {
			return nil, rpcstatus.ErrProviderFailure
		}
		seen[value.Index] = true
		raw, ok := presence[value.Index]
		if !ok {
			return nil, rpcstatus.ErrProviderFailure
		}
		normalized[i], err = normalizePull(value, raw, owner, name)
		if err != nil {
			return nil, rpcstatus.ErrProviderFailure
		}
	}
	records := make([]*repowolfv1.GiteaPullRecord, len(normalized))
	for i, value := range normalized {
		records[i] = projectPull(value, request.Fields)
	}
	return &repowolfv1.GiteaResponse{Result: &repowolfv1.GiteaResponse_PullList{PullList: &repowolfv1.GiteaPullListResult{Pulls: records}}}, nil
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
